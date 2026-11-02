package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/sourcerevision"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/plugindecode"
)

type externalDeliveryFixture struct {
	root        string
	repo        string
	entryWork   string
	catalogWork string
	lockPath    string
}

func newExternalDeliveryFixture(t *testing.T, subdirectory, selection string, writePackage func(*testing.T, string)) externalDeliveryFixture {
	t.Helper()
	root := pluginLockFixture(t)
	repo := filepath.Join(root, "repo")

	entryOrigin, entryWork := createBareRepoWithWorkingClone(t, "entry.git")
	packageRoot := entryWork
	if subdirectory != "" {
		packageRoot = filepath.Join(entryWork, filepath.FromSlash(subdirectory))
	}
	writePackage(t, packageRoot)
	commitAndPushAll(t, entryWork, "seed external plugin")

	catalogOrigin, catalogWork := createBareRepoWithWorkingClone(t, "catalog.git")
	writeFile(t, filepath.Join(catalogWork, ".claude-plugin", "marketplace.json"),
		externalPluginCatalogManifest(entryOrigin, subdirectory))
	commitAndPushAll(t, catalogWork, "seed external catalog")

	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  team: {git: file://"+
			filepath.ToSlash(catalogOrigin)+", ref: main, format: claude-marketplace}\nresources:\n  team:\n"+selection)
	withWorkingDir(t, repo)
	return externalDeliveryFixture{
		root: root, repo: repo, entryWork: entryWork,
		catalogWork: catalogWork,
		lockPath:    filepath.Join(repo, ".agents", "lock.yaml"),
	}
}

func externalPluginCatalogManifest(origin, subdirectory string) string {
	source := `{"source":"url","url":"` + filepath.ToSlash(origin) + `","ref":"main"}`
	if subdirectory != "" {
		source = `{"source":"git-subdir","url":"` + filepath.ToSlash(origin) + `","ref":"main","path":"` + subdirectory + `"}`
	}
	return `{"name":"team","plugins":[{"name":"ext-kit","source":` + source + `}]}`
}

func writeExternalSkillVersion(t *testing.T, root, body string) {
	t.Helper()
	writeClaudePluginPackage(t, root, "ext-kit", "ext-skill")
	writeFile(t, filepath.Join(root, "skills", "ext-skill", "SKILL.md"),
		"---\nname: ext-skill\n---\n\n"+body+"\n")
}

// spec: plugin-delivery/external-git-subdirectory-entry-delivers-from-that-subdirectory
// spec: plugin-delivery/external-entry-exclusions-are-honoured
func TestSyncExternalGitSubdirectoryHonorsExclusions(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	fixture := newExternalDeliveryFixture(t, "packages/ext-kit",
		"    - {kind: plugin, name: ext-kit, delivery: decompose, exclude: [skill/b]}\n",
		func(t *testing.T, root string) {
			writeTwoMemberPluginPackage(t, root, "ext-kit", "a", "b")
		})

	output, err := executeCommand("sync", "--no-backup")
	if err != nil {
		t.Fatalf("sync of an external subdirectory plugin failed: %v\n%s", err, output)
	}
	if strings.Contains(output, "plugin.delivery.pending") {
		t.Fatalf("external subdirectory plugin delivery must not remain pending:\n%s", output)
	}

	members, err := filepath.Glob(filepath.Join(fixture.repo, ".claude", "skills", "ext-kit-*", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, member := range members {
		names = append(names, filepath.Base(filepath.Dir(member)))
	}
	if !reflect.DeepEqual(names, []string{"ext-kit-a"}) {
		t.Fatalf("delivered plugin members = %v, want only the post-exclusion member ext-kit-a", names)
	}

	doc, data := readLock(t, fixture.lockPath)
	var externalRoot *lockfile.GitRoot
	var externalRegistry *lockfile.GitRegistry
	var catalogRegistry *lockfile.GitRegistry
	for _, registry := range doc.Registries {
		for _, root := range registry.Roots {
			if root.Path == "packages/ext-kit" && root.RevisionSchema == "plugin-v1" {
				externalRegistry = &registry
				externalRoot = &root
			}
			if root.SourceHarness == "claude-marketplace" {
				catalogRegistry = &registry
			}
		}
	}
	if externalRoot == nil || externalRegistry == nil || catalogRegistry == nil {
		t.Fatalf("external plugin lock must record its subdirectory root:\n%s", data)
	}
	decodedCatalog, _, err := plugindecode.DecodeCatalog(fixture.catalogWork, "claude-marketplace")
	if err != nil {
		t.Fatal(err)
	}
	var externalEntry *plugindecode.CatalogEntry
	for i := range decodedCatalog.Entries {
		if decodedCatalog.Entries[i].Name == "ext-kit" {
			externalEntry = &decodedCatalog.Entries[i]
		}
	}
	if externalEntry == nil {
		t.Fatal("fixture catalog does not contain ext-kit")
	}
	oid, err := cache.ParseGitOID(externalRegistry.Commit.String())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, release, err := cache.LeaseExactGitReadOnlyWithContext(context.Background(), externalRegistry.URL, oid)
	if err != nil {
		t.Fatalf("lease locked subdirectory snapshot: %v", err)
	}
	defer release()
	verified, err := sourcerevision.VerifyLockedGitRootWithPluginContext(snapshot, *externalRoot,
		&sourcerevision.PluginVerifyContext{External: &sourcerevision.PluginExternal{
			CatalogKind: "git", CatalogURL: catalogRegistry.URL, CatalogRef: catalogRegistry.Ref,
			CatalogCommit: catalogRegistry.Commit.String(), CatalogRoot: ".", EntryName: externalEntry.Name,
			EntryDigest: externalEntry.DefinitionDigest, CatalogFormat: "claude-marketplace",
			EntryStrict: externalEntry.Strict, EntryOverlay: externalEntry.Overlay, Exclusions: []string{"skill/b"},
		}})
	if err != nil {
		t.Fatalf("locked external plugin manifest did not verify: %v", err)
	}
	if members := verified.PluginManifest.Members; len(members) != 1 || members[0].Entry != "ext-kit" ||
		!reflect.DeepEqual(members[0].Keys, []string{"skill/a"}) {
		t.Fatalf("locked post-exclusion member keys = %+v, want ext-kit:[skill/a]", members)
	}
}

// spec: plugin-delivery/moved-external-ref-keeps-delivering-the-locked-commit
func TestSyncExternalEntryUsesLockedCommitAfterRefMoves(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	fixture := newExternalDeliveryFixture(t, "",
		"    - {kind: plugin, name: ext-kit, delivery: decompose}\n",
		func(t *testing.T, root string) { writeExternalSkillVersion(t, root, "locked revision") })

	if output, err := executeCommand("sync", "--no-backup"); err != nil {
		t.Fatalf("initial external plugin sync failed: %v\n%s", err, output)
	}
	_, lockedBytes := readLock(t, fixture.lockPath)

	writeFile(t, filepath.Join(fixture.entryWork, "skills", "ext-skill", "SKILL.md"),
		"---\nname: ext-skill\n---\n\nlive ref revision\n")
	commitAndPushAll(t, fixture.entryWork, "advance external plugin ref")

	output, err := executeCommand("sync", "--locked")
	if err != nil {
		t.Fatalf("locked sync after the external ref moved failed: %v\n%s", err, output)
	}
	if strings.Contains(output, "plugin.delivery.pending") {
		t.Fatalf("locked sync must deliver from the recorded commit instead of leaving the plugin pending:\n%s", output)
	}
	assertExternalSkillContent(t, fixture.repo, "locked revision")
	_, afterLocked := readLock(t, fixture.lockPath)
	if !bytes.Equal(lockedBytes, afterLocked) {
		t.Fatalf("sync --locked changed the external lock bytes")
	}

	output, err = executeCommand("sync", "--no-backup")
	if err != nil {
		t.Fatalf("plain sync after the external ref moved failed: %v\n%s", err, output)
	}
	if strings.Contains(output, "plugin.delivery.pending") {
		t.Fatalf("plain sync must deliver from the recorded commit instead of leaving the plugin pending:\n%s", output)
	}
	assertExternalSkillContent(t, fixture.repo, "locked revision")
	_, afterPlain := readLock(t, fixture.lockPath)
	if !bytes.Equal(lockedBytes, afterPlain) {
		t.Fatalf("plain sync changed the external lock bytes without an update")
	}
}

// spec: plugin-delivery/delivered-external-entry-reports-no-drift
func TestSyncCheckExternalEntryWritesNothing(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	fixture := newExternalDeliveryFixture(t, "",
		"    - {kind: plugin, name: ext-kit, delivery: decompose}\n",
		func(t *testing.T, root string) { writeExternalSkillVersion(t, root, "stable content") })

	output, err := executeCommand("sync", "--no-backup")
	if err != nil {
		t.Fatalf("initial external plugin sync failed: %v\n%s", err, output)
	}
	if strings.Contains(output, "plugin.delivery.pending") {
		t.Fatalf("initial external plugin delivery must not remain pending:\n%s", output)
	}
	before := snapshotExternalDeliveryTree(t, fixture.root)

	output, err = executeCommand("sync", "--check")
	if err != nil {
		t.Fatalf("sync --check reported drift after external plugin delivery: %v\n%s", err, output)
	}
	if strings.Contains(output, "plugin.delivery.pending") {
		t.Fatalf("sync --check must not report external plugin delivery as pending:\n%s", output)
	}
	after := snapshotExternalDeliveryTree(t, fixture.root)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("sync --check changed files beneath the fixture root")
	}
}

// spec: plugin-delivery/unavailable-locked-external-commit-blocks-only-that-plugin
func TestUnavailableLockedExternalCommitBlocksOnlyPlugin(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root := pluginLockFixture(t)
	entryOrigin, entryWork := createBareRepoWithWorkingClone(t, "entry.git")
	writeExternalSkillVersion(t, entryWork, "locked revision")
	commitAndPushAll(t, entryWork, "seed external plugin")
	entryURL := "file://" + filepath.ToSlash(entryOrigin)
	canonical, err := lockfile.CanonicalGitConfigURL(entryURL)
	if err != nil {
		t.Fatal(err)
	}
	oid, err := cache.ParseGitOID("sha1:" + gitHead(t, entryWork))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := cache.AcquireExactGitWithContext(context.Background(), canonical, entryURL, oid)
	if err != nil {
		t.Fatalf("acquire exact external package snapshot: %v", err)
	}
	if _, err := os.Stat(snapshot); err != nil {
		t.Fatalf("exact external package snapshot is unavailable: %v", err)
	}
	locked := pipeline.LockMetadata{
		Source: config.RepoCommitted, URL: canonical, Ref: "main", Commit: oid.String(),
		RootPaths: []string{"."}, SnapshotAvailable: true,
	}

	catalogDir := filepath.Join(root, "catalog")
	writeClaudePluginPackage(t, filepath.Join(catalogDir, "plugins", "local-kit"), "local-kit", "local-skill")
	writeFile(t, filepath.Join(catalogDir, ".claude-plugin", "marketplace.json"),
		`{"name":"team","plugins":[{"name":"ext-kit","source":{"source":"url","url":"`+
			filepath.ToSlash(entryOrigin)+`","ref":"main"}},{"name":"local-kit","source":"./plugins/local-kit"}]}`)
	leafDir := filepath.Join(root, "leaf")
	writeSkill(t, leafDir, "other", "Other resource", nil)
	configureExternalPipelineRepo(t, filepath.Join(root, "repo"), catalogDir, leafDir)

	initial := syncExternalPipeline(t, filepath.Join(root, "repo"), []pipeline.LockMetadata{locked})
	if len(initial.Errors) != 0 {
		t.Fatalf("initial sync with the exact locked snapshot failed: %v", initial.Errors)
	}
	pluginMember := filepath.Join(root, "repo", ".claude", "skills", "ext-kit-ext-skill", "SKILL.md")
	previous, err := os.ReadFile(pluginMember)
	if err != nil {
		t.Fatalf("initial external plugin member was not installed: %v; warnings: %v", err, initial.Warnings)
	}
	previousInfo, err := os.Stat(pluginMember)
	if err != nil {
		t.Fatal(err)
	}
	localMember := filepath.Join(catalogDir, "plugins", "local-kit", "skills", "local-skill", "SKILL.md")
	writeFile(t, localMember, "---\nname: local-skill\n---\n\nUpdated sibling plugin\n")
	localProjection := filepath.Join(root, "repo", ".claude", "skills", "local-kit-local-skill", "SKILL.md")

	writeSkill(t, leafDir, "other", "Updated independent resource", nil)
	writeExternalSkillVersion(t, cache.RegistryCachePath(entryURL, "main"), "mutable registry cache")
	unavailable := locked
	unavailable.SnapshotAvailable = false
	failed := syncExternalPipeline(t, filepath.Join(root, "repo"), []pipeline.LockMetadata{unavailable})
	assertExternalDeliveryUnavailable(t, failed)
	current, readErr := os.ReadFile(pluginMember)
	currentInfo, statErr := os.Stat(pluginMember)
	if readErr != nil || statErr != nil || !bytes.Equal(current, previous) ||
		!currentInfo.ModTime().Equal(previousInfo.ModTime()) || currentInfo.Mode() != previousInfo.Mode() {
		t.Fatalf("unavailable locked snapshot changed the prior plugin projection: read=%v stat=%v", readErr, statErr)
	}
	other, err := os.ReadFile(filepath.Join(root, "repo", ".claude", "skills", "other", "SKILL.md"))
	if err != nil || !strings.Contains(string(other), "Updated independent resource") {
		t.Fatalf("independent resource did not deliver during the blocked plugin update: %v / %s", err, other)
	}
	sibling, err := os.ReadFile(localProjection)
	if err != nil || !strings.Contains(string(sibling), "Updated sibling plugin") {
		t.Fatalf("independent plugin did not deliver during the blocked plugin update: %v / %s", err, sibling)
	}

	firstInstallRepo := filepath.Join(root, "first-install")
	configureExternalPipelineRepo(t, firstInstallRepo, catalogDir, leafDir)
	first := syncExternalPipeline(t, firstInstallRepo, nil)
	assertExternalDeliveryUnavailable(t, first)
	if _, err := os.Lstat(filepath.Join(firstInstallRepo, ".claude", "skills", "ext-kit-ext-skill")); !os.IsNotExist(err) {
		t.Fatalf("first install with no external lock record must install no plugin members: %v", err)
	}
	if _, err := os.Stat(filepath.Join(firstInstallRepo, ".claude", "skills", "other", "SKILL.md")); err != nil {
		t.Fatalf("independent resource was blocked on first plugin install: %v", err)
	}
	if _, err := os.Stat(filepath.Join(firstInstallRepo, ".claude", "skills", "local-kit-local-skill", "SKILL.md")); err != nil {
		t.Fatalf("independent plugin was blocked on first external plugin install: %v", err)
	}
}

func configureExternalPipelineRepo(t *testing.T, repo, catalogDir, leafDir string) {
	t.Helper()
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n"+
			"  team: {directory: "+filepath.ToSlash(catalogDir)+", format: claude-marketplace}\n"+
			"  leaf: {directory: "+filepath.ToSlash(leafDir)+"}\nresources:\n"+
			"  team:\n    - {kind: plugin, name: ext-kit, delivery: decompose}\n    - {kind: plugin, name: local-kit, delivery: decompose}\n  leaf:\n    - skill/other\n")
}

func syncExternalPipeline(t *testing.T, repo string, metadata []pipeline.LockMetadata) pipeline.SyncResult {
	t.Helper()
	workspace, err := pipeline.NewWorkspace(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	return pipeline.Sync(context.Background(), pipeline.ResolveOptions{
		Workspace: workspace, NoBackup: true, LockMetadata: metadata,
	})
}

func assertExternalDeliveryUnavailable(t *testing.T, result pipeline.SyncResult) {
	t.Helper()
	for _, err := range result.Errors {
		var issue interface {
			DiagnosticCode() string
			DiagnosticSeverity() diag.Severity
			Resource() string
		}
		if errors.As(err, &issue) && issue.DiagnosticCode() == "plugin.delivery.external-unavailable" &&
			issue.DiagnosticSeverity() == diag.SeverityError && issue.Resource() == "plugin/ext-kit" {
			return
		}
	}
	t.Fatalf("sync errors = %v, want an error diagnostic for plugin/ext-kit; warnings: %v", result.Errors, result.Warnings)
}

func assertExternalSkillContent(t *testing.T, repo, want string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, ".claude", "skills", "ext-kit-ext-skill", "SKILL.md"))
	if err != nil || !strings.Contains(string(data), want) {
		t.Fatalf("external plugin skill content = %q / %v, want %q", data, err, want)
	}
}

type externalDeliveryFile struct {
	data    []byte
	mode    os.FileMode
	modTime int64
}

func snapshotExternalDeliveryTree(t *testing.T, root string) map[string]externalDeliveryFile {
	t.Helper()
	files := make(map[string]externalDeliveryFile)
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		item := externalDeliveryFile{mode: info.Mode(), modTime: info.ModTime().UnixNano()}
		if !info.IsDir() {
			item.data, err = os.ReadFile(path)
			if err != nil {
				return err
			}
		}
		files[filepath.ToSlash(rel)] = item
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
