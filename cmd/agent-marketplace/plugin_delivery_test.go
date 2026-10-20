package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type pluginGenerationSnapshot struct {
	data []byte
	mode os.FileMode
}

func snapshotPluginGeneration(t *testing.T, root string) map[string]pluginGenerationSnapshot {
	t.Helper()
	out := make(map[string]pluginGenerationSnapshot)
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = pluginGenerationSnapshot{data: data, mode: info.Mode().Perm()}
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot package generation %s: %v", root, err)
	}
	return out
}

func configurePluginDeliveryFixture(t *testing.T, repo, harnesses, selections string) {
	t.Helper()
	root := filepath.Dir(repo)
	catalogDir := filepath.Join(root, "marketplace")
	writeConfig(t, filepath.Join(root, "home", ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nharnesses: ["+harnesses+"]\n")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  team: {directory: "+
			filepath.ToSlash(catalogDir)+", format: claude-marketplace}\n"+selections)
	withWorkingDir(t, repo)
}

func addUnmappableDevinHook(t *testing.T, pluginDir string) {
	t.Helper()
	addPluginHook(t, pluginDir, "Notification", "echo notify")
}

func addPluginHook(t *testing.T, pluginDir, event, command string) {
	t.Helper()
	hooksDir := filepath.Join(pluginDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(hooksDir, "hooks.json"),
		`{"`+event+`":[{"matcher":"Bash","hooks":[{"type":"command","command":"`+command+`"}]}]}`)
}

// spec: plugin-delivery/a-failing-member-blocks-the-whole-instance-update
func TestPluginInstanceKeepsPreviousProjectionWhenOneMemberFails(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repo, run := pluginCommandFixture(t, "    - plugin/review-kit\n")
	configurePluginDeliveryFixture(t, repo, "devin", "resources:\n  team:\n    - plugin/review-kit\n")
	skillPath := filepath.Join(repo, ".devin", "skills", "review-kit-review", "SKILL.md")
	if output, err := run("sync", "--no-backup"); err != nil {
		t.Fatalf("initial plugin installation failed: %v\n%s", err, output)
	}
	previous, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("initial plugin member missing: %v", err)
	}

	addUnmappableDevinHook(t, filepath.Join(filepath.Dir(repo), "marketplace", "plugins", "review-kit"))
	output, err := run("sync", "--no-backup")
	if err == nil || !strings.Contains(output, "hook.devin.event-unmappable") {
		t.Fatalf("failing member must block the plugin instance update: err=%v\n%s", err, output)
	}
	current, err := os.ReadFile(skillPath)
	if err != nil || string(current) != string(previous) {
		t.Fatalf("the prior plugin projection must stay unchanged: err=%v\nold=%s\nnew=%s", err, previous, current)
	}
}

// spec: plugin-delivery/a-failing-member-on-first-install-installs-nothing
func TestPluginInstanceWithFailingMemberInstallsNothingInitially(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repo, run := pluginCommandFixture(t, "    - plugin/review-kit\n")
	addUnmappableDevinHook(t, filepath.Join(filepath.Dir(repo), "marketplace", "plugins", "review-kit"))
	configurePluginDeliveryFixture(t, repo, "devin", "resources:\n  team:\n    - plugin/review-kit\n")

	output, err := run("sync", "--no-backup")
	if err == nil || !strings.Contains(output, "hook.devin.event-unmappable") {
		t.Fatalf("failing member must block a first plugin installation: err=%v\n%s", err, output)
	}
	if _, statErr := os.Lstat(filepath.Join(repo, ".devin", "skills", "review-kit-review")); !os.IsNotExist(statErr) {
		t.Fatalf("first installation must leave no members from the blocked plugin: %v", statErr)
	}
}

// spec: plugin-delivery/a-failing-member-on-first-install-installs-nothing
func TestPluginDecodeFailureBlocksTheWholeInstanceUnlessExcluded(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, tc := range []struct {
		name       string
		excludeBad bool
		wantMember bool
	}{
		{name: "bad-member-blocks-instance"},
		{name: "excluded-bad-member-allows-instance", excludeBad: true, wantMember: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, run := pluginCommandFixture(t, "    - plugin/review-kit\n")
			pluginDir := filepath.Join(filepath.Dir(repo), "marketplace", "plugins", "review-kit")
			badSkill := filepath.Join(pluginDir, "skills", "bad")
			if err := os.MkdirAll(badSkill, 0o755); err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(badSkill, "SKILL.md"), "---\nname: bad\ninvalid: [\n---\nbody\n")
			selection := "    - plugin/review-kit\n"
			if tc.excludeBad {
				selection = "    - {kind: plugin, name: review-kit, exclude: [skill/bad]}\n"
			}
			configurePluginDeliveryFixture(t, repo, "claude", "resources:\n  team:\n"+selection)

			output, err := run("sync", "--no-backup")
			memberPath := filepath.Join(repo, ".claude", "skills", "review-kit-review", "SKILL.md")
			_, statErr := os.Stat(memberPath)
			if tc.wantMember {
				if err != nil || statErr != nil {
					t.Fatalf("excluding the malformed member must unblock its plugin: err=%v stat=%v\n%s", err, statErr, output)
				}
				return
			}
			if err == nil || !strings.Contains(output, "materialize.compile.error") {
				t.Fatalf("malformed non-excluded member must block the plugin instance: err=%v\n%s", err, output)
			}
			if !os.IsNotExist(statErr) {
				t.Fatalf("first install left a valid sibling from the blocked plugin: %v", statErr)
			}
		})
	}
}

// spec: plugin-delivery/other-instances-continue-past-a-blocked-instance
func TestOtherPluginInstancesAndLeavesContinueWhenOneInstanceBlocks(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repo, run := pluginCommandFixture(t, "    - plugin/review-kit\n")
	root := filepath.Dir(repo)
	catalogDir := filepath.Join(root, "marketplace")
	writePluginSourceFixture(t, filepath.Join(catalogDir, "plugins", "good-kit"), ".claude-plugin", "good-kit", "okay")
	writeFile(t, filepath.Join(catalogDir, ".claude-plugin", "marketplace.json"),
		`{"name":"team","plugins":[{"name":"review-kit","source":"./plugins/review-kit"},{"name":"good-kit","source":"./plugins/good-kit"}]}`)
	addUnmappableDevinHook(t, filepath.Join(catalogDir, "plugins", "review-kit"))
	leafDir := filepath.Join(root, "loose-skills")
	writeSkill(t, leafDir, "tooling", "Tooling", nil)
	writeConfig(t, filepath.Join(root, "home", ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nharnesses: [claude, devin]\n")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  team: {directory: "+
			filepath.ToSlash(catalogDir)+", format: claude-marketplace}\n  loose: {directory: "+
			filepath.ToSlash(leafDir)+"}\nresources:\n  team:\n    - plugin/review-kit\n    - plugin/good-kit\n  loose:\n    - skill/tooling\n")
	withWorkingDir(t, repo)

	output, err := run("sync", "--no-backup")
	if err == nil || !strings.Contains(output, "hook.devin.event-unmappable") {
		t.Fatalf("the blocked instance must be reported while independent resources continue: err=%v\n%s", err, output)
	}
	for _, path := range []string{
		filepath.Join(repo, ".claude", "skills", "review-kit-review", "SKILL.md"),
		filepath.Join(repo, ".devin", "skills", "good-kit-okay", "SKILL.md"),
		filepath.Join(repo, ".devin", "skills", "tooling", "SKILL.md"),
	} {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Errorf("independent output %s missing: %v", path, statErr)
		}
	}
	if _, statErr := os.Lstat(filepath.Join(repo, ".devin", "skills", "review-kit-review")); !os.IsNotExist(statErr) {
		t.Errorf("the Devin instance with a failing member must install none of its members: %v", statErr)
	}
}

// spec: plugin-delivery/excluding-the-failing-member-unblocks-the-instance
func TestExcludingFailingPluginMemberUnblocksInstance(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repo, run := pluginCommandFixture(t, "    - plugin/review-kit\n")
	addUnmappableDevinHook(t, filepath.Join(filepath.Dir(repo), "marketplace", "plugins", "review-kit"))
	configurePluginDeliveryFixture(t, repo, "devin",
		"resources:\n  team:\n    - {kind: plugin, name: review-kit, exclude: [hook/Notification-command]}\n")

	output, err := run("sync", "--no-backup")
	if err != nil {
		t.Fatalf("excluding the failing hook must unblock the plugin instance: %v\n%s", err, output)
	}
	if _, statErr := os.Stat(filepath.Join(repo, ".devin", "skills", "review-kit-review", "SKILL.md")); statErr != nil {
		t.Fatalf("remaining plugin member was not installed: %v", statErr)
	}
}

// spec: plugin-delivery/plugin-hook-command-reaches-its-generation
func TestPluginHookUsesCompletePackageGeneration(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repo, run := pluginCommandFixture(t, "    - plugin/review-kit\n")
	root := filepath.Dir(repo)
	pluginDir := filepath.Join(root, "marketplace", "plugins", "review-kit")
	scripts := filepath.Join(pluginDir, "scripts")
	if err := os.MkdirAll(scripts, 0o755); err != nil {
		t.Fatal(err)
	}
	const script = "#!/bin/sh\nprintf 'generation bytes\\n'\n"
	scriptPath := filepath.Join(scripts, "check.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	addPluginHook(t, pluginDir, "PreToolUse", "${CLAUDE_PLUGIN_ROOT}/scripts/check.sh")

	output, err := run("sync", "--no-backup")
	if err != nil {
		t.Fatalf("plugin generation sync failed: %v\n%s", err, output)
	}
	generationRoot := filepath.Join(root, "state", "agent-marketplace", "packages")
	generations, err := filepath.Glob(filepath.Join(generationRoot, "*", "scripts", "check.sh"))
	if err != nil || len(generations) != 1 {
		t.Fatalf("published generation hook assets = %v / %v", generations, err)
	}
	generationScript := generations[0]
	data, err := os.ReadFile(generationScript)
	info, statErr := os.Stat(generationScript)
	if err != nil || statErr != nil || string(data) != script || info.Mode().Perm() != 0o755 {
		t.Fatalf("generation script bytes/mode = %q / %v / %v", data, info, err)
	}
	settings, err := os.ReadFile(filepath.Join(repo, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("read installed Claude settings: %v", err)
	}
	generationRoot = filepath.Dir(filepath.Dir(generationScript))
	if !strings.Contains(string(settings), filepath.Join(generationRoot, "scripts", "check.sh")) {
		t.Fatalf("installed hook must reference the complete generation: %s", settings)
	}
	if strings.Contains(string(settings), filepath.Join(root, "marketplace", "plugins", "review-kit")) {
		t.Fatalf("installed hook must not reference the acquisition source: %s", settings)
	}
	states, err := filepath.Glob(filepath.Join(root, "state", "agent-marketplace", "ownership", "plugin-packages", "*.json"))
	if err != nil || len(states) != 1 {
		t.Fatalf("plugin ownership state files = %v / %v", states, err)
	}
	stateBytes, err := os.ReadFile(states[0])
	if err != nil || !strings.Contains(string(stateBytes), filepath.Base(generationRoot)) {
		t.Fatalf("plugin ownership state does not reference the package generation: %s / %v", stateBytes, err)
	}
}

// spec: plugin-delivery/remove-keeps-plugin-data-and-reports-it
// spec: plugin-delivery/unreferenced-generations-are-collected
func TestPluginDataSurvivesRemovalAndGarbageCollection(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repo, run := pluginCommandFixture(t, "    - plugin/review-kit\n")
	root := filepath.Dir(repo)
	packageDir := filepath.Join(root, "portable")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(packageDir, "plugin.json"),
		`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"persist-kit"}`)
	writeFile(t, filepath.Join(packageDir, "mcp.json"),
		`{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"persist":{"type":"stdio","command":"node","args":["${PLUGIN_DATA}"]}}}`)
	writeConfig(t, filepath.Join(root, "home", ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nharnesses: [claude]\n")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  portable: {directory: "+
			filepath.ToSlash(packageDir)+", format: agent-plugin}\nresources:\n  portable:\n    - plugin/persist-kit\n")
	manualPath := filepath.Join(repo, ".mcp.json")
	writeFile(t, manualPath, `{"mcpServers":{"manual":{"command":"echo manual"}}}`+"\n")
	withWorkingDir(t, repo)

	output, err := run("sync", "--no-backup")
	if err != nil {
		t.Fatalf("agent-plugin sync failed: %v\n%s", err, output)
	}
	mcpBytes, err := os.ReadFile(manualPath)
	if err != nil {
		t.Fatalf("read installed MCP configuration: %v", err)
	}
	generations, err := filepath.Glob(filepath.Join(root, "state", "agent-marketplace", "packages", "*", "plugin.json"))
	if err != nil || len(generations) != 1 {
		t.Fatalf("published plugin generations = %v / %v", generations, err)
	}
	generationDir := filepath.Dir(generations[0])
	prefix := filepath.Join(root, "state", "agent-marketplace", "plugin-data") + string(os.PathSeparator)
	start := strings.Index(string(mcpBytes), prefix)
	if start < 0 {
		listing, _ := run("list")
		var paths []string
		_ = filepath.Walk(repo, func(path string, _ os.FileInfo, _ error) error {
			paths = append(paths, path)
			return nil
		})
		t.Fatalf("plugin MCP command did not resolve its persistent data root: %s\nsync:\n%s\nlist:\n%s\nfiles:\n%s", mcpBytes, output, listing, strings.Join(paths, "\n"))
	}
	endRelative := strings.IndexByte(string(mcpBytes[start:]), '"')
	if endRelative < 0 {
		t.Fatalf("plugin data path is not terminated in MCP output: %s", mcpBytes)
	}
	dataPath := string(mcpBytes[start : start+endRelative])
	if err := os.MkdirAll(dataPath, 0o700); err != nil {
		t.Fatal(err)
	}
	dataFile := filepath.Join(dataPath, "state.db")
	writeFile(t, dataFile, "persistent plugin data")

	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  portable: {directory: "+
			filepath.ToSlash(packageDir)+", format: agent-plugin}\nresources: {}\n")
	output, err = run("sync", "--no-backup")
	if err != nil {
		t.Fatalf("deselecting the plugin failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, dataPath) {
		t.Fatalf("sync must report the retained plugin-data path %q:\n%s", dataPath, output)
	}
	after, err := os.ReadFile(manualPath)
	if err != nil || !strings.Contains(string(after), `"manual"`) || strings.Contains(string(after), `"persist"`) {
		t.Fatalf("removal must keep handwritten MCP content and remove plugin output: err=%v\n%s", err, after)
	}
	if data, err := os.ReadFile(dataFile); err != nil || string(data) != "persistent plugin data" {
		t.Fatalf("plugin data did not survive removal: %q / %v", data, err)
	}
	if output, err := run("gc"); err != nil {
		t.Fatalf("gc failed after plugin removal: %v\n%s", err, output)
	}
	if _, err := os.Lstat(generationDir); !os.IsNotExist(err) {
		t.Fatalf("unreferenced package generation remained after gc: %v", err)
	}
	if data, err := os.ReadFile(dataFile); err != nil || string(data) != "persistent plugin data" {
		t.Fatalf("plugin data did not survive gc: %q / %v", data, err)
	}
}

// spec: plugin-delivery/remove-keeps-plugin-data-and-reports-it
func TestRemovalDoesNotReportMissingPluginDataDirectory(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repo, run := pluginCommandFixture(t, "    - plugin/review-kit\n")
	output, err := run("sync", "--no-backup")
	if err != nil {
		t.Fatalf("initial plugin sync failed: %v\n%s", err, output)
	}
	root := filepath.Dir(repo)
	dataPaths, err := filepath.Glob(filepath.Join(root, "state", "agent-marketplace", "plugin-data", "*"))
	if err != nil || len(dataPaths) != 1 {
		t.Fatalf("plugin data paths = %v / %v, want one instance path", dataPaths, err)
	}
	if err := os.RemoveAll(dataPaths[0]); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  team: {directory: "+
			filepath.ToSlash(filepath.Join(root, "marketplace"))+", format: claude-marketplace}\nresources: {}\n")
	output, err = run("sync", "--no-backup")
	if err != nil {
		t.Fatalf("plugin removal failed: %v\n%s", err, output)
	}
	if strings.Contains(output, dataPaths[0]) {
		t.Fatalf("sync reported nonexistent retained plugin data %q:\n%s", dataPaths[0], output)
	}
}

// spec: plugin-delivery/repository-removal-leaves-the-machine-instance
func TestRepositoryPluginRemovalLeavesMachineInstance(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	repo, run := pluginCommandFixture(t, "    - plugin/review-kit\n")
	root := filepath.Dir(repo)
	catalogDir := filepath.Join(root, "marketplace")
	globalConfig := "apiVersion: agent-marketplace/v4\nharnesses: [claude]\nmaterialization: {mode: file}\nsources:\n  team: {directory: " +
		filepath.ToSlash(catalogDir) + ", format: claude-marketplace}\nresources:\n  team:\n    - plugin/review-kit\n"
	writeConfig(t, filepath.Join(root, "home", ".agents", "config.yaml"), globalConfig)
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), globalConfig)
	withWorkingDir(t, repo)

	if output, err := run("sync", "--no-backup"); err != nil {
		t.Fatalf("machine and repository plugin sync failed: %v\n%s", err, output)
	}
	repoMember := filepath.Join(repo, ".claude", "skills", "review-kit-review", "SKILL.md")
	machineMember := filepath.Join(root, "home", ".claude", "skills", "review-kit-review", "SKILL.md")
	for _, path := range []string{repoMember, machineMember} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("plugin member missing at %s: %v", path, err)
		}
	}

	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources:\n  team: {directory: "+
			filepath.ToSlash(catalogDir)+", format: claude-marketplace}\nresources: {}\n")
	if output, err := run("sync", "--no-backup"); err != nil {
		t.Fatalf("repository plugin deselection failed: %v\n%s", err, output)
	}
	if _, err := os.Stat(repoMember); !os.IsNotExist(err) {
		t.Fatalf("repository instance remained after deselection: %v", err)
	}
	if _, err := os.Stat(machineMember); err != nil {
		t.Fatalf("machine instance was removed with repository selection: %v", err)
	}
}
