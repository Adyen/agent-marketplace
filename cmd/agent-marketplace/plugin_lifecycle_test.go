package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/processstart"
)

func configurePluginResources(t *testing.T, repo, resources string) {
	t.Helper()
	catalogDir := filepath.Join(filepath.Dir(repo), "marketplace")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  team: {directory: "+
			filepath.ToSlash(catalogDir)+", format: claude-marketplace}\nresources:\n  team:\n"+resources)
	withWorkingDir(t, repo)
}

func TestListKindPluginFiltersPluginsAndAcceptsPluginName(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repo, run := pluginCommandFixture(t, "    - plugin/review-kit\n")
	looseDir := filepath.Join(filepath.Dir(repo), "loose")
	writeSkill(t, looseDir, "tooling", "Tooling", nil)
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  team: {directory: "+
			filepath.ToSlash(filepath.Join(filepath.Dir(repo), "marketplace"))+", format: claude-marketplace}\n  loose: {directory: "+
			filepath.ToSlash(looseDir)+"}\nresources:\n  team:\n    - plugin/review-kit\n  loose:\n    - skill/tooling\n")

	output, err := run("list", "--kind", "plugin")
	if err != nil {
		t.Fatalf("list --kind plugin failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "plugin") || !strings.Contains(output, "review-kit") {
		t.Fatalf("plugin listing is missing:\n%s", output)
	}
	if strings.Contains(output, "tooling") {
		t.Fatalf("plugin filter included a non-plugin resource:\n%s", output)
	}

	help, err := run("list", "--help")
	if err != nil || !strings.Contains(help, "plugin") {
		t.Fatalf("list --kind help omits plugin: %v\n%s", err, help)
	}

	output, err = run("list", "--kind", "not-a-kind")
	if err == nil || !strings.Contains(output+err.Error(), "plugin") {
		t.Fatalf("unknown-kind diagnostic must list plugin: err=%v\n%s", err, output)
	}

	writeFile(t, filepath.Join(filepath.Dir(repo), "marketplace", ".claude-plugin", "marketplace.json"),
		`{"name":"team","plugins":[]}`)
	configurePluginResources(t, repo, "    include: []\n")
	output, err = run("list", "--kind", "plugin")
	if err != nil || strings.Contains(output, "review-kit") {
		t.Fatalf("empty plugin listing = %v\n%s", err, output)
	}
}

func TestInspectPluginReportsPackageDetailsWithoutWriting(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repo, run := pluginCommandFixture(t, "    - plugin/review-kit\n")
	root := filepath.Dir(repo)
	pluginDir := filepath.Join(root, "marketplace", "plugins", "review-kit")
	writeFile(t, filepath.Join(pluginDir, ".claude-plugin", "plugin.json"),
		`{"name":"review-kit","description":"Review workflows","version":"2.1.0","dependencies":["base-kit"]}`)
	addUnmappableDevinHook(t, pluginDir)
	writeFile(t, filepath.Join(pluginDir, "scripts", "check.sh"), "#!/bin/sh\n")
	if err := os.Chmod(filepath.Join(pluginDir, "scripts", "check.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, filepath.Join(root, "home", ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nharnesses: [claude, devin]\n")
	badSkill := filepath.Join(pluginDir, "skills", "unsupported")
	if err := os.MkdirAll(badSkill, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(badSkill, "SKILL.md"), "---\nname: \"Invalid Name!!\"\n---\n")
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}

	output, err := run("inspect", "plugin/review-kit")
	if err != nil {
		t.Fatalf("inspect plugin failed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"review-kit", "Review workflows", filepath.ToSlash(filepath.Join(root, "marketplace")),
		"claude-plugin", "REVISION", "base-kit", "check.sh", "unsupported", "TARGET DELIVERY", "LOSSES", "BLOCKERS",
		"hook.devin.event-unmappable", "plugin.format.dependencies-unsupported",
		"claude: blocked (plugin.format.dependencies-unsupported)",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("inspection omitted %q:\n%s", want, output)
		}
	}
	after, err := os.ReadFile(configPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("inspection changed config: read error=%v\nbefore=%s\nafter=%s", err, before, after)
	}
	if _, err := os.Lstat(filepath.Join(repo, ".agents", "lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("inspection wrote an acquisition lock: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(repo, ".claude")); !os.IsNotExist(err) {
		t.Fatalf("inspection wrote harness output: %v", err)
	}
}

func TestInspectPluginShowsNativeDeliveryPending(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, run := pluginCommandFixture(t, "    - {kind: plugin, name: review-kit, delivery: native}\n")
	output, err := run("inspect", "plugin/review-kit")
	if err != nil {
		t.Fatalf("inspect native plugin: %v\n%s", err, output)
	}
	if !strings.Contains(output, "claude: native (pending)") {
		t.Fatalf("inspect must report native delivery as pending:\n%s", output)
	}
}

func TestInspectRejectsUnknownPluginAndNonPluginLocator(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	_, run := pluginCommandFixture(t, "    - plugin/review-kit\n")
	for _, locator := range []string{"plugin/missing", "skill/review"} {
		output, err := run("inspect", locator)
		if err == nil {
			t.Errorf("inspect %q succeeded:\n%s", locator, output)
		}
	}
}

// spec: plugin-packages/install-writes-a-plugin-request
func TestInstallPluginWritesRequestAndSyncsIt(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repo, run := pluginCommandFixture(t, "    include: []\n")

	output, err := run("install", "plugin/review-kit")
	if err != nil {
		t.Fatalf("install plugin failed: %v\n%s", err, output)
	}
	configBytes, err := os.ReadFile(filepath.Join(repo, ".agents", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(configBytes), "plugin/review-kit") {
		t.Fatalf("install did not write a plugin request:\n%s", configBytes)
	}
	member := filepath.Join(repo, ".claude", "skills", "review-kit-review", "SKILL.md")
	if _, err := os.Stat(member); err != nil {
		t.Fatalf("install did not sync the plugin member: %v\n%s", err, output)
	}
}

func TestInstallUnknownPluginLeavesConfigAndLockUnchanged(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repo, run := pluginCommandFixture(t, "    include: []\n")
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}

	output, err := run("install", "plugin/missing")
	if err == nil {
		t.Fatalf("unknown plugin install succeeded:\n%s", output)
	}
	after, readErr := os.ReadFile(configPath)
	if readErr != nil || !bytes.Equal(before, after) {
		t.Fatalf("unknown plugin install changed config: %v\nbefore=%s\nafter=%s", readErr, before, after)
	}
	if _, statErr := os.Lstat(filepath.Join(repo, ".agents", "lock.yaml")); !os.IsNotExist(statErr) {
		t.Fatalf("unknown plugin install wrote a lock: %v", statErr)
	}
}

// spec: plugin-packages/remove-retires-the-plugin-only-on-its-last-selection
func TestRemovePluginKeepsCollectionSelectedPluginUntilCollectionRemoval(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root := pluginLockFixture(t)
	repo := filepath.Join(root, "repo")
	catalogOrigin, catalogWorking := createBareRepoWithWorkingClone(t, "plugin-catalog.git")
	writeCatalogRepo(t, catalogWorking, "")
	commitAndPushAll(t, catalogWorking, "seed plugin")
	collectionOrigin, collectionWorking := createBareRepoWithWorkingClone(t, "plugin-collection.git")
	writeSkill(t, collectionWorking, "unused", "Collection registry layout", nil)
	writeCollection(t, collectionWorking, "review-team",
		"name: review-team\nresources:\n  - {kind: plugin, name: local-kit, registry: file://"+filepath.ToSlash(catalogOrigin)+"}\n")
	commitAndPushAll(t, collectionWorking, "seed collection")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  team: {git: file://"+
			filepath.ToSlash(catalogOrigin)+", ref: main, format: claude-marketplace}\n  bundles: {git: file://"+
			filepath.ToSlash(collectionOrigin)+", ref: main}\nresources:\n  team:\n    - plugin/local-kit\n  bundles:\n    - collection/review-team\n")
	withWorkingDir(t, repo)
	if output, err := executeCommand("sync", "--no-backup"); err != nil {
		t.Fatalf("initial plugin sync failed: %v\n%s", err, output)
	}
	member := filepath.Join(repo, ".claude", "skills", "local-kit-local-skill", "SKILL.md")
	if _, err := os.Stat(member); err != nil {
		t.Fatalf("initial plugin member missing: %v", err)
	}

	if output, err := executeCommand("remove", "local-kit", "--kind", "plugin"); err != nil {
		t.Fatalf("direct plugin removal failed: %v\n%s", err, output)
	}
	if _, err := os.Stat(member); err != nil {
		t.Fatalf("collection selection should keep the plugin installed: %v", err)
	}
	if output, err := executeCommand("remove", "review-team", "--kind", "collection"); err != nil {
		t.Fatalf("collection removal failed: %v\n%s", err, output)
	}
	if _, err := os.Lstat(member); !os.IsNotExist(err) {
		configBytes, readErr := os.ReadFile(filepath.Join(repo, ".agents", "config.yaml"))
		t.Fatalf("last plugin selection was removed but its member remains: %v; config read=%v\n%s", err, readErr, configBytes)
	}
}

func TestUpdateAndSyncCheckAcceptPluginSelections(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root := pluginLockFixture(t)
	repo := filepath.Join(root, "repo")
	catalogOrigin, catalogWorking := createBareRepoWithWorkingClone(t, "plugin-update.git")
	writeCatalogRepo(t, catalogWorking, "")
	commitAndPushAll(t, catalogWorking, "seed plugin")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  team: {git: file://"+
			filepath.ToSlash(catalogOrigin)+", ref: main, format: claude-marketplace}\nresources:\n  team:\n    - plugin/local-kit\n")
	withWorkingDir(t, repo)
	if output, err := executeCommand("sync", "--no-backup"); err != nil {
		t.Fatalf("plugin sync failed: %v\n%s", err, output)
	}
	if output, err := executeCommand("sync", "--check"); err != nil {
		t.Fatalf("healthy plugin sync --check failed: %v\n%s", err, output)
	}
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	oldCommitBytes, err := exec.Command("git", "-C", catalogWorking, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	oldCommit := strings.TrimSpace(string(oldCommitBytes))
	writeFile(t, filepath.Join(catalogWorking, "plugins", "local-kit", "skills", "local-skill", "SKILL.md"),
		"---\nname: local-skill\ndescription: updated plugin member\n---\n\nUpdated body.\n")
	commitAndPushAll(t, catalogWorking, "advance plugin member")
	newCommitBytes, err := exec.Command("git", "-C", catalogWorking, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	newCommit := strings.TrimSpace(string(newCommitBytes))
	if newCommit == oldCommit {
		t.Fatal("upstream plugin commit did not advance")
	}
	if output, err := executeCommand("update", "--registry", "file://"+filepath.ToSlash(catalogOrigin), "--kind", "plugin"); err != nil {
		t.Fatalf("plugin update failed: %v\n%s", err, output)
	}
	advancedLock, err := os.ReadFile(lockPath)
	if err != nil || !strings.Contains(string(advancedLock), newCommit) || strings.Contains(string(advancedLock), oldCommit) {
		t.Fatalf("plugin update did not advance the lock commit: err=%v\n%s", err, advancedLock)
	}
	configBefore, err := os.ReadFile(filepath.Join(repo, ".agents", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	member := filepath.Join(repo, ".claude", "skills", "local-kit-local-skill", "SKILL.md")
	if err := os.Remove(member); err != nil {
		t.Fatalf("create plugin member drift: %v", err)
	}
	harnessSnapshot := func() map[string][]byte {
		files := make(map[string][]byte)
		err := filepath.Walk(filepath.Join(repo, ".claude"), func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(repo, path)
			if err != nil {
				return err
			}
			files[rel] = data
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return files
	}
	harnessBefore := harnessSnapshot()
	output, err := executeCommand("sync", "--check")
	if err == nil || !strings.Contains(output, "local-kit-local-skill") {
		t.Fatalf("sync --check must report plugin member drift: err=%v\n%s", err, output)
	}
	configAfter, configErr := os.ReadFile(filepath.Join(repo, ".agents", "config.yaml"))
	lockAfter, lockErr := os.ReadFile(lockPath)
	if configErr != nil || lockErr != nil || !bytes.Equal(configBefore, configAfter) || !bytes.Equal(advancedLock, lockAfter) {
		t.Fatalf("sync --check changed config or lock: configErr=%v lockErr=%v\nbefore config=%s\nafter config=%s\nbefore lock=%s\nafter lock=%s", configErr, lockErr, configBefore, configAfter, advancedLock, lockAfter)
	}
	if harnessAfter := harnessSnapshot(); !reflect.DeepEqual(harnessBefore, harnessAfter) {
		t.Fatalf("sync --check changed harness files: before=%v after=%v", harnessBefore, harnessAfter)
	}
	if _, statErr := os.Lstat(member); !os.IsNotExist(statErr) {
		t.Fatalf("sync --check recreated the drifted plugin member: %v", statErr)
	}
}

// spec: plugin-packages/plugin-with-dependencies-is-refused
func TestPluginWithDependenciesIsInspectableButNotInstalled(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repo, run := pluginCommandFixture(t, "    - plugin/review-kit\n")
	root := filepath.Dir(repo)
	catalogDir := filepath.Join(root, "marketplace")
	writeFile(t, filepath.Join(catalogDir, "plugins", "review-kit", ".claude-plugin", "plugin.json"),
		`{"name":"review-kit","dependencies":["base-kit"]}`)
	writePluginSourceFixture(t, filepath.Join(catalogDir, "plugins", "good-kit"), ".claude-plugin", "good-kit", "okay")
	writeFile(t, filepath.Join(catalogDir, ".claude-plugin", "marketplace.json"),
		`{"name":"team","plugins":[{"name":"review-kit","source":"./plugins/review-kit"},{"name":"good-kit","source":"./plugins/good-kit"}]}`)
	looseDir := filepath.Join(root, "loose")
	writeSkill(t, looseDir, "tooling", "Tooling", nil)
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  team: {directory: "+
			filepath.ToSlash(catalogDir)+", format: claude-marketplace}\n  loose: {directory: "+
			filepath.ToSlash(looseDir)+"}\nresources:\n  team:\n    - plugin/review-kit\n    - plugin/good-kit\n  loose:\n    - skill/tooling\n")
	withWorkingDir(t, repo)

	inspection, err := run("inspect", "plugin/review-kit")
	if err != nil || !strings.Contains(inspection, "base-kit") {
		t.Fatalf("dependency-declaring plugin must remain inspectable: %v\n%s", err, inspection)
	}
	output, err := run("sync", "--no-backup")
	if err == nil || !strings.Contains(output, "plugin.format.dependencies-unsupported") {
		t.Fatalf("sync must refuse dependencies with the dedicated diagnostic: %v\n%s", err, output)
	}
	for _, path := range []string{
		filepath.Join(repo, ".claude", "skills", "review-kit-review", "SKILL.md"),
	} {
		if _, statErr := os.Lstat(path); !os.IsNotExist(statErr) {
			t.Errorf("dependency-refused plugin member was installed at %s: %v", path, statErr)
		}
	}
	for _, path := range []string{
		filepath.Join(repo, ".claude", "skills", "good-kit-okay", "SKILL.md"),
		filepath.Join(repo, ".claude", "skills", "tooling", "SKILL.md"),
	} {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Errorf("independent resource %s was not installed: %v", path, statErr)
		}
	}
}

// spec: plugin-packages/plugin-lifecycle-runs-no-package-process
func TestPluginLifecycleDoesNotRunPackageScripts(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repo, run := pluginCommandFixture(t, "    - plugin/review-kit\n")
	root := filepath.Dir(repo)
	pluginDir := filepath.Join(root, "marketplace", "plugins", "review-kit")
	marker := filepath.Join(root, "ran-package-process")
	script := filepath.Join(pluginDir, "scripts", "run.sh")
	writeFile(t, script, "#!/bin/sh\nprintf ran > "+marker+"\n")
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(pluginDir, "hooks", "hooks.json"),
		`{"PreToolUse":[{"matcher":"*","hooks":[{"type":"command","command":"`+script+`"}]}]}`)

	original := processstart.Command
	var starts []string
	processstart.Command = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		starts = append(starts, name)
		return exec.CommandContext(ctx, name, args...)
	}
	defer func() { processstart.Command = original }()

	for _, args := range [][]string{
		{"list"}, {"inspect", "plugin/review-kit"}, {"lock", "--check"},
		{"sync", "--no-backup"}, {"status"},
	} {
		output, err := run(args...)
		if err != nil {
			t.Fatalf("%v failed: %v\n%s", args, err, output)
		}
	}
	if len(starts) == 0 {
		t.Fatal("process-start recorder observed no Git acquisition")
	}
	for _, name := range starts {
		if filepath.Base(name) != "git" {
			t.Errorf("unexpected subprocess %q; starts=%v", name, starts)
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("plugin script ran during lifecycle operations: %v", err)
	}
}

// spec: plugin-packages/status-separates-locked-and-installed-revisions
func TestStatusSeparatesPluginLockedAndInstalledRevisionsAfterBlockedUpdate(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	root := pluginLockFixture(t)
	repo := filepath.Join(root, "repo")
	origin, working := createBareRepoWithWorkingClone(t, "plugin-status.git")
	writeCatalogRepo(t, working, "")
	commitAndPushAll(t, working, "initial plugin")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nharnesses: [devin]\nsources:\n  team: {git: file://"+
			filepath.ToSlash(origin)+", ref: main, format: claude-marketplace}\nresources:\n  team:\n    - plugin/local-kit\n")
	withWorkingDir(t, repo)
	if output, err := executeCommand("sync", "--no-backup"); err != nil {
		t.Fatalf("initial plugin sync failed: %v\n%s", err, output)
	}
	oldCommitBytes, err := exec.Command("git", "-C", working, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	oldCommit := strings.TrimSpace(string(oldCommitBytes))
	addUnmappableDevinHook(t, filepath.Join(working, "plugins", "local-kit"))
	commitAndPushAll(t, working, "add blocked hook")
	newCommitBytes, err := exec.Command("git", "-C", working, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	newCommit := strings.TrimSpace(string(newCommitBytes))
	if output, err := executeCommand("update"); err == nil {
		t.Fatalf("update with an unmappable plugin member succeeded:\n%s", output)
	}

	output, err := executeCommand("status")
	report := output
	if err != nil {
		report += err.Error()
	}
	if err != nil && !strings.Contains(report, "hook.devin.event-unmappable") {
		t.Fatalf("status failed: %v\n%s", err, output)
	}
	for _, want := range []string{oldCommit, newCommit, "hook.devin.event-unmappable"} {
		if !strings.Contains(report, want) {
			t.Errorf("status omitted %q:\n%s", want, report)
		}
	}
	states, err := ownership.ReadPluginInstanceStates()
	if err != nil {
		t.Fatal(err)
	}
	var statePath string
	for _, state := range states {
		if state.Name == "local-kit" && state.Target == "devin" {
			statePath, err = ownership.PluginStatePath(state.InstanceKey)
			break
		}
	}
	if err != nil || statePath == "" {
		t.Fatalf("find installed plugin state: %v", err)
	}
	stateBytes, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var legacy map[string]any
	if err := json.Unmarshal(stateBytes, &legacy); err != nil {
		t.Fatal(err)
	}
	delete(legacy["instance"].(map[string]any), "source_revision")
	legacyBytes, err := json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, append(legacyBytes, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	output, err = executeCommand("status")
	if err != nil && !strings.Contains(output+err.Error(), "hook.devin.event-unmappable") {
		t.Fatalf("status with older ownership state failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "INSTALLED REVISION") || !strings.Contains(output, "unknown") {
		t.Fatalf("status did not provide a clear fallback for ownership state without source_revision:\n%s", output)
	}
}
