package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/codemode"
)

func writeCodeModeRegistry(t *testing.T, working string) {
	t.Helper()
	writeFile(t, filepath.Join(working, "skills", "skills", "code-mode", "SKILL.md"),
		"---\nname: code-mode\ndescription: Code-mode bootstrap skill\n---\n\n# code-mode\n")
	writeFile(t, filepath.Join(working, "skills", ".mcp.json"),
		`{"mcpServers":{"agent-marketplace":{"command":"agent-marketplace","args":["mcp"]}}}`)
	writeFile(t, filepath.Join(working, "skills", "collections", "code-mode", "COLLECTION.yaml"), `
name: code-mode
description: Code mode bootstrap skill plus the agent-marketplace MCP server.
resources:
  - name: code-mode
    kind: skill
  - name: agent-marketplace
    kind: mcp
`)
}

func stubAgentMarketplaceOnPATH(t *testing.T) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(bin, "agent-marketplace"), "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(filepath.Join(bin, "agent-marketplace"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// spec: code-mode-mcp/installing-collection-selects-skill-and-mcp
func TestCodeModeCollectionInstallAndSync(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	stubAgentMarketplaceOnPATH(t)

	origin, working := createBareRepoWithWorkingClone(t, "agent-marketplace-registry.git")
	writeCodeModeRegistry(t, working)
	commitAndPushAll(t, working, "Add code-mode collection")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nharnesses: [claude]\n")
	withWorkingDir(t, repoRoot)

	out, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}
	if data, err := os.ReadFile(filepath.Join(repoRoot, ".mcp.json")); err == nil &&
		strings.Contains(string(data), "agent-marketplace") {
		t.Fatalf("code-mode registered without selection:\n%s", data)
	}

	out, err = executeCommand("install", "code-mode", "--from", origin, "--path", "skills")
	if err != nil {
		t.Fatalf("install code-mode collection: %v\n%s", err, out)
	}
	out, err = executeCommand("sync")
	if err != nil {
		t.Fatalf("sync after install: %v\n%s", err, out)
	}

	data, err := os.ReadFile(filepath.Join(repoRoot, ".mcp.json"))
	if err != nil {
		t.Fatalf(".mcp.json missing: %v", err)
	}
	if !strings.Contains(string(data), `"agent-marketplace"`) {
		t.Fatalf("owned agent-marketplace MCP entry missing:\n%s", data)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, ".claude", "skills", "code-mode")); err != nil {
		t.Fatalf("bootstrap skill not materialized: %v", err)
	}
}

// spec: code-mode-mcp/installing-collection-selects-skill-and-mcp
func TestCodeModeCollectionMissingExecutableFails(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "agent-marketplace-registry.git")
	writeCodeModeRegistry(t, working)
	commitAndPushAll(t, working, "Add code-mode collection")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	cfgPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	cfgBody := "apiVersion: agent-marketplace/v4\nharnesses: [claude]\n"
	writeConfig(t, cfgPath, cfgBody)
	withWorkingDir(t, repoRoot)

	out, err := executeCommand("install", "code-mode", "--from", origin, "--path", "skills")
	if err == nil || !strings.Contains(err.Error()+out, "executable") {
		t.Fatalf("expected missing-executable failure, got err=%v out=%s", err, out)
	}
	if after, rerr := os.ReadFile(cfgPath); rerr != nil || string(after) != cfgBody {
		t.Fatalf("failed install changed the config:\n%s", after)
	}
	if _, serr := os.Stat(filepath.Join(repoRoot, ".mcp.json")); !os.IsNotExist(serr) {
		t.Fatal("failed install claimed an MCP entry")
	}
}

// spec: code-mode-mcp/installing-collection-selects-skill-and-mcp
func TestCodeModeCollectionUnrepresentableMemberFails(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	registry := t.TempDir()
	writeCodeModeRegistry(t, registry)
	writeFile(t, filepath.Join(registry, "skills", ".mcp.json"),
		`{"mcpServers":{"agent-marketplace":{"command":"${file:/x}","args":["mcp"]}}}`)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	cfgPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	cfgBody := "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  reg:\n    directory: " +
		filepath.ToSlash(filepath.Join(registry, "skills")) +
		"\nresources:\n  reg:\n    include: [collection/code-mode]\n"
	writeConfig(t, cfgPath, cfgBody)
	withWorkingDir(t, repoRoot)

	out, err := executeCommand("sync")
	if err == nil || !strings.Contains(err.Error()+out, "agent-marketplace") {
		t.Fatalf("expected unrepresentable-member failure, got err=%v out=%s", err, out)
	}
	if data, rerr := os.ReadFile(filepath.Join(repoRoot, ".devin", "mcp_config.json")); rerr == nil &&
		strings.Contains(string(data), "agent-marketplace") {
		t.Fatalf("unrepresentable member claimed a usable entry:\n%s", data)
	}
	if after, rerr := os.ReadFile(cfgPath); rerr != nil || string(after) != cfgBody {
		t.Fatalf("failed sync changed the config:\n%s", after)
	}
}

// spec: code-mode-mcp/removing-collection-preserves-other-owners
func TestCodeModeCollectionRemovePreservesOthers(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	stubAgentMarketplaceOnPATH(t)

	origin, working := createBareRepoWithWorkingClone(t, "agent-marketplace-registry.git")
	writeCodeModeRegistry(t, working)
	commitAndPushAll(t, working, "Add code-mode collection")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nharnesses: [claude]\n")
	writeFile(t, filepath.Join(repoRoot, ".mcp.json"),
		`{"mcpServers":{"handwritten":{"command":"echo","args":["hi"]}}}`)
	withWorkingDir(t, repoRoot)

	out, err := executeCommand("install", "code-mode", "--from", origin, "--path", "skills")
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	if out, err = executeCommand("sync"); err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}

	mcpPath := filepath.Join(repoRoot, ".mcp.json")
	installed, err := os.ReadFile(mcpPath)
	if err != nil || !strings.Contains(string(installed), `"agent-marketplace"`) {
		t.Fatalf("owned agent-marketplace MCP entry missing:\n%s", installed)
	}

	out, err = executeCommand("install", "agent-marketplace", "--kind", "mcp", "--from", origin, "--path", "skills")
	if err != nil {
		t.Fatalf("independent member install: %v\n%s", err, out)
	}
	if out, err = executeCommand("sync"); err != nil {
		t.Fatalf("sync after independent install: %v\n%s", err, out)
	}

	out, err = executeCommand("remove", "code-mode")
	if err != nil {
		t.Fatalf("remove code-mode collection: %v\n%s", err, out)
	}
	if out, err = executeCommand("sync"); err != nil {
		t.Fatalf("sync after remove: %v\n%s", err, out)
	}

	data, err := os.ReadFile(mcpPath)
	if err != nil || !strings.Contains(string(data), `"agent-marketplace"`) {
		t.Fatalf("independently selected MCP entry removed with the collection:\n%s", data)
	}
	if !strings.Contains(string(data), "handwritten") {
		t.Fatalf("handwritten MCP entry pruned:\n%s", data)
	}
	if _, serr := os.Stat(filepath.Join(repoRoot, ".claude", "skills", "code-mode")); !os.IsNotExist(serr) {
		t.Fatal("collection-owned skill not removed")
	}

	out, err = executeCommand("remove", "agent-marketplace", "--kind", "mcp")
	if err != nil {
		t.Fatalf("remove independent member: %v\n%s", err, out)
	}
	if out, err = executeCommand("sync"); err != nil {
		t.Fatalf("sync after member remove: %v\n%s", err, out)
	}
	data, err = os.ReadFile(mcpPath)
	if err == nil && strings.Contains(string(data), `"agent-marketplace"`) {
		t.Fatalf("owned MCP entry not removed:\n%s", data)
	}
	if err == nil && !strings.Contains(string(data), "handwritten") {
		t.Fatalf("handwritten MCP entry pruned:\n%s", data)
	}

	if out, err = executeCommand("sync"); err != nil {
		t.Fatalf("repeat sync: %v\n%s", err, out)
	}
	again, rerr := os.ReadFile(mcpPath)
	if rerr == nil && string(again) != string(data) {
		t.Fatalf("repeat sync not byte-idempotent:\n--- before ---\n%s\n--- after ---\n%s", data, again)
	}
}

// spec: code-mode-mcp/removing-collection-preserves-other-owners
func TestCodeModeCollectionUnownedCollisionDiagnosed(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	stubAgentMarketplaceOnPATH(t)

	registry := t.TempDir()
	writeCodeModeRegistry(t, registry)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources:\n  reg:\n    directory: "+
			filepath.ToSlash(filepath.Join(registry, "skills"))+
			"\nresources:\n  reg:\n    include: [collection/code-mode]\n")
	mcpPath := filepath.Join(repoRoot, ".mcp.json")
	handwritten := `{"mcpServers":{"agent-marketplace":{"command":"echo","args":["hi"]}}}`
	writeFile(t, mcpPath, handwritten)
	withWorkingDir(t, repoRoot)

	out, err := executeCommand("sync")
	if err == nil || !strings.Contains(err.Error()+out, "agent-marketplace") {
		t.Fatalf("expected diagnosed unmanaged collision, got err=%v out=%s", err, out)
	}
	if data, rerr := os.ReadFile(mcpPath); rerr != nil || string(data) != handwritten {
		t.Fatalf("unowned colliding entry overwritten:\n%s", data)
	}
}

// spec: code-mode-mcp/installing-collection-selects-skill-and-mcp
func TestSameNameMcpWithOtherRuntimeNotGated(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "agent-marketplace-registry.git")
	writeCodeModeRegistry(t, working)
	writeFile(t, filepath.Join(working, "skills", ".mcp.json"),
		`{"mcpServers":{"agent-marketplace":{"command":"definitely-missing-other-binary","args":["serve"]}}}`)
	commitAndPushAll(t, working, "Add code-mode collection")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nharnesses: [claude]\n")
	withWorkingDir(t, repoRoot)

	out, err := executeCommand("install", "code-mode", "--from", origin, "--path", "skills")
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	if out, err = executeCommand("sync"); err != nil {
		t.Fatalf("sync gated an unrelated same-name MCP: %v\n%s", err, out)
	}
	data, err := os.ReadFile(filepath.Join(repoRoot, ".mcp.json"))
	if err != nil || !strings.Contains(string(data), "definitely-missing-other-binary") {
		t.Fatalf("same-name MCP entry not materialized:\n%s", data)
	}
}

// spec: code-mode-mcp/installing-collection-selects-skill-and-mcp
func TestCodeModeGateScopesToSelectedRegistry(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	other := t.TempDir()
	writeFile(t, filepath.Join(other, ".mcp.json"),
		`{"mcpServers":{"agent-marketplace":{"command":"definitely-missing-other-binary","args":["serve"]}}}`)
	codemode := t.TempDir()
	writeFile(t, filepath.Join(codemode, ".mcp.json"),
		`{"mcpServers":{"agent-marketplace":{"command":"agent-marketplace","args":["mcp"]}}}`)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources:\n  a-other:\n    directory: "+
			filepath.ToSlash(other)+"\n  z-codemode:\n    directory: "+filepath.ToSlash(codemode)+"\n")
	withWorkingDir(t, repoRoot)

	out, err := executeCommand("install", "agent-marketplace", "--kind", "mcp")
	if err != nil {
		t.Fatalf("install of non-code-mode agent-marketplace MCP gated by another source: %v\n%s", err, out)
	}
	data, err := os.ReadFile(filepath.Join(repoRoot, ".mcp.json"))
	if err != nil || !strings.Contains(string(data), "definitely-missing-other-binary") {
		t.Fatalf("selected same-name MCP entry not materialized:\n%s", data)
	}
}

// spec: code-mode-mcp/installing-collection-selects-skill-and-mcp
func TestCodeModeGateFailsSelectedSourceBeforeCommit(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	codemode := t.TempDir()
	writeFile(t, filepath.Join(codemode, ".mcp.json"),
		`{"mcpServers":{"agent-marketplace":{"command":"agent-marketplace","args":["mcp"]}}}`)
	other := t.TempDir()
	writeFile(t, filepath.Join(other, ".mcp.json"),
		`{"mcpServers":{"agent-marketplace":{"command":"definitely-missing-other-binary","args":["serve"]}}}`)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	cfgPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	cfgBody := "apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources:\n  a-codemode:\n    directory: " +
		filepath.ToSlash(codemode) + "\n  z-other:\n    directory: " + filepath.ToSlash(other) + "\n"
	writeConfig(t, cfgPath, cfgBody)
	withWorkingDir(t, repoRoot)

	out, err := executeCommand("install", "agent-marketplace", "--kind", "mcp")
	if err == nil || !strings.Contains(err.Error()+out, "executable") {
		t.Fatalf("expected missing-executable failure, got err=%v out=%s", err, out)
	}
	if after, rerr := os.ReadFile(cfgPath); rerr != nil || string(after) != cfgBody {
		t.Fatalf("failed install changed the config:\n%s", after)
	}
	if _, serr := os.Stat(filepath.Join(repoRoot, ".mcp.json")); !os.IsNotExist(serr) {
		t.Fatal("failed install claimed an MCP entry")
	}
}

// spec: code-mode-mcp/installing-collection-selects-skill-and-mcp
func TestFirstPartyRegistryResolvesPreexistingSkillNames(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	_, thisFile, _, _ := runtime.Caller(0)
	skillsDir, err := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "..", "..", "skills"))
	if err != nil {
		t.Fatal(err)
	}

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"),
		"apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources:\n  fp:\n    directory: "+filepath.ToSlash(skillsDir)+"\nresources:\n  fp:\n    include: [skill/agent-marketplace, skill/agent-marketplace-development, skill/registry-authoring]\n")
	withWorkingDir(t, repoRoot)

	out, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("sync against first-party registry: %v\n%s", err, out)
	}
	for _, name := range []string{"agent-marketplace", "agent-marketplace-development", "registry-authoring"} {
		if _, serr := os.Stat(filepath.Join(repoRoot, ".claude", "skills", name)); serr != nil {
			t.Fatalf("preexisting skill %q not materialized: %v", name, serr)
		}
	}
}

// spec: code-mode-mcp/retired-enablement-fails-with-migration-guidance
func TestRetiredCodeModeEnablementFails(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	withWorkingDir(t, repoRoot)

	for _, args := range [][]string{
		{"mcp", "enable"}, {"mcp", "disable"}, {"init", "--code-mode", "--harnesses", "claude"},
	} {
		if out, err := executeCommand(args...); err == nil {
			t.Fatalf("%v unexpectedly succeeded: %s", args, out)
		}
	}

	cfgPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	body := "apiVersion: agent-marketplace/v4\nharnesses: [claude]\ncodeMode:\n  enabled: true\n"
	writeConfig(t, cfgPath, body)
	out, err := executeCommand("sync")
	if err == nil || !strings.Contains(err.Error()+out, "codeMode") || !strings.Contains(err.Error()+out, "install") {
		t.Fatalf("expected code-mode migration diagnostic, got err=%v out=%s", err, out)
	}
	after, rerr := os.ReadFile(cfgPath)
	if rerr != nil || string(after) != body {
		t.Fatalf("rejected config was modified:\n%s", after)
	}
}

// spec: code-mode-mcp/tools-list-exposes-exactly-search-and-execute
// spec: code-mode-mcp/installed-server-retains-two-tools
func TestMcpServerListsExactlySearchAndExecute(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))

	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- runMCPServer(t.Context(), inR, outW) }()

	send := func(v map[string]any) {
		raw, _ := json.Marshal(v)
		if _, err := inW.Write(append(raw, '\n')); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})
	send(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"})
	_ = inW.Close()

	dec := json.NewDecoder(outR)
	var names []string
	for i := 0; i < 2; i++ {
		var resp struct {
			ID     int `json:"id"`
			Result struct {
				Tools []struct {
					Name string `json:"name"`
				} `json:"tools"`
			} `json:"result"`
		}
		if err := dec.Decode(&resp); err != nil {
			t.Fatalf("decode response %d: %v", i, err)
		}
		for _, tool := range resp.Result.Tools {
			names = append(names, tool.Name)
		}
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("server: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("server did not exit on stdin close")
	}
	if len(names) != 2 || names[0] != "search" || names[1] != "execute" {
		t.Fatalf("tools = %v", names)
	}
}

func TestMcpServerExitsOnContextCancel(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))

	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = inW.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runMCPServer(ctx, inR, io.Discard) }()

	if _, err := inW.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("server: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("server did not exit on context cancel while stdin stayed open")
	}
}

// spec: code-mode-mcp/missing-evidence-fails
func TestPromoteFailsWithoutSessionEvidence(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	_, err := executeCommand("promote", "reg/skill/never-used")
	if err == nil || !strings.Contains(err.Error(), "no code-mode session log") {
		t.Fatalf("expected missing-evidence error, got %v", err)
	}
}

// spec: code-mode-mcp/latest-matching-entry-wins
func TestPromoteFindsLatestSessionEntry(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	logDir := filepath.Join(dir, "agent-marketplace", "mcp-sessions")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		t.Fatal(err)
	}
	old := codemode.SessionEntry{Timestamp: time.Now().Add(-time.Hour).UTC(), Session: "a", Event: "skill.load", Ref: "reg/skill/pdf", Revision: "v1", Digest: "sha256:1"}
	recent := codemode.SessionEntry{Timestamp: time.Now().UTC(), Session: "b", Event: "server.start", Ref: "reg/mcp/srv", Revision: "v2", Digest: "sha256:2"}
	for i, e := range []codemode.SessionEntry{old, recent} {
		raw, _ := json.Marshal(e)
		if err := os.WriteFile(filepath.Join(logDir, string(rune('a'+i))+".jsonl"), append(raw, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	entry, err := latestSessionEntry("reg/mcp/srv")
	if err != nil {
		t.Fatalf("latestSessionEntry: %v", err)
	}
	if entry.Session != "b" || entry.Revision != "v2" {
		t.Fatalf("entry = %+v", entry)
	}
	if _, err := latestSessionEntry("reg/skill/absent"); err == nil {
		t.Fatal("expected error for unrecorded ref")
	}
}
