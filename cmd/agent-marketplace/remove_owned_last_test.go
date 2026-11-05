package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveBlockedOwnedMCPLastAndInferredKind(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".state"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	origin, working := createV4GitRegistry(t, "owned-last-mcp.git")
	writeFile(t, filepath.Join(working, ".mcp.json"), "{\n  \"mcpServers\": {\"remove\": {\"command\": \"remove-binary\"}}\n}\n")
	commitAndPushAll(t, working, "seed MCP")
	repo := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "\nharnesses: [devin]\nsources:\n  registry:\n    git: "+origin+"\n    ref: main\nresources:\n  registry:\n    all: true\n")
	withWorkingDir(t, repo)
	if out, err := executeCommand("sync"); err != nil {
		t.Fatalf("seed sync: %v\n%s", err, out)
	}
	writeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "\nharnesses: [devin]\nallowed_registries: []\n")
	if out, err := executeCommand("remove", "remove", "--from", origin); err != nil {
		t.Fatalf("remove: %v\n%s", err, out)
	}
	raw, err := os.ReadFile(filepath.Join(repo, ".devin", "mcp_config.json"))
	if err != nil || bytes.Contains(raw, []byte("remove-binary")) {
		t.Fatalf("aggregate=%q error=%v", raw, err)
	}
}
