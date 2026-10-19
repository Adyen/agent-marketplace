package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func writeClaudePluginPackage(t *testing.T, dir, name, member string) {
	t.Helper()
	writePluginSourceFixture(t, dir, ".claude-plugin", name, member)
}

// writeCatalogRepo seeds a claude-marketplace working clone with one relative
// entry and, optionally, one external git entry.
func writeCatalogRepo(t *testing.T, working, externalURL string) {
	t.Helper()
	writeClaudePluginPackage(t, filepath.Join(working, "plugins", "local-kit"), "local-kit", "local-skill")
	manifest := `{"name":"team","plugins":[` +
		`{"name":"local-kit","source":"./plugins/local-kit"}`
	if externalURL != "" {
		manifest += `,{"name":"ext-kit","source":{"source":"url","url":"` + filepath.ToSlash(externalURL) + `","ref":"main"}}`
	}
	manifest += `]}`
	writeFile(t, filepath.Join(working, ".claude-plugin", "marketplace.json"), manifest)
}

func pluginLockFixture(t *testing.T) (root string) {
	t.Helper()
	root = canonicalTempDir(t)
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	config.SetTestOrganizationConfigPath(t, filepath.Join(root, "missing-org.yaml"))
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [claude]\n")
	return root
}

func readLock(t *testing.T, path string) (lockfile.Document, []byte) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read lock %s: %v", path, err)
	}
	doc, err := lockfile.Decode(data)
	if err != nil {
		t.Fatalf("plugin lock must decode with the strict v2 decoder: %v\n%s", err, data)
	}
	return doc, data
}

func pluginResources(doc lockfile.Document) []lockfile.GitResource {
	var out []lockfile.GitResource
	for _, registry := range doc.Registries {
		for _, root := range registry.Roots {
			for _, resource := range root.Resources {
				if resource.Kind == "plugin" {
					out = append(out, resource)
				}
			}
		}
	}
	return out
}

// spec: plugin-packages/plugin-selecting-layers-keep-the-v2-lock-format
func TestSyncPluginSelectingLayerKeepsV2LockFormat(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root := pluginLockFixture(t)
	repo := filepath.Join(root, "repo")

	catalogOrigin, catalogWorking := createBareRepoWithWorkingClone(t, "catalog.git")
	writeCatalogRepo(t, catalogWorking, "")
	commitAndPushAll(t, catalogWorking, "seed catalog")

	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  team: {git: file://"+filepath.ToSlash(catalogOrigin)+", ref: main, format: claude-marketplace}\nresources:\n  team:\n    - plugin/local-kit\n")
	withWorkingDir(t, repo)
	if output, err := executeCommand("sync", "--no-backup"); err != nil {
		t.Fatalf("sync of a plugin-selecting layer must succeed: %v\n%s", err, output)
	}

	doc, _ := readLock(t, filepath.Join(repo, ".agents", "lock.yaml"))
	if doc.APIVersion != lockfile.APIVersionV2 {
		t.Fatalf("lock apiVersion = %q, want %q", doc.APIVersion, lockfile.APIVersionV2)
	}
	plugins := pluginResources(doc)
	if len(plugins) != 1 || plugins[0].Name != "local-kit" {
		t.Fatalf("lock must carry exactly one plugin record for local-kit: %+v", plugins)
	}
	if len(plugins[0].SupportFiles) == 0 {
		t.Fatalf("plugin record must carry the verified file inventory")
	}
	found := false
	for _, registry := range doc.Registries {
		for _, r := range registry.Roots {
			if r.RevisionSchema == "plugin-v1" && r.SourceHarness == "claude-marketplace" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("format root must use revision_schema plugin-v1 with the format ID as source_harness:\n%+v", doc.Registries)
	}
}

// spec: plugin-packages/external-catalog-entry-is-locked-to-its-own-commit
func TestSyncExternalCatalogEntryLockedToOwnCommit(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root := pluginLockFixture(t)
	repo := filepath.Join(root, "repo")

	entryOrigin, entryWorking := createBareRepoWithWorkingClone(t, "entry.git")
	writeClaudePluginPackage(t, entryWorking, "ext-kit", "ext-skill")
	commitAndPushAll(t, entryWorking, "seed entry")
	lockedCommit := gitHead(t, entryWorking)

	catalogOrigin, catalogWorking := createBareRepoWithWorkingClone(t, "catalog.git")
	writeCatalogRepo(t, catalogWorking, entryOrigin)
	commitAndPushAll(t, catalogWorking, "seed catalog")

	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  team: {git: file://"+filepath.ToSlash(catalogOrigin)+", ref: main, format: claude-marketplace}\nresources:\n  team:\n    - plugin/ext-kit\n")
	withWorkingDir(t, repo)
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	if output, err := executeCommand("sync", "--no-backup"); err != nil {
		t.Fatalf("sync of an external plugin selection must succeed: %v\n%s", err, output)
	}
	doc, before := readLock(t, lockPath)
	var entryRegistry *lockfile.GitRegistry
	for i := range doc.Registries {
		r := &doc.Registries[i]
		for _, rt := range r.Roots {
			for _, res := range rt.Resources {
				if res.Kind == "plugin" && res.Name == "ext-kit" {
					entryRegistry = r
				}
			}
		}
	}
	if entryRegistry == nil {
		t.Fatalf("external plugin must be locked on its own registry record:\n%s", before)
	}
	if !strings.HasSuffix(entryRegistry.Commit.String(), lockedCommit) {
		t.Fatalf("external record commit = %s, want %s", entryRegistry.Commit.String(), lockedCommit)
	}
	contentDigest := ""
	for _, rt := range entryRegistry.Roots {
		for _, res := range rt.Resources {
			if res.Kind == "plugin" {
				contentDigest = res.ContentDigest.String()
			}
		}
	}

	// Move the entry ref past the locked commit; locked mode must still use
	// the recorded commit, observable through the unchanged lock bytes and
	// the unchanged plugin content digest.
	writeFile(t, filepath.Join(entryWorking, "moved.txt"), "moved")
	commitAndPushAll(t, entryWorking, "advance entry main")

	output, err := executeCommand("sync", "--locked")
	if err != nil {
		t.Fatalf("sync --locked must use the recorded external commit: %v\n%s", err, output)
	}
	_, after := readLock(t, lockPath)
	if string(after) != string(before) {
		t.Fatalf("sync --locked must leave lock bytes unchanged:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	docAfter, _ := readLock(t, lockPath)
	got := ""
	for _, registry := range docAfter.Registries {
		for _, rt := range registry.Roots {
			for _, res := range rt.Resources {
				if res.Kind == "plugin" && res.Name == "ext-kit" {
					got = res.ContentDigest.String()
				}
			}
		}
	}
	if got != contentDigest {
		t.Fatalf("locked plugin content digest changed after ref moved: %s -> %s", contentDigest, got)
	}
}

// spec: plugin-packages/changed-catalog-entry-makes-the-lock-stale
func TestSyncChangedCatalogEntryMakesLockStale(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root := pluginLockFixture(t)
	repo := filepath.Join(root, "repo")

	entryOrigin, entryWorking := createBareRepoWithWorkingClone(t, "entry.git")
	writeClaudePluginPackage(t, entryWorking, "ext-kit", "ext-skill")
	commitAndPushAll(t, entryWorking, "seed entry")

	catalogDir := filepath.Join(root, "marketplace")
	writeCatalogRepo(t, catalogDir, entryOrigin)

	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  team: {directory: "+filepath.ToSlash(catalogDir)+", format: claude-marketplace}\nresources:\n  team:\n    - plugin/ext-kit\n")
	withWorkingDir(t, repo)
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	if output, err := executeCommand("sync", "--no-backup"); err != nil {
		t.Fatalf("sync of a directory-catalog external plugin must succeed: %v\n%s", err, output)
	}

	// Change the selected entry's definition in the live directory catalog.
	writeFile(t, filepath.Join(catalogDir, ".claude-plugin", "marketplace.json"),
		`{"name":"team","plugins":[`+
			`{"name":"local-kit","source":"./plugins/local-kit"},`+
			`{"name":"ext-kit","description":"changed","source":{"source":"url","url":"`+filepath.ToSlash(entryOrigin)+`","ref":"main"}}]}`)

	snapshot := func() map[string][]byte {
		files := map[string][]byte{}
		for _, rel := range []string{filepath.Join(".agents", "config.yaml"), filepath.Join(".agents", "lock.yaml"), filepath.Join(".claude", "skills", "ext-skill", "SKILL.md")} {
			data, err := os.ReadFile(filepath.Join(repo, rel))
			if err != nil {
				continue
			}
			files[rel] = data
		}
		return files
	}
	beforeFiles := snapshot()

	for _, args := range [][]string{{"sync", "--check"}, {"sync", "--locked"}} {
		output, err := executeCommand(args...)
		if err == nil {
			t.Fatalf("%v must report the plugin record stale after the entry changed:\n%s", args, output)
		}
		if !strings.Contains(output, "lock.") {
			t.Fatalf("%v must report a stale lock diagnostic, got:\n%s", args, output)
		}
		if strings.Contains(output, "plugin.compile") {
			t.Fatalf("%v must report the stale lock, not attempt plugin member delivery, got:\n%s", args, output)
		}
		after := snapshot()
		for rel, data := range beforeFiles {
			if string(after[rel]) != string(data) {
				t.Fatalf("%v must write nothing after reporting stale; %s changed", args, rel)
			}
		}
		if _, statErr := os.Stat(lockPath); statErr != nil {
			t.Fatalf("%v must not remove the lock: %v", args, statErr)
		}
	}
}

// spec: plugin-packages/changed-catalog-entry-makes-the-lock-stale
func TestSyncChangedExternalEntryRefResolvesAfresh(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root := pluginLockFixture(t)
	repo := filepath.Join(root, "repo")

	entryOrigin, entryWorking := createBareRepoWithWorkingClone(t, "entry.git")
	writeClaudePluginPackage(t, entryWorking, "ext-kit", "ext-skill")
	commitAndPushAll(t, entryWorking, "seed entry")

	catalogDir := filepath.Join(root, "marketplace")
	writeCatalogRepo(t, catalogDir, entryOrigin)

	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  team: {directory: "+filepath.ToSlash(catalogDir)+", format: claude-marketplace}\nresources:\n  team:\n    - plugin/ext-kit\n")
	withWorkingDir(t, repo)
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	if output, err := executeCommand("sync", "--no-backup"); err != nil {
		t.Fatalf("sync of a directory-catalog external plugin must succeed: %v\n%s", err, output)
	}
	_, before := readLock(t, lockPath)

	// Commit a second ref locally but keep it unpublished: read-only runs must
	// report stale from the recorded commit without resolving the new ref.
	runGit(t, entryWorking, "checkout", "-b", "release")
	writeFile(t, filepath.Join(entryWorking, "release.txt"), "release")
	runGit(t, entryWorking, "add", ".")
	runGit(t, entryWorking, "commit", "-m", "release commit")
	releaseCommit := gitHead(t, entryWorking)

	writeFile(t, filepath.Join(catalogDir, ".claude-plugin", "marketplace.json"),
		`{"name":"team","plugins":[`+
			`{"name":"local-kit","source":"./plugins/local-kit"},`+
			`{"name":"ext-kit","source":{"source":"url","url":"`+filepath.ToSlash(entryOrigin)+`","ref":"release"}}]}`)

	for _, args := range [][]string{{"sync", "--check"}, {"sync", "--locked"}, {"sync", "--offline"}} {
		output, err := executeCommand(args...)
		if err == nil {
			t.Fatalf("%v must report stale after the entry ref changed:\n%s", args, output)
		}
		if !strings.Contains(output, "lock.") {
			t.Fatalf("%v must report a stale lock diagnostic without resolving the unpublished ref, got:\n%s", args, output)
		}
		_, after := readLock(t, lockPath)
		if string(after) != string(before) {
			t.Fatalf("%v must write nothing after reporting stale", args)
		}
	}

	// Publish the ref; plain sync resolves it afresh.
	runGit(t, entryWorking, "push", "origin", "release")

	if output, err := executeCommand("sync", "--no-backup"); err != nil {
		t.Fatalf("plain sync must re-resolve the edited entry ref: %v\n%s", err, output)
	}
	doc, data := readLock(t, lockPath)
	var entryRegistry *lockfile.GitRegistry
	for i := range doc.Registries {
		r := &doc.Registries[i]
		for _, rt := range r.Roots {
			for _, res := range rt.Resources {
				if res.Kind == "plugin" && res.Name == "ext-kit" {
					entryRegistry = r
				}
			}
		}
	}
	if entryRegistry == nil {
		t.Fatalf("external plugin must keep its own registry record:\n%s", data)
	}
	if entryRegistry.Ref != "release" || !strings.HasSuffix(entryRegistry.Commit.String(), releaseCommit) {
		t.Fatalf("external record = ref %q commit %s, want release @ %s", entryRegistry.Ref, entryRegistry.Commit.String(), releaseCommit)
	}
	if output, err := executeCommand("sync", "--check"); err != nil {
		t.Fatalf("sync --check must pass after the resync: %v\n%s", err, output)
	}
}

// spec: plugin-packages/changed-catalog-entry-makes-the-lock-stale
func TestSyncNewlySelectedExternalEntryReadOnlyReportsStale(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root := pluginLockFixture(t)
	repo := filepath.Join(root, "repo")

	entryOrigin, entryWorking := createBareRepoWithWorkingClone(t, "entry.git")
	writeClaudePluginPackage(t, entryWorking, "ext-kit", "ext-skill")
	commitAndPushAll(t, entryWorking, "seed entry")

	regOrigin, regWorking := createBareRepoWithWorkingClone(t, "reg.git")
	writeSkill(t, regWorking, "tooling", "Tooling baseline", nil)
	commitAndPushAll(t, regWorking, "seed reg")

	catalogDir := filepath.Join(root, "marketplace")
	writeCatalogRepo(t, catalogDir, entryOrigin)

	cfgPath := filepath.Join(repo, ".agents", "config.yaml")
	configBody := func(withExternal bool) string {
		selection := "    - plugin/local-kit\n"
		if withExternal {
			selection += "    - plugin/ext-kit\n"
		}
		return "apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  team: {directory: " + filepath.ToSlash(catalogDir) + ", format: claude-marketplace}\n  reg: {git: file://" + filepath.ToSlash(regOrigin) + ", ref: main}\nresources:\n  team:\n" + selection + "  reg:\n    - skill/tooling\n"
	}
	writeConfig(t, cfgPath, configBody(false))
	withWorkingDir(t, repo)
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	if output, err := executeCommand("sync", "--no-backup"); err != nil {
		t.Fatalf("sync of the pre-selection layer must succeed: %v\n%s", err, output)
	}
	_, before := readLock(t, lockPath)

	// Make the entry origin unreachable, then select the external entry:
	// read-only modes must report the lock stale without resolving its ref.
	hidden := entryOrigin + ".hidden"
	if err := os.Rename(entryOrigin, hidden); err != nil {
		t.Fatalf("hide entry origin: %v", err)
	}
	writeConfig(t, cfgPath, configBody(true))

	for _, args := range [][]string{{"sync", "--check"}, {"sync", "--locked"}, {"sync", "--offline"}} {
		output, err := executeCommand(args...)
		if err == nil {
			t.Fatalf("%v must report stale when a selected external entry has no locked record:\n%s", args, output)
		}
		if !strings.Contains(output, "lock.") {
			t.Fatalf("%v must report a stale lock diagnostic without resolving the entry ref, got:\n%s", args, output)
		}
		_, after := readLock(t, lockPath)
		if string(after) != string(before) {
			t.Fatalf("%v must write nothing after reporting stale", args)
		}
	}

	if err := os.Rename(hidden, entryOrigin); err != nil {
		t.Fatalf("restore entry origin: %v", err)
	}
	if output, err := executeCommand("sync", "--no-backup"); err != nil {
		t.Fatalf("plain sync must resolve the newly selected external entry: %v\n%s", err, output)
	}
	doc, data := readLock(t, lockPath)
	found := false
	for _, registry := range doc.Registries {
		for _, rt := range registry.Roots {
			for _, res := range rt.Resources {
				if res.Kind == "plugin" && res.Name == "ext-kit" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatalf("plain sync must record the newly selected external entry:\n%s", data)
	}
}

// spec: plugin-packages/catalog-listed-origin-outside-source-policy-is-refused
func TestSyncCatalogListedOriginOutsideSourcePolicyIsRefused(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root := pluginLockFixture(t)
	repo := filepath.Join(root, "repo")

	catalogDir := filepath.Join(root, "marketplace")
	writeClaudePluginPackage(t, filepath.Join(catalogDir, "plugins", "local-kit"), "local-kit", "local-skill")
	writeFile(t, filepath.Join(catalogDir, ".claude-plugin", "marketplace.json"),
		`{"name":"team","plugins":[`+
			`{"name":"local-kit","source":"./plugins/local-kit"},`+
			`{"name":"ext-kit","source":{"source":"url","url":"https://disallowed.example.com/team/ext-kit.git","ref":"main"}}]}`)

	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nallowed_registries: ['^catalog\\.example\\.com/']\nsources:\n  team: {directory: "+filepath.ToSlash(catalogDir)+", format: claude-marketplace}\nresources:\n  team:\n    - plugin/ext-kit\n")
	withWorkingDir(t, repo)

	output, err := executeCommand("sync", "--no-backup")
	if err == nil {
		t.Fatalf("selecting a catalog-listed origin outside the source policy must fail:\n%s", output)
	}
	if !strings.Contains(output, "allowed_registries") || !strings.Contains(output, "disallowed.example.com") {
		t.Fatalf("refusal must name the policy and the origin:\n%s", output)
	}
	if !strings.Contains(output, "ext-kit") {
		t.Fatalf("refusal must name the catalog entry:\n%s", output)
	}
	if _, statErr := os.Stat(filepath.Join(repo, ".agents", "lock.yaml")); !os.IsNotExist(statErr) {
		t.Fatalf("no lock may be written when the origin is refused")
	}
	// The refused origin must never have been cloned: no object store or
	// snapshot for it may exist in the exact-git cache.
	cacheRoot := filepath.Join(root, "cache")
	found := false
	_ = filepath.Walk(cacheRoot, func(path string, _ os.FileInfo, _ error) error {
		if strings.Contains(path, "disallowed") {
			found = true
		}
		return nil
	})
	if found {
		t.Fatalf("refused origin must not be cloned or fetched")
	}
}

// Removing a package-format git source after locking must report the lock
// stale: its record is a configured-source record, not an external record
// exempt from the identity match.
func TestSyncRemovedPackageFormatSourceMakesLockStale(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root := pluginLockFixture(t)
	repo := filepath.Join(root, "repo")

	origin, working := createBareRepoWithWorkingClone(t, "pkg.git")
	writeFile(t, filepath.Join(working, "plugin.json"), `{"$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json", "name": "agent-kit"}`)
	writeFile(t, filepath.Join(working, "skills", "agent-skill", "SKILL.md"), "---\nname: agent-skill\n---\n")
	commitAndPushAll(t, working, "seed package")

	cfgPath := filepath.Join(repo, ".agents", "config.yaml")
	writeConfig(t, cfgPath, "apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  team: {git: file://"+filepath.ToSlash(origin)+", ref: main, format: agent-plugin}\nresources:\n  team: {all: true}\n")
	withWorkingDir(t, repo)
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	if output, err := executeCommand("sync", "--no-backup"); err != nil {
		t.Fatalf("sync of a package-format source must succeed: %v\n%s", err, output)
	}
	_, before := readLock(t, lockPath)

	// Remove the format source entirely; the lock's record must now report
	// stale instead of being treated as an external record.
	writeConfig(t, cfgPath, "apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\n")

	output, err := executeCommand("sync", "--check")
	if err == nil {
		t.Fatalf("sync --check must report stale after the format source was removed:\n%s", output)
	}
	if !strings.Contains(output, "lock.") {
		t.Fatalf("sync --check must report a stale lock diagnostic, got:\n%s", output)
	}
	_, after := readLock(t, lockPath)
	if string(after) != string(before) {
		t.Fatalf("sync --check must write nothing after reporting stale")
	}
}

// Deselecting a plugin whose catalog entry is external must report the lock
// stale; the next plain sync drops the external record.
func TestSyncDeselectedExternalEntryMakesLockStale(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root := pluginLockFixture(t)
	repo := filepath.Join(root, "repo")

	entryOrigin, entryWorking := createBareRepoWithWorkingClone(t, "entry.git")
	writeClaudePluginPackage(t, entryWorking, "ext-kit", "ext-skill")
	commitAndPushAll(t, entryWorking, "seed entry")

	catalogOrigin, catalogWorking := createBareRepoWithWorkingClone(t, "catalog.git")
	writeCatalogRepo(t, catalogWorking, entryOrigin)
	commitAndPushAll(t, catalogWorking, "seed catalog")

	cfgPath := filepath.Join(repo, ".agents", "config.yaml")
	configBody := func(selectPlugin bool) string {
		resources := ""
		if selectPlugin {
			resources = "\nresources:\n  team:\n    - plugin/ext-kit\n"
		}
		return "apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  team: {git: file://" + filepath.ToSlash(catalogOrigin) + ", ref: main, format: claude-marketplace}\n" + resources
	}
	writeConfig(t, cfgPath, configBody(true))
	withWorkingDir(t, repo)
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	if output, err := executeCommand("sync", "--no-backup"); err != nil {
		t.Fatalf("sync of an external plugin selection must succeed: %v\n%s", err, output)
	}
	_, before := readLock(t, lockPath)

	writeConfig(t, cfgPath, configBody(false))
	output, err := executeCommand("sync", "--check")
	if err == nil {
		t.Fatalf("sync --check must report stale after the external entry was deselected:\n%s", output)
	}
	if !strings.Contains(output, "lock.") {
		t.Fatalf("sync --check must report a stale lock diagnostic, got:\n%s", output)
	}
	_, after := readLock(t, lockPath)
	if string(after) != string(before) {
		t.Fatalf("sync --check must write nothing after reporting stale")
	}

	if output, err := executeCommand("sync", "--no-backup"); err != nil {
		t.Fatalf("sync after deselecting must succeed: %v\n%s", err, output)
	}
	doc, data := readLock(t, lockPath)
	for _, registry := range doc.Registries {
		for _, rt := range registry.Roots {
			for _, res := range rt.Resources {
				if res.Kind == "plugin" {
					t.Fatalf("deselected external record must be dropped from the lock:\n%s", data)
				}
			}
		}
	}
	if len(doc.Registries) != 1 {
		t.Fatalf("only the catalog's own record must remain:\n%s", data)
	}
}

// A remote-free layer keeps no lock when its only plugin source is a
// directory format source whose selection names no external entry.
func TestSyncDirectoryCatalogWithoutExternalEntriesWritesNoLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root := pluginLockFixture(t)
	repo := filepath.Join(root, "repo")

	catalogDir := filepath.Join(root, "marketplace")
	writeCatalogRepo(t, catalogDir, "")

	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  team: {directory: "+filepath.ToSlash(catalogDir)+", format: claude-marketplace}\nresources:\n  team:\n    - plugin/local-kit\n")
	withWorkingDir(t, repo)
	output, err := executeCommand("sync", "--no-backup")
	if err != nil {
		t.Fatalf("sync of a directory catalog with no external selection must succeed: %v\n%s", err, output)
	}
	if _, statErr := os.Lstat(filepath.Join(repo, ".agents", "lock.yaml")); !os.IsNotExist(statErr) {
		t.Fatalf("no lock may be written when the layer has no records to lock")
	}
}

// Interim sync writes plugin lock records, delivers non-plugin resources,
// and skips plugin member delivery with plugin.delivery.pending.
func TestSyncDeliversOtherResourcesAndWarnsPendingPluginDelivery(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root := pluginLockFixture(t)
	repo := filepath.Join(root, "repo")

	catalogOrigin, catalogWorking := createBareRepoWithWorkingClone(t, "catalog.git")
	writeCatalogRepo(t, catalogWorking, "")
	commitAndPushAll(t, catalogWorking, "seed catalog")

	skillDir := filepath.Join(root, "skills-src")
	writeSkill(t, skillDir, "tooling", "Tooling baseline", nil)

	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  team: {git: file://"+filepath.ToSlash(catalogOrigin)+", ref: main, format: claude-marketplace}\n  skills-src: {directory: "+filepath.ToSlash(skillDir)+"}\nresources:\n  team:\n    - plugin/local-kit\n  skills-src:\n    - skill/tooling\n")
	withWorkingDir(t, repo)
	output, err := executeCommand("sync", "--no-backup")
	if err != nil {
		t.Fatalf("sync must deliver non-plugin resources despite a pending plugin delivery: %v\n%s", err, output)
	}
	if !strings.Contains(output, "plugin.delivery.pending") || !strings.Contains(output, "local-kit") {
		t.Fatalf("sync must warn plugin.delivery.pending naming the plugin:\n%s", output)
	}
	if _, err := os.Stat(filepath.Join(repo, ".claude", "skills", "tooling", "SKILL.md")); err != nil {
		t.Fatalf("non-plugin resource must be delivered: %v", err)
	}
	if _, statErr := os.Lstat(filepath.Join(repo, ".claude", "skills", "local-skill")); !os.IsNotExist(statErr) {
		t.Fatalf("plugin member files must not be written: %v", statErr)
	}
	doc, _ := readLock(t, filepath.Join(repo, ".agents", "lock.yaml"))
	if plugins := pluginResources(doc); len(plugins) != 1 || plugins[0].Name != "local-kit" {
		t.Fatalf("lock must record the selected plugin: %+v", plugins)
	}
}

// A healthy plugin-selecting lock passes sync --check: plugin member
// delivery is skipped with plugin.delivery.pending, exactly like a writable
// sync, and nothing is written.
func TestSyncCheckSkipsPendingPluginDelivery(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root := pluginLockFixture(t)
	repo := filepath.Join(root, "repo")

	catalogOrigin, catalogWorking := createBareRepoWithWorkingClone(t, "catalog.git")
	writeCatalogRepo(t, catalogWorking, "")
	commitAndPushAll(t, catalogWorking, "seed catalog")

	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  team: {git: file://"+filepath.ToSlash(catalogOrigin)+", ref: main, format: claude-marketplace}\nresources:\n  team:\n    - plugin/local-kit\n")
	withWorkingDir(t, repo)
	if output, err := executeCommand("sync", "--no-backup"); err != nil {
		t.Fatalf("sync of a plugin selection must succeed: %v\n%s", err, output)
	}
	snapshot := func() map[string][]byte {
		files := map[string][]byte{}
		_ = filepath.Walk(repo, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			if rel, rerr := filepath.Rel(repo, path); rerr == nil {
				if data, rerr := os.ReadFile(path); rerr == nil {
					files[rel] = data
				}
			}
			return nil
		})
		return files
	}
	before := snapshot()

	output, err := executeCommand("sync", "--check")
	if err != nil {
		t.Fatalf("sync --check on a healthy plugin lock must succeed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "plugin.delivery.pending") || !strings.Contains(output, "local-kit") {
		t.Fatalf("sync --check must warn plugin.delivery.pending naming the plugin:\n%s", output)
	}
	after := snapshot()
	for rel, data := range before {
		if string(after[rel]) != string(data) {
			t.Fatalf("sync --check must write nothing; %s changed", rel)
		}
	}
	if _, statErr := os.Lstat(filepath.Join(repo, ".claude", "skills", "local-skill")); !os.IsNotExist(statErr) {
		t.Fatalf("plugin member files must not be written by sync --check")
	}
}

// A required-only group is a valid empty selection in v4: the lock records
// and acquires no plugin and no external origin for it.
func TestSyncRequiredOnlyGroupLocksNoPlugin(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root := pluginLockFixture(t)
	repo := filepath.Join(root, "repo")

	entryOrigin, entryWorking := createBareRepoWithWorkingClone(t, "entry.git")
	writeClaudePluginPackage(t, entryWorking, "ext-kit", "ext-skill")
	commitAndPushAll(t, entryWorking, "seed entry")

	catalogOrigin, catalogWorking := createBareRepoWithWorkingClone(t, "catalog.git")
	writeCatalogRepo(t, catalogWorking, entryOrigin)
	commitAndPushAll(t, catalogWorking, "seed catalog")

	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  team: {git: file://"+filepath.ToSlash(catalogOrigin)+", ref: main, format: claude-marketplace}\nresources:\n  team: {required: true}\n")
	withWorkingDir(t, repo)
	if output, err := executeCommand("sync", "--no-backup"); err != nil {
		t.Fatalf("sync of a required-only plugin group must succeed: %v\n%s", err, output)
	}
	doc, data := readLock(t, filepath.Join(repo, ".agents", "lock.yaml"))
	if plugins := pluginResources(doc); len(plugins) != 0 {
		t.Fatalf("required-only group must lock no plugin:\n%s", data)
	}
	if len(doc.Registries) != 1 {
		t.Fatalf("required-only group must acquire no external origin:\n%s", data)
	}
}

// A required group with an include locks exactly the named plugin.
func TestSyncRequiredGroupLocksOnlyIncludedPlugin(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root := pluginLockFixture(t)
	repo := filepath.Join(root, "repo")

	catalogOrigin, catalogWorking := createBareRepoWithWorkingClone(t, "catalog.git")
	writeCatalogRepo(t, catalogWorking, "")
	writeClaudePluginPackage(t, filepath.Join(catalogWorking, "plugins", "extra-kit"), "extra-kit", "extra-skill")
	writeFile(t, filepath.Join(catalogWorking, ".claude-plugin", "marketplace.json"), `{"name":"team","plugins":[`+
		`{"name":"local-kit","source":"./plugins/local-kit"},`+
		`{"name":"extra-kit","source":"./plugins/extra-kit"}]}`)
	commitAndPushAll(t, catalogWorking, "seed catalog")

	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  team: {git: file://"+filepath.ToSlash(catalogOrigin)+", ref: main, format: claude-marketplace}\nresources:\n  team: {required: true, include: [plugin/local-kit]}\n")
	withWorkingDir(t, repo)
	if output, err := executeCommand("sync", "--no-backup"); err != nil {
		t.Fatalf("sync of a required group with an include must succeed: %v\n%s", err, output)
	}
	doc, data := readLock(t, filepath.Join(repo, ".agents", "lock.yaml"))
	if plugins := pluginResources(doc); len(plugins) != 1 || plugins[0].Name != "local-kit" {
		t.Fatalf("lock must record exactly plugin/local-kit:\n%s", data)
	}
	for _, args := range [][]string{{"sync", "--check"}, {"sync", "--locked"}} {
		if output, err := executeCommand(args...); err != nil {
			t.Fatalf("%v must verify the lock: %v\n%s", args, err, output)
		}
	}
}

// A wildcard exclusion on a non-required group empties the plugin selection
// in both the lock and the resolver.
func TestSyncWildcardExclusionLocksNoPlugin(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root := pluginLockFixture(t)
	repo := filepath.Join(root, "repo")

	for _, exclusion := range []string{"*", "plugin/*"} {
		catalogOrigin, catalogWorking := createBareRepoWithWorkingClone(t, "catalog-"+strings.NewReplacer("*", "all", "/", "-").Replace(exclusion)+".git")
		writeCatalogRepo(t, catalogWorking, "")
		commitAndPushAll(t, catalogWorking, "seed catalog")

		writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  team: {git: file://"+filepath.ToSlash(catalogOrigin)+", ref: main, format: claude-marketplace}\nresources:\n  team: {all: true, exclude: ['"+exclusion+"']}\n")
		withWorkingDir(t, repo)
		if output, err := executeCommand("sync", "--no-backup"); err != nil {
			t.Fatalf("sync of exclusion %q must succeed: %v\n%s", exclusion, err, output)
		}
		doc, data := readLock(t, filepath.Join(repo, ".agents", "lock.yaml"))
		if plugins := pluginResources(doc); len(plugins) != 0 {
			t.Fatalf("exclusion %q must lock no plugin:\n%s", exclusion, data)
		}
		for _, args := range [][]string{{"sync", "--check"}, {"sync", "--locked"}} {
			if output, err := executeCommand(args...); err != nil {
				t.Fatalf("%v after exclusion %q must verify the lock:\n%s", args, exclusion, output)
			}
		}
	}
}

// A required group's exclusion does not remove a plugin the resolver selects;
// the lock stays fresh instead of oscillating.
func TestSyncRequiredGroupExclusionKeepsPluginAndStaysFresh(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root := pluginLockFixture(t)
	repo := filepath.Join(root, "repo")

	catalogOrigin, catalogWorking := createBareRepoWithWorkingClone(t, "catalog.git")
	writeCatalogRepo(t, catalogWorking, "")
	commitAndPushAll(t, catalogWorking, "seed catalog")

	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  team: {git: file://"+filepath.ToSlash(catalogOrigin)+", ref: main, format: claude-marketplace}\nresources:\n  team: {all: true, required: true, exclude: [plugin/local-kit]}\n")
	withWorkingDir(t, repo)
	if output, err := executeCommand("sync", "--no-backup"); err != nil {
		t.Fatalf("sync of a required group with an exclusion must succeed: %v\n%s", err, output)
	}
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	doc, before := readLock(t, lockPath)
	if plugins := pluginResources(doc); len(plugins) != 1 || plugins[0].Name != "local-kit" {
		t.Fatalf("required group exclusion must not remove the selected plugin:\n%s", before)
	}
	for _, args := range [][]string{{"sync", "--check"}, {"sync", "--locked"}} {
		if output, err := executeCommand(args...); err != nil {
			t.Fatalf("%v must verify the lock: %v\n%s", args, err, output)
		}
	}
	if output, err := executeCommand("sync", "--no-backup"); err != nil {
		t.Fatalf("second sync must succeed: %v\n%s", err, output)
	}
	_, after := readLock(t, lockPath)
	if string(after) != string(before) {
		t.Fatalf("the lock must stay fresh across syncs:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// writeMixedFormatRepo seeds a repository with a loose skill and a
// claude-marketplace catalog under cat/ carrying one local plugin.
func writeMixedFormatRepo(t *testing.T) (string, string) {
	origin, working := createBareRepoWithWorkingClone(t, "mixed.git")
	writeSkill(t, working, "tooling", "Tooling baseline", nil)
	writeClaudePluginPackage(t, filepath.Join(working, "cat", "plugins", "local-kit"), "local-kit", "local-skill")
	writeFile(t, filepath.Join(working, "cat", ".claude-plugin", "marketplace.json"),
		`{"name":"team","plugins":[{"name":"local-kit","source":"./plugins/local-kit"}]}`)
	return origin, working
}

// A collections/ directory inside a plugin-v1 root is not discovered: format
// sources have no collections, matching verification-side discovery.
func TestSyncMixedRecordFormatRootCollectionsAreNotDiscovered(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root := pluginLockFixture(t)
	repo := filepath.Join(root, "repo")

	origin, working := writeMixedFormatRepo(t)
	collectionBody := "name: bundle\nresources:\n  - {kind: plugin, name: local-kit}\n"
	// The loose copy lets resolution select the collection; the copy inside
	// the plugin-v1 root must not be discovered by the lock path.
	writeCollection(t, working, "bundle", collectionBody)
	writeCollection(t, filepath.Join(working, "cat"), "bundle", collectionBody)
	commitAndPushAll(t, working, "seed mixed record")

	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  cat: {git: file://"+filepath.ToSlash(origin)+", ref: main, path: cat, format: claude-marketplace}\n  loose: {git: file://"+filepath.ToSlash(origin)+", ref: main}\nresources:\n  loose:\n    - skill/tooling\n    - collection/bundle\n")
	withWorkingDir(t, repo)
	if output, err := executeCommand("sync", "--no-backup"); err != nil {
		t.Fatalf("sync of a mixed record must succeed: %v\n%s", err, output)
	}
	for _, args := range [][]string{{"sync", "--check"}, {"sync", "--locked"}} {
		if output, err := executeCommand(args...); err != nil {
			t.Fatalf("%v must verify the lock without a manifest mismatch: %v\n%s", args, err, output)
		}
	}
}

// The shared catalog loader feeds the plugin selection plan: a collections/
// directory inside a format source is never decoded, while a loose source's
// collections become plan edges exactly as resolution sees them.
func TestPlanLoaderSkipsFormatSourceCollections(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	pluginLockFixture(t)

	_, working := writeMixedFormatRepo(t)
	writeCollection(t, working, "loose-bundle", "name: loose-bundle\nresources:\n  - {kind: skill, name: tooling}\n")
	writeCollection(t, filepath.Join(working, "cat"), "cat-bundle", "name: cat-bundle\nresources:\n  - {kind: plugin, name: local-kit}\n")

	cfg := &config.Config{Registries: []config.RegistryEntry{
		{URL: working, V4Kind: config.V4SourceDirectory},
		{URL: filepath.Join(working, "cat"), V4Kind: config.V4SourceDirectory, Format: "claude-marketplace"},
	}}
	loaded := catalog.LoadRegistriesWithContext(context.Background(), cfg, "", catalog.LoadModeCachedNoFetch)
	if len(loaded.Errors) != 0 {
		t.Fatalf("load: %v", loaded.Errors)
	}
	var collections []string
	for _, cat := range loaded.Catalogs {
		for _, entry := range cat.Entries {
			if entry.Kind == ir.KindCollection {
				collections = append(collections, entry.Name)
			}
		}
	}
	if len(collections) != 1 || collections[0] != "loose-bundle" {
		t.Fatalf("collection inside a format source must not be discovered: %v", collections)
	}
}

// An offline writable sync retains the format group and must keep the
// existing external records unchanged.
func TestSyncOfflineKeepsExternalRecords(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root := pluginLockFixture(t)
	repo := filepath.Join(root, "repo")

	entryOrigin, entryWorking := createBareRepoWithWorkingClone(t, "entry.git")
	writeClaudePluginPackage(t, entryWorking, "ext-kit", "ext-skill")
	commitAndPushAll(t, entryWorking, "seed entry")

	catalogOrigin, catalogWorking := createBareRepoWithWorkingClone(t, "catalog.git")
	writeCatalogRepo(t, catalogWorking, entryOrigin)
	commitAndPushAll(t, catalogWorking, "seed catalog")

	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  team: {git: file://"+filepath.ToSlash(catalogOrigin)+", ref: main, format: claude-marketplace}\nresources:\n  team:\n    - plugin/ext-kit\n")
	withWorkingDir(t, repo)
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	if output, err := executeCommand("sync", "--no-backup"); err != nil {
		t.Fatalf("sync of an external plugin selection must succeed: %v\n%s", err, output)
	}
	_, before := readLock(t, lockPath)

	if output, err := executeCommand("sync", "--offline"); err != nil {
		t.Fatalf("sync --offline must succeed on a fresh lock: %v\n%s", err, output)
	}
	_, after := readLock(t, lockPath)
	if string(after) != string(before) {
		t.Fatalf("sync --offline must leave the lock bytes unchanged:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// Two selected catalog entries resolving to the same external origin are
// refused before acquisition and before any lock write.
func TestSyncSharedExternalOriginIsRefused(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root := pluginLockFixture(t)
	repo := filepath.Join(root, "repo")

	entryOrigin, entryWorking := createBareRepoWithWorkingClone(t, "entry.git")
	writeClaudePluginPackage(t, entryWorking, "ext-kit", "ext-skill")
	commitAndPushAll(t, entryWorking, "seed entry")

	catalogDir := filepath.Join(root, "marketplace")
	writeClaudePluginPackage(t, filepath.Join(catalogDir, "plugins", "local-kit"), "local-kit", "local-skill")
	writeFile(t, filepath.Join(catalogDir, ".claude-plugin", "marketplace.json"),
		`{"name":"team","plugins":[`+
			`{"name":"local-kit","source":"./plugins/local-kit"},`+
			`{"name":"ext-a","source":{"source":"url","url":"`+filepath.ToSlash(entryOrigin)+`","ref":"main"}},`+
			`{"name":"ext-b","source":{"source":"url","url":"`+filepath.ToSlash(entryOrigin)+`","ref":"main"}}]}`)

	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  team: {directory: "+filepath.ToSlash(catalogDir)+", format: claude-marketplace}\nresources:\n  team:\n    - plugin/ext-a\n    - plugin/ext-b\n")
	withWorkingDir(t, repo)
	output, err := executeCommand("sync", "--no-backup")
	if err == nil {
		t.Fatalf("selecting two entries that share one external origin must fail:\n%s", output)
	}
	if !strings.Contains(output, "plugin.catalog.shared-origin") || !strings.Contains(output, "ext-a") || !strings.Contains(output, "ext-b") {
		t.Fatalf("refusal must carry the code and name both entries:\n%s", output)
	}
	if _, statErr := os.Stat(filepath.Join(repo, ".agents", "lock.yaml")); !os.IsNotExist(statErr) {
		t.Fatalf("no lock may be written when the selection is refused")
	}
}

// Two catalogs each selecting a different entry of one external repo collapse
// to one lock record; the union check refuses before any lock write.
func TestSyncCrossCatalogSharedExternalOriginRefuses(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root := pluginLockFixture(t)
	repo := filepath.Join(root, "repo")

	entryOrigin, entryWorking := createBareRepoWithWorkingClone(t, "entry.git")
	writeClaudePluginPackage(t, entryWorking, "ext-kit", "ext-skill")
	commitAndPushAll(t, entryWorking, "seed entry")

	catA := filepath.Join(root, "marketplace-a")
	writeClaudePluginPackage(t, filepath.Join(catA, "plugins", "local-a"), "local-a", "local-a-skill")
	writeFile(t, filepath.Join(catA, ".claude-plugin", "marketplace.json"),
		`{"name":"a","plugins":[{"name":"ext-a","source":{"source":"url","url":"`+filepath.ToSlash(entryOrigin)+`","ref":"main"}}]}`)
	catB := filepath.Join(root, "marketplace-b")
	writeClaudePluginPackage(t, filepath.Join(catB, "plugins", "local-b"), "local-b", "local-b-skill")
	writeFile(t, filepath.Join(catB, ".claude-plugin", "marketplace.json"),
		`{"name":"b","plugins":[{"name":"ext-b","source":{"source":"url","url":"`+filepath.ToSlash(entryOrigin)+`","ref":"main"}}]}`)

	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  a: {directory: "+filepath.ToSlash(catA)+", format: claude-marketplace}\n  b: {directory: "+filepath.ToSlash(catB)+", format: claude-marketplace}\nresources:\n  a:\n    - plugin/ext-a\n  b:\n    - plugin/ext-b\n")
	withWorkingDir(t, repo)
	output, err := executeCommand("sync", "--no-backup")
	if err == nil {
		t.Fatalf("two catalogs selecting one external origin must fail:\n%s", output)
	}
	if !strings.Contains(output, "plugin.catalog.shared-origin") || !strings.Contains(output, "ext-a") || !strings.Contains(output, "ext-b") {
		t.Fatalf("refusal must carry the code and name both entries:\n%s", output)
	}
	if _, statErr := os.Stat(filepath.Join(repo, ".agents", "lock.yaml")); !os.IsNotExist(statErr) {
		t.Fatalf("no lock may be written when the selection is refused")
	}
}

// lockHasPluginResource reports whether the lock holds a resource row for
// the named plugin: a selected path plugin appears inside its format root's
// plugin-v1 resources, a selected external entry inside its own record.
func lockHasPluginResource(doc lockfile.Document, name string) bool {
	for i := range doc.Registries {
		for j := range doc.Registries[i].Roots {
			for _, res := range doc.Registries[i].Roots[j].Resources {
				if res.Kind == "plugin" && res.Name == name {
					return true
				}
			}
		}
	}
	return false
}

// A loose source selected implicitly (all:true) contributes a collection
// naming a catalog plugin; the lock commits the collection-selected plugin
// through the same selection the resolver performs, and read-only modes
// recompute it identically.
func TestSyncLooseAllCollectionLocksPlugin(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root := pluginLockFixture(t)
	repo := filepath.Join(root, "repo")

	formatOrigin, formatWorking := createBareRepoWithWorkingClone(t, "cat.git")
	writeCatalogRepo(t, formatWorking, "")
	commitAndPushAll(t, formatWorking, "seed catalog")

	looseOrigin, looseWorking := createBareRepoWithWorkingClone(t, "loose.git")
	writeSkill(t, looseWorking, "tooling", "Tooling baseline", nil)
	writeCollection(t, looseWorking, "team", "name: team\nresources:\n  - {kind: plugin, name: local-kit, registry: file://"+filepath.ToSlash(formatOrigin)+"}\n")
	commitAndPushAll(t, looseWorking, "seed loose")

	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  cat: {git: file://"+filepath.ToSlash(formatOrigin)+", ref: main, format: claude-marketplace}\n  loose: {git: file://"+filepath.ToSlash(looseOrigin)+", ref: main}\nresources:\n  loose:\n    all: true\n")
	withWorkingDir(t, repo)
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	if output, err := executeCommand("sync", "--no-backup"); err != nil {
		t.Fatalf("sync of a collection-bound plugin must succeed: %v\n%s", err, output)
	}
	doc, data := readLock(t, lockPath)
	var manifest *lockfile.GitRoot
	for i := range doc.Registries {
		for j := range doc.Registries[i].Roots {
			if doc.Registries[i].Roots[j].RevisionSchema == "plugin-v1" && len(doc.Registries[i].Roots[j].Resources) > 0 {
				manifest = &doc.Registries[i].Roots[j]
			}
		}
	}
	if manifest == nil {
		t.Fatalf("the implicitly selected collection must lock the plugin it names:\n%s", data)
	}
	for _, args := range [][]string{{"sync", "--check"}, {"sync", "--locked"}} {
		if output, err := executeCommand(args...); err != nil {
			t.Fatalf("%v must recompute the same selection and verify: %v\n%s", args, err, output)
		}
	}
}

// A collection member pinned to a ref the format source does not track binds
// nothing: the plugin must not be locked, exactly as resolution drops the
// member.
func TestSyncCollectionMemberRefMismatchLocksNoPlugin(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root := pluginLockFixture(t)
	repo := filepath.Join(root, "repo")

	entryOrigin, entryWorking := createBareRepoWithWorkingClone(t, "entry.git")
	writeClaudePluginPackage(t, entryWorking, "ext-kit", "ext-skill")
	commitAndPushAll(t, entryWorking, "seed entry")

	formatOrigin, formatWorking := createBareRepoWithWorkingClone(t, "cat.git")
	writeCatalogRepo(t, formatWorking, entryOrigin)
	commitAndPushAll(t, formatWorking, "seed catalog")

	looseOrigin, looseWorking := createBareRepoWithWorkingClone(t, "loose.git")
	writeSkill(t, looseWorking, "tooling", "Tooling baseline", nil)
	writeCollection(t, looseWorking, "team", "name: team\nresources:\n  - {kind: plugin, name: local-kit, registry: file://"+filepath.ToSlash(formatOrigin)+"}\n  - {kind: plugin, name: ext-kit, registry: file://"+filepath.ToSlash(formatOrigin)+", ref: other}\n")
	commitAndPushAll(t, looseWorking, "seed loose")

	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  cat: {git: file://"+filepath.ToSlash(formatOrigin)+", ref: main, format: claude-marketplace}\n  loose: {git: file://"+filepath.ToSlash(looseOrigin)+", ref: main}\nresources:\n  loose:\n    - collection/team\n")
	withWorkingDir(t, repo)
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	if output, err := executeCommand("sync", "--no-backup"); err != nil {
		t.Fatalf("sync must succeed with the unpinned miss as a warning: %v\n%s", err, output)
	}
	doc, data := readLock(t, lockPath)
	if !lockHasPluginResource(doc, "local-kit") {
		t.Fatalf("the collection-bound plugin must be locked:\n%s", data)
	}
	if lockHasPluginResource(doc, "ext-kit") {
		t.Fatalf("a member pinned to a ref the source does not track must not be locked:\n%s", data)
	}
	for _, args := range [][]string{{"sync", "--check"}, {"sync", "--locked"}} {
		if output, err := executeCommand(args...); err != nil {
			t.Fatalf("%v must verify the same selection: %v\n%s", args, err, output)
		}
	}
}

// A required collection member that names a plugin missing from the bound
// catalog surfaces the resolver's not-found error instead of silently
// committing a manifest without it.
func TestSyncCollectionMemberMissingFromCatalogRefuses(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root := pluginLockFixture(t)
	repo := filepath.Join(root, "repo")

	formatOrigin, formatWorking := createBareRepoWithWorkingClone(t, "cat.git")
	writeCatalogRepo(t, formatWorking, "")
	commitAndPushAll(t, formatWorking, "seed catalog")

	looseOrigin, looseWorking := createBareRepoWithWorkingClone(t, "loose.git")
	writeSkill(t, looseWorking, "tooling", "Tooling baseline", nil)
	writeCollection(t, looseWorking, "team", "name: team\nresources:\n  - {kind: plugin, name: ghost-kit, registry: file://"+filepath.ToSlash(formatOrigin)+"}\n")
	commitAndPushAll(t, looseWorking, "seed loose")

	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  cat: {git: file://"+filepath.ToSlash(formatOrigin)+", ref: main, format: claude-marketplace}\n  loose: {git: file://"+filepath.ToSlash(looseOrigin)+", ref: main}\nresources:\n  cat: {all: true, required: true}\n  loose:\n    - collection/team\n")
	withWorkingDir(t, repo)
	output, err := executeCommand("sync", "--no-backup")
	if err == nil {
		t.Fatalf("a required collection member missing from the catalog must fail:\n%s", output)
	}
	if !strings.Contains(output, "ghost-kit") {
		t.Fatalf("the error must name the missing member:\n%s", output)
	}
	if _, statErr := os.Stat(filepath.Join(repo, ".agents", "lock.yaml")); !os.IsNotExist(statErr) {
		t.Fatalf("no lock may be written when the selection fails")
	}
}

// A plugin reached only through a nested collection include is locked,
// exactly as resolution walks includes.
func TestSyncNestedCollectionIncludeLocksPlugin(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root := pluginLockFixture(t)
	repo := filepath.Join(root, "repo")

	entryOrigin, entryWorking := createBareRepoWithWorkingClone(t, "entry.git")
	writeClaudePluginPackage(t, entryWorking, "ext-kit", "ext-skill")
	commitAndPushAll(t, entryWorking, "seed entry")

	formatOrigin, formatWorking := createBareRepoWithWorkingClone(t, "cat.git")
	writeCatalogRepo(t, formatWorking, entryOrigin)
	commitAndPushAll(t, formatWorking, "seed catalog")

	looseOrigin, looseWorking := createBareRepoWithWorkingClone(t, "loose.git")
	writeSkill(t, looseWorking, "tooling", "Tooling baseline", nil)
	writeCollection(t, looseWorking, "outer", "name: outer\nincludes:\n  - {name: inner}\n")
	writeCollection(t, looseWorking, "inner", "name: inner\nresources:\n  - {kind: plugin, name: ext-kit, registry: file://"+filepath.ToSlash(formatOrigin)+"}\n")
	commitAndPushAll(t, looseWorking, "seed loose")

	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  cat: {git: file://"+filepath.ToSlash(formatOrigin)+", ref: main, format: claude-marketplace}\n  loose: {git: file://"+filepath.ToSlash(looseOrigin)+", ref: main}\nresources:\n  loose:\n    all: true\n")
	withWorkingDir(t, repo)
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	if output, err := executeCommand("sync", "--no-backup"); err != nil {
		t.Fatalf("sync of a nested-collection plugin must succeed: %v\n%s", err, output)
	}
	doc, data := readLock(t, lockPath)
	if !lockHasPluginResource(doc, "ext-kit") {
		t.Fatalf("a plugin reached through a nested include must be locked:\n%s", data)
	}
	for _, args := range [][]string{{"sync", "--check"}, {"sync", "--locked"}} {
		if output, err := executeCommand(args...); err != nil {
			t.Fatalf("%v must recompute the same selection and verify: %v\n%s", args, err, output)
		}
	}
}
