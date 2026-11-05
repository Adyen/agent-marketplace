package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/sourcerevision"
)

func TestLockedNestedHookAndMCPInventory(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	origin, working := createBareRepoWithWorkingClone(t, "nested-inventory.git")
	writeFile(t, filepath.Join(working, ".claude-plugin", "plugin.json"), `{"name":"container","version":"1.0.0"}`)
	writeFile(t, filepath.Join(working, "plugins", "audit", "hooks", "hooks.json"), `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo audit"}]}]}}`)
	writeFile(t, filepath.Join(working, "plugins", "audit", ".mcp.json"), `{"mcpServers":{"nested-server":{"command":"fixture-server"}}}`)
	commitAndPushAll(t, working, "nested inventory")
	repo := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  origin: {git: file://"+origin+", ref: main, source_harness: claude}\nresources:\n  origin: {all: true}\n")
	withWorkingDir(t, repo)
	if out, err := executeCommand("lock"); err != nil {
		t.Fatalf("lock: %v\n%s", err, out)
	}
	lockPath := filepath.Join(repo, ".agents", "lock.yaml")
	doc := readExactManifestLock(t, lockPath)
	if len(doc.Registries) != 1 || len(doc.Registries[0].Roots) != 1 {
		t.Fatalf("unexpected roots: %+v", doc.Registries)
	}
	if len(doc.Registries[0].Roots[0].Resources) != 2 {
		t.Errorf("nested inventory missing: %+v", doc.Registries[0].Roots[0].Resources)
	}
	if out, err := executeCommand("sync", "--locked"); err != nil {
		t.Fatalf("locked sync: %v\n%s", err, out)
	}
	outputPath := filepath.Join(repo, ".factory", "mcp.json")
	before, err := os.ReadFile(outputPath)
	if err != nil || !strings.Contains(string(before), "fixture-server") {
		t.Fatalf("nested MCP was not materialized: %v", err)
	}
	empty := t.TempDir()
	writeFile(t, filepath.Join(empty, ".claude-plugin", "plugin.json"), `{"name":"container","version":"1.0.0"}`)
	legacy, err := sourcerevision.Build(sourcerevision.Input{SnapshotRoot: empty, SourceHarness: "claude"})
	if err != nil || len(legacy.Manifest.Resources) != 0 {
		t.Fatalf("empty legacy control failed: %v", err)
	}
	doc.Registries[0].Roots[0].RevisionDigest = lockfile.Digest(legacy.Manifest.RevisionDigest)
	doc.Registries[0].Roots[0].Resources = nil
	writeExactManifestLock(t, lockPath, doc)
	beforeLock, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	out, err := executeCommand("sync", "--locked")
	if !errors.Is(err, sourcerevision.ErrManifestMismatch) {
		t.Errorf("missing nested inventory accepted: %v\n%s", err, out)
	}
	after, err := os.ReadFile(outputPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("refusal changed installed output")
	}
	afterLock, err := os.ReadFile(lockPath)
	if err != nil || !bytes.Equal(beforeLock, afterLock) {
		t.Fatal("refusal changed locked inventory")
	}
	if out, err := executeCommand("lock", "--upgrade"); err != nil {
		t.Fatalf("explicit regeneration failed: %v\n%s", err, out)
	}
	upgraded := readExactManifestLock(t, lockPath)
	if upgraded.Registries[0].Commit != doc.Registries[0].Commit || len(upgraded.Registries[0].Roots[0].Resources) != 2 {
		t.Fatal("regeneration changed commit or omitted nested inventory")
	}
	if out, err := executeCommand("sync", "--locked"); err != nil {
		t.Fatalf("regenerated locked sync failed: %v\n%s", err, out)
	}
}
