package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeOverlayPluginPackage seeds a claude-plugin package whose entry carries
// overlay component paths: one skill appended under more/extra.
func writeOverlayPluginPackage(t *testing.T, dir, name string) {
	t.Helper()
	writePluginSourceFixture(t, dir, ".claude-plugin", name, "base")
	extraDir := filepath.Join(dir, "more", "extra")
	if err := os.MkdirAll(extraDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extraDir, "SKILL.md"), []byte("---\nname: extra\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// spec: plugin-packages/relative-entry-resolves-against-the-catalog-root
func TestPluginCatalogStrictEntryOverlayAppliesThroughListAndLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root := pluginLockFixture(t)
	repo := filepath.Join(root, "repo")

	catalogOrigin, catalogWorking := createBareRepoWithWorkingClone(t, "catalog.git")
	writeOverlayPluginPackage(t, filepath.Join(catalogWorking, "plugins", "kit"), "kit")
	writePluginSourceFixture(t, filepath.Join(catalogWorking, "plugins", "conflict"), ".claude-plugin", "conflict", "conflict-skill")
	writeFile(t, filepath.Join(catalogWorking, ".claude-plugin", "marketplace.json"),
		`{"name":"team","plugins":[`+
			`{"name":"kit","source":"./plugins/kit","strict":true,"skills":["./more/extra"]},`+
			`{"name":"conflict","source":"./plugins/conflict","strict":false,"skills":["./more/extra"]}]}`)
	commitAndPushAll(t, catalogWorking, "seed catalog")

	// Selecting the strict entry with an exclusion naming only the
	// overlaid member proves the member set carries it: an unknown member
	// exclusion is refused before any write.
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  team: {git: file://"+filepath.ToSlash(catalogOrigin)+", ref: main, format: claude-marketplace}\nresources:\n  team:\n    - {kind: plugin, name: kit, exclude: [skill/extra]}\n")
	withWorkingDir(t, repo)

	if output, err := executeCommand("sync", "--no-backup"); err != nil {
		t.Fatalf("sync of a strict overlay entry must succeed: %v\n%s", err, output)
	}
	doc, _ := readLock(t, filepath.Join(repo, ".agents", "lock.yaml"))
	plugins := pluginResources(doc)
	if len(plugins) != 1 || plugins[0].Name != "kit" {
		t.Fatalf("lock must carry exactly one plugin record for kit: %+v", plugins)
	}

	output, err := executeCommand("list")
	if err != nil {
		t.Fatalf("list failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "kit") || !strings.Contains(output, "conflict") {
		t.Fatalf("list must show both entries:\n%s", output)
	}
	if !strings.Contains(output, "plugin.catalog.manifest-conflict") {
		t.Fatalf("the strict:false entry with component fields must list as manifest-conflict:\n%s", output)
	}

	// Selecting the conflicting entry fails with the refusal code while the
	// sibling stays installable.
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  team: {git: file://"+filepath.ToSlash(catalogOrigin)+", ref: main, format: claude-marketplace}\nresources:\n  team:\n    - plugin/conflict\n")
	output, err = executeCommand("sync", "--no-backup")
	if err == nil || !strings.Contains(output, "plugin.catalog.manifest-conflict") {
		t.Fatalf("selecting the conflicting entry must fail with plugin.catalog.manifest-conflict: err=%v\n%s", err, output)
	}
}

// spec: plugin-packages/changed-catalog-entry-makes-the-lock-stale
func TestSyncChangedEntryComponentFieldMakesLockStale(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root := pluginLockFixture(t)
	repo := filepath.Join(root, "repo")

	entryOrigin, entryWorking := createBareRepoWithWorkingClone(t, "entry.git")
	writeClaudePluginPackage(t, entryWorking, "ext-kit", "ext-skill")
	commitAndPushAll(t, entryWorking, "seed entry")

	catalogDir := filepath.Join(root, "marketplace")
	writeCatalogRepo(t, catalogDir, "")
	writeFile(t, filepath.Join(catalogDir, ".claude-plugin", "marketplace.json"),
		`{"name":"team","plugins":[`+
			`{"name":"local-kit","source":"./plugins/local-kit"},`+
			`{"name":"ext-kit","source":{"source":"url","url":"`+filepath.ToSlash(entryOrigin)+`","ref":"main"},"skills":["./more/extra"]}]}`)

	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  team: {directory: "+filepath.ToSlash(catalogDir)+", format: claude-marketplace}\nresources:\n  team:\n    - plugin/ext-kit\n")
	withWorkingDir(t, repo)
	if output, err := executeCommand("sync", "--no-backup"); err != nil {
		t.Fatalf("sync of a directory-catalog external plugin must succeed: %v\n%s", err, output)
	}

	// Edit only the selected entry's component field; the overlay digest the
	// lock commits covers component fields, so check mode must report the
	// record stale.
	writeFile(t, filepath.Join(catalogDir, ".claude-plugin", "marketplace.json"),
		`{"name":"team","plugins":[`+
			`{"name":"local-kit","source":"./plugins/local-kit"},`+
			`{"name":"ext-kit","source":{"source":"url","url":"`+filepath.ToSlash(entryOrigin)+`","ref":"main"},"skills":["./more/other"]}]}`)
	output, err := executeCommand("sync", "--check")
	if err == nil || !strings.Contains(output, "lock.") {
		t.Fatalf("sync --check after editing an entry component field must report the lock stale: err=%v\n%s", err, output)
	}
}
