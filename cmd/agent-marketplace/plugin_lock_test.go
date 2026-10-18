package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
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
	_, before := readLock(t, lockPath)

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
	_ = before
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

// F3(a): removing a package-format git source after locking must report the
// lock stale — its record is a configured-source record, not an external
// record exempt from the identity match.
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

// F3(b): deselecting a plugin whose catalog entry is external must report
// the lock stale; the next plain sync drops the external record.
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

// F6: a remote-free layer keeps no lock when its only plugin source is a
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

// AC-d: interim sync writes plugin lock records, delivers non-plugin
// resources, and skips plugin member delivery with plugin.delivery.pending.
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
