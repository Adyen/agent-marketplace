//go:build e2e

package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInterruptedPluginDeliveryRecoversAsOneUnit(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	env.WriteGlobalConfig("apiVersion: agent-marketplace/v4\nharnesses: [claude]\n")
	env.WriteFile("/workspace/vendor/plugin/.claude-plugin/plugin.json", `{"name":"crash-kit"}`)
	env.WriteFile("/workspace/vendor/plugin/hooks/hooks.json", `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo plugin-hook"}]}]}`)
	env.WriteRepoConfig("apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources: {plugin: {directory: ./vendor/plugin, format: claude-plugin}}\nresources: {plugin: {include: [plugin/crash-kit]}}\n")

	crash := env.RunAMWithEnv(map[string]string{"AGENT_MARKETPLACE_E2E_CRASH_AT": "after-publish:0"}, "sync", "--no-backup")
	if crash.ExitCode != 137 {
		t.Fatalf("injected plugin sync crash exited %d:\n%s", crash.ExitCode, crash.Combined)
	}
	stateRoot := filepath.Join(env.home, ".local", "state", "agent-marketplace")
	journals, err := filepath.Glob(filepath.Join(stateRoot, "dependencies", ".dependencytxn-*.json"))
	if err != nil || len(journals) != 1 {
		t.Fatalf("plugin delivery journals = %v / %v, want one interrupted transaction", journals, err)
	}
	journalBytes, err := os.ReadFile(journals[0])
	if err != nil {
		t.Fatal(err)
	}
	var journal struct {
		Files []struct {
			Path string `json:"path"`
			Temp string `json:"temp"`
		} `json:"files"`
	}
	if err := json.Unmarshal(journalBytes, &journal); err != nil {
		t.Fatalf("decode plugin delivery journal: %v", err)
	}
	if len(journal.Files) == 0 {
		t.Fatal("plugin delivery transaction has no participants")
	}
	memberPath := env.mustResolvePath("/workspace/.claude/settings.json")
	canonicalMemberPath, err := filepath.EvalSymlinks(filepath.Dir(memberPath))
	if err != nil {
		t.Fatal(err)
	}
	canonicalMemberPath = filepath.Join(canonicalMemberPath, filepath.Base(memberPath))
	journalOwnsMember := false
	for _, file := range journal.Files {
		if filepath.Clean(file.Path) == filepath.Clean(canonicalMemberPath) {
			journalOwnsMember = true
			break
		}
	}
	if !journalOwnsMember {
		t.Fatalf("plugin delivery journal does not include the hook output %s: %+v", memberPath, journal.Files)
	}
	type fileSnapshot struct {
		exists bool
		data   []byte
		mode   os.FileMode
	}
	snapshot := func(path string) fileSnapshot {
		t.Helper()
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			return fileSnapshot{}
		}
		if err != nil {
			t.Fatalf("inspect journal participant %s: %v", path, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read journal participant %s: %v", path, err)
		}
		return fileSnapshot{exists: true, data: data, mode: info.Mode()}
	}
	participants := make(map[string]fileSnapshot, len(journal.Files)*2)
	seen := make(map[string]bool, len(journal.Files))
	for _, file := range journal.Files {
		if file.Path == "" || seen[file.Path] {
			t.Fatalf("invalid or duplicate journal participant path %q", file.Path)
		}
		seen[file.Path] = true
		participants[file.Path] = snapshot(file.Path)
		if file.Temp != "" {
			participants[file.Temp] = snapshot(file.Temp)
		}
	}

	for _, args := range [][]string{{"sync", "--check"}, {"status"}} {
		result := env.RunAM(args...)
		if result.ExitCode == 0 || !strings.Contains(result.Combined, "dependency.recovery-required") {
			t.Fatalf("read-only %v did not refuse pending plugin delivery (exit %d):\n%s", args, result.ExitCode, result.Combined)
		}
		for path, want := range participants {
			got := snapshot(path)
			if got.exists != want.exists || string(got.data) != string(want.data) || got.mode != want.mode {
				t.Fatalf("read-only %v replayed or changed participant %s", args, path)
			}
		}
	}

	env.MustRunAM("sync", "--no-backup")
	if remaining, err := filepath.Glob(filepath.Join(stateRoot, "dependencies", ".dependencytxn-*.json")); err != nil || len(remaining) != 0 {
		t.Fatalf("recovered plugin delivery left journals %v / %v", remaining, err)
	}
	hooks, err := os.ReadFile(memberPath)
	if err != nil || !strings.Contains(string(hooks), "plugin-hook") {
		t.Fatalf("recovery did not complete plugin hook delivery: %v\n%s", err, hooks)
	}
	if err := filepath.Walk(stateRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		name := info.Name()
		if strings.HasSuffix(name, ".tmp") || strings.HasPrefix(name, ".dependencytxn-") || strings.HasPrefix(name, ".packagegen-") {
			t.Errorf("recovery left staging residue %s", path)
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect state for staging residue: %v", err)
	}
	stageDirs := map[string]bool{filepath.Join(stateRoot, "packages"): true}
	for _, file := range journal.Files {
		stageDirs[filepath.Dir(file.Path)] = true
	}
	for dir := range stageDirs {
		stages, err := filepath.Glob(filepath.Join(dir, ".dependencytxn-*.tmp"))
		if err != nil || len(stages) > 0 {
			t.Fatalf("transaction staging residue in %s = %v / %v", dir, stages, err)
		}
	}
	packageStages, err := filepath.Glob(filepath.Join(stateRoot, "packages", ".packagegen-*"))
	if err != nil || len(packageStages) > 0 {
		t.Fatalf("package generation staging residue = %v / %v", packageStages, err)
	}
	generations, err := filepath.Glob(filepath.Join(stateRoot, "packages", "*", ".claude-plugin", "plugin.json"))
	if err != nil || len(generations) != 1 {
		t.Fatalf("recovered plugin generations = %v / %v", generations, err)
	}
	stateFiles, err := filepath.Glob(filepath.Join(stateRoot, "ownership", "*", "state.json"))
	if err != nil || len(stateFiles) != 1 {
		t.Fatalf("recovered aggregate ownership states = %v / %v", stateFiles, err)
	}
	state, err := os.ReadFile(stateFiles[0])
	if err != nil || !strings.Contains(string(state), filepath.Base(filepath.Dir(filepath.Dir(generations[0])))) {
		t.Fatalf("recovered ownership state does not reference plugin generation: %v\n%s", err, state)
	}
}

// spec: plugin-delivery/a-failing-member-blocks-the-whole-instance-update
func TestInterruptedPluginInstanceStateCommitRecoversAllInstances(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	env.WriteGlobalConfig("apiVersion: agent-marketplace/v4\nharnesses: [claude]\n")
	env.WriteFile("/workspace/vendor/.claude-plugin/marketplace.json",
		`{"name":"team","plugins":[{"name":"crash-kit","source":"./plugins/crash-kit"},{"name":"other-kit","source":"./plugins/other-kit"}]}`)
	env.WriteFile("/workspace/vendor/plugins/crash-kit/.claude-plugin/plugin.json", `{"name":"crash-kit"}`)
	env.WriteFile("/workspace/vendor/plugins/crash-kit/skills/review/SKILL.md", "---\nname: review\ndescription: Review\n---\nReview body\n")
	env.WriteFile("/workspace/vendor/plugins/crash-kit/hooks/hooks.json", `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo plugin-hook"}]}]}`)
	env.WriteFile("/workspace/vendor/plugins/other-kit/.claude-plugin/plugin.json", `{"name":"other-kit"}`)
	env.WriteFile("/workspace/vendor/plugins/other-kit/skills/check/SKILL.md", "---\nname: check\ndescription: Check\n---\nCheck body\n")
	env.WriteRepoConfig("apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\nsources: {catalog: {directory: ./vendor, format: claude-marketplace}}\nresources: {catalog: {include: [plugin/crash-kit, plugin/other-kit]}}\n")
	env.MustRunAM("sync", "--no-backup")

	stateRoot := filepath.Join(env.home, ".local", "state", "agent-marketplace")
	pluginStateDir := filepath.Join(stateRoot, "ownership", "plugin-packages")
	stateFiles, err := filepath.Glob(filepath.Join(pluginStateDir, "*.json"))
	if err != nil || len(stateFiles) != 2 {
		t.Fatalf("per-instance plugin state files = %v / %v, want two", stateFiles, err)
	}
	type instanceState struct {
		Instance struct {
			InstanceKey   string `json:"instance_key"`
			Name          string `json:"name"`
			Target        string `json:"target"`
			Scope         string `json:"scope"`
			GenerationKey string `json:"generation_key"`
		} `json:"instance"`
	}
	stateByName := make(map[string]string, 2)
	for _, statePath := range stateFiles {
		data, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatal(err)
		}
		var state instanceState
		if err := json.Unmarshal(data, &state); err != nil {
			t.Fatalf("decode per-instance state %s: %v", statePath, err)
		}
		ref := state.Instance
		if ref.InstanceKey == "" || filepath.Base(statePath) != ref.InstanceKey+".json" || ref.Target != "claude" || ref.GenerationKey == "" {
			t.Fatalf("invalid per-instance state at %s: %+v", statePath, ref)
		}
		if stateByName[ref.Name] != "" {
			t.Fatalf("duplicate plugin instance state for %s", ref.Name)
		}
		canonicalStatePath, err := filepath.EvalSymlinks(statePath)
		if err != nil {
			t.Fatalf("resolve per-instance state path %s: %v", statePath, err)
		}
		stateByName[ref.Name] = canonicalStatePath
	}
	if stateByName["crash-kit"] == "" || stateByName["other-kit"] == "" {
		t.Fatalf("plugin instance state names = %v, want crash-kit and other-kit", stateByName)
	}
	for _, statePath := range stateFiles {
		if err := os.Remove(statePath); err != nil {
			t.Fatal(err)
		}
	}

	crash := env.RunAMWithEnv(map[string]string{"AGENT_MARKETPLACE_E2E_CRASH_AT": "after-publish:0"}, "sync", "--no-backup")
	if crash.ExitCode != 137 {
		t.Fatalf("instance-state commit crash exited %d:\n%s", crash.ExitCode, crash.Combined)
	}
	journals, err := filepath.Glob(filepath.Join(stateRoot, "dependencies", ".dependencytxn-*.json"))
	if err != nil || len(journals) != 1 {
		t.Fatalf("instance-state journals = %v / %v, want one interrupted transaction", journals, err)
	}
	journalBytes, err := os.ReadFile(journals[0])
	if err != nil {
		t.Fatal(err)
	}
	var journal struct {
		Files []struct {
			Path string `json:"path"`
			Temp string `json:"temp"`
		} `json:"files"`
	}
	if err := json.Unmarshal(journalBytes, &journal); err != nil {
		t.Fatalf("decode instance-state journal: %v", err)
	}
	if len(journal.Files) != 2 {
		t.Fatalf("journal participants = %+v, want exactly both instance records", journal.Files)
	}
	expected := map[string]bool{stateByName["crash-kit"]: true, stateByName["other-kit"]: true}
	type fileSnapshot struct {
		exists bool
		data   []byte
		mode   os.FileMode
	}
	snapshot := func(path string) fileSnapshot {
		t.Helper()
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			return fileSnapshot{}
		}
		if err != nil {
			t.Fatalf("inspect transaction participant %s: %v", path, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read transaction participant %s: %v", path, err)
		}
		return fileSnapshot{exists: true, data: data, mode: info.Mode()}
	}
	participants := make(map[string]fileSnapshot, len(journal.Files)*2)
	for _, file := range journal.Files {
		if !expected[file.Path] {
			t.Fatalf("unrecognized transaction participant %s; expected %v", file.Path, stateByName)
		}
		delete(expected, file.Path)
		participants[file.Path] = snapshot(file.Path)
		if file.Temp == "" || filepath.Dir(file.Temp) != filepath.Dir(file.Path) {
			t.Fatalf("invalid transaction temporary path for %s: %s", file.Path, file.Temp)
		}
		participants[file.Temp] = snapshot(file.Temp)
	}
	if len(expected) != 0 {
		t.Fatalf("instance-state journal omitted participants %v", expected)
	}
	assertReadOnly := func(args ...string) {
		t.Helper()
		result := env.RunAM(args...)
		if result.ExitCode == 0 || !strings.Contains(result.Combined, "dependency.recovery-required") {
			t.Fatalf("read-only %v did not refuse pending state commit (exit %d):\n%s", args, result.ExitCode, result.Combined)
		}
		for path, want := range participants {
			got := snapshot(path)
			if got.exists != want.exists || string(got.data) != string(want.data) || got.mode != want.mode {
				t.Fatalf("read-only %v changed participant %s", args, path)
			}
		}
	}
	assertReadOnly("sync", "--check")
	assertReadOnly("status")

	recoveryCrash := env.RunAMWithEnv(map[string]string{"AGENT_MARKETPLACE_E2E_CRASH_AT": "before-publish:1"}, "sync", "--no-backup")
	if recoveryCrash.ExitCode != 137 {
		t.Fatalf("interrupted recovery exited %d:\n%s", recoveryCrash.ExitCode, recoveryCrash.Combined)
	}
	assertReadOnly("sync", "--check")
	env.MustRunAM("sync", "--no-backup")

	for _, rel := range []string{
		"/workspace/.claude/skills/crash-kit-review/SKILL.md",
		"/workspace/.claude/skills/other-kit-check/SKILL.md",
	} {
		if _, err := os.Stat(env.mustResolvePath(rel)); err != nil {
			t.Fatalf("recovered plugin output %s is missing: %v", rel, err)
		}
	}
	hooks, err := os.ReadFile(env.mustResolvePath("/workspace/.claude/settings.json"))
	if err != nil || !strings.Contains(string(hooks), "plugin-hook") {
		t.Fatalf("recovered hook output is missing: %v\n%s", err, hooks)
	}
	stateFiles, err = filepath.Glob(filepath.Join(pluginStateDir, "*.json"))
	if err != nil || len(stateFiles) != 2 {
		t.Fatalf("recovered per-instance state files = %v / %v", stateFiles, err)
	}
	for _, statePath := range stateFiles {
		data, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatal(err)
		}
		var state instanceState
		if err := json.Unmarshal(data, &state); err != nil {
			t.Fatalf("decode recovered instance state: %v", err)
		}
		if _, err := os.Stat(filepath.Join(stateRoot, "packages", state.Instance.GenerationKey)); err != nil {
			t.Fatalf("recovered instance %s references missing generation: %v", state.Instance.Name, err)
		}
	}
	if remaining, err := filepath.Glob(filepath.Join(stateRoot, "dependencies", ".dependencytxn-*.json")); err != nil || len(remaining) != 0 {
		t.Fatalf("recovered instance state left journals %v / %v", remaining, err)
	}
	if err := filepath.Walk(stateRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasPrefix(info.Name(), ".dependencytxn-") || strings.HasPrefix(info.Name(), ".packagegen-") {
			t.Errorf("recovery left staging residue %s", path)
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect state for staging residue: %v", err)
	}
}
