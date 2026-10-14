package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

func TestInstallCommandAllIncludesCrossRegistryHooks(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	firstOrigin, firstWorking := createBareRepoWithWorkingClone(t, "first-hooks.git")
	writeFile(t, filepath.Join(firstWorking, ".factory", "hooks", "hooks.json"), `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo first"}]}]}`)
	commitAndPushAll(t, firstWorking, "Add first hook")

	secondOrigin, secondWorking := createBareRepoWithWorkingClone(t, "second-hooks.git")
	writeFile(t, filepath.Join(secondWorking, ".factory", "hooks", "hooks.json"), `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo second"}]}]}`)
	commitAndPushAll(t, secondWorking, "Add second hook")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
harnesses: [factory]
sources:
  first:
    git: file://`+firstOrigin+`
    ref: main
  second:
    git: file://`+secondOrigin+`
    ref: main
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "PreToolUse-command", "--kind", "hook", "--from", firstOrigin, "--ref", "main")
	if err != nil {
		t.Fatalf("named install first hook provider: %v\n%s", err, output)
	}
	namedData, err := os.ReadFile(filepath.Join(repoRoot, ".factory", "hooks", "hooks.json"))
	if err != nil {
		t.Fatalf("read named-install hooks: %v", err)
	}
	var namedDocument map[string][]struct {
		Matcher string `json:"matcher"`
		Hooks   []struct {
			Type    string `json:"type"`
			Command string `json:"command"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(namedData, &namedDocument); err != nil {
		t.Fatalf("decode named-install hooks: %v\n%s", err, namedData)
	}
	namedGroups := namedDocument["PreToolUse"]
	if len(namedDocument) != 1 || len(namedGroups) != 1 || namedGroups[0].Matcher != "Bash" || len(namedGroups[0].Hooks) != 1 || namedGroups[0].Hooks[0].Type != "command" || namedGroups[0].Hooks[0].Command != "echo first" {
		t.Fatalf("named-install hooks = %#v, want one Bash command hook from first provider", namedDocument)
	}
	if strings.Contains(string(namedData), "echo second") {
		t.Fatalf("named install selected second provider: %s", namedData)
	}

	output, err = executeCommand("install", "--all", "--kind", "hook")
	if err != nil {
		t.Fatalf("install --all --kind hook: %v\n%s", err, output)
	}

	cfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("load repo config: %v", err)
	}
	if len(cfg.Registries) != 2 {
		t.Fatalf("registry count = %d, want 2", len(cfg.Registries))
	}
	for _, entry := range cfg.Registries {
		if len(entry.Hooks.Include) != 1 || entry.Hooks.Include[0] != "PreToolUse-command" {
			t.Fatalf("registry %q hooks = %#v, want its hook include", entry.URL, entry.Hooks.Include)
		}
	}

	data, err := os.ReadFile(filepath.Join(repoRoot, ".factory", "hooks", "hooks.json"))
	if err != nil {
		t.Fatalf("read materialized hooks: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("decode materialized hooks: %v\n%s", err, data)
	}
	groups, ok := document["PreToolUse"].([]any)
	if !ok || len(groups) != 2 {
		t.Fatalf("PreToolUse groups = %#v, want two registry-specific groups", document["PreToolUse"])
	}
	if !strings.Contains(string(data), "echo first") || !strings.Contains(string(data), "echo second") {
		t.Fatalf("materialized hooks lost a registry contribution: %s", data)
	}

	output, err = executeCommand("remove", "PreToolUse-command", "--kind", "hook", "--from", firstOrigin)
	if err != nil {
		t.Fatalf("remove first hook provider: %v\n%s", err, output)
	}
	cfg, err = config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("load repo config after remove: %v", err)
	}
	if len(cfg.Registries) != 2 {
		t.Fatalf("registry count after remove = %d, want both providers retained", len(cfg.Registries))
	}
	for _, entry := range cfg.Registries {
		switch entry.URL {
		case "file://" + firstOrigin:
			if len(entry.Hooks.Include) != 0 {
				t.Fatalf("first registry hooks after remove = %#v, want empty", entry.Hooks.Include)
			}
		case "file://" + secondOrigin:
			if len(entry.Hooks.Include) != 1 || entry.Hooks.Include[0] != "PreToolUse-command" {
				t.Fatalf("second registry hooks after remove = %#v, want provider retained", entry.Hooks.Include)
			}
		default:
			t.Fatalf("unexpected source after provider-scoped remove: %q", entry.URL)
		}
	}
	lock := readLockDocument(t, filepath.Join(repoRoot, ".agents", "lock.yaml"))
	if len(lock.Registries) != 2 {
		t.Fatalf("lock registries after remove = %#v, want both registry pins retained", lock.Registries)
	}

	data, err = os.ReadFile(filepath.Join(repoRoot, ".factory", "hooks", "hooks.json"))
	if err != nil {
		t.Fatalf("read materialized hooks after remove: %v", err)
	}
	var remaining map[string][]struct {
		Matcher string `json:"matcher"`
		Hooks   []struct {
			Type    string `json:"type"`
			Command string `json:"command"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &remaining); err != nil {
		t.Fatalf("decode materialized hooks after remove: %v\n%s", err, data)
	}
	remainingGroups := remaining["PreToolUse"]
	if len(remaining) != 1 || len(remainingGroups) != 1 || remainingGroups[0].Matcher != "Bash" || len(remainingGroups[0].Hooks) != 1 || remainingGroups[0].Hooks[0].Command != "echo second" {
		t.Fatalf("remaining hooks = %#v, want only second Bash provider", remainingGroups)
	}
}
