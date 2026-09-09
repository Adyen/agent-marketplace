package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

func TestMigrateCommandPreservesValidV2UserAndRepo(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	globalAgentsDir := filepath.Join(homeDir, ".agents")
	if err := os.MkdirAll(globalAgentsDir, 0o755); err != nil {
		t.Fatalf("mkdir global agents: %v", err)
	}
	globalPath := filepath.Join(globalAgentsDir, "config.yaml")
	if err := os.WriteFile(globalPath, []byte("apiVersion: agent-marketplace/v2\nharnesses:\n  - factory\nregistries: []\n"), 0o644); err != nil {
		t.Fatalf("write v2 global config: %v", err)
	}

	repoRoot := t.TempDir()
	agentsDir := filepath.Join(repoRoot, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir repo agents: %v", err)
	}
	repoConfig := `apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: git@example.com:infra.git
    ref: main
skills:
  - vm-lifecycle
tags:
  - k8s
`
	if err := os.WriteFile(filepath.Join(agentsDir, "config.yaml"), []byte(repoConfig), 0o644); err != nil {
		t.Fatalf("write v2 repo config: %v", err)
	}

	withWorkingDir(t, repoRoot)

	cmd := newRootCmd()
	cmd.SetArgs([]string{"migrate"})
	var out strings.Builder
	cmd.SetOut(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected migrate command to succeed: %v", err)
	}

	repoCfg, err := config.LoadConfigFromPath(filepath.Join(agentsDir, "config.yaml"))
	if err != nil {
		t.Fatalf("expected migrated repo config to load: %v", err)
	}
	if repoCfg.APIVersion != config.APIVersionV2 {
		t.Fatalf("expected repo apiVersion v2, got %q", repoCfg.APIVersion)
	}
	if !equalStrings(repoCfg.Harnesses, []string{"factory"}) {
		t.Fatalf("expected repo harnesses to be preserved, got %v", repoCfg.Harnesses)
	}
	if !equalStrings(repoCfg.Tags, []string{"k8s"}) {
		t.Fatalf("expected tags preserved, got %v", repoCfg.Tags)
	}

	migratedData, err := os.ReadFile(filepath.Join(agentsDir, "config.yaml"))
	if err != nil {
		t.Fatalf("read migrated config: %v", err)
	}
	if !strings.Contains(string(migratedData), "- vm-lifecycle") {
		t.Fatalf("expected migrated config to preserve skills list, got:\n%s", migratedData)
	}

	globalCfg, err := config.LoadConfigFromPath(globalPath)
	if err != nil {
		t.Fatalf("expected migrated global config to load: %v", err)
	}
	if globalCfg.APIVersion != config.APIVersionV2 {
		t.Fatalf("expected global apiVersion v2, got %q", globalCfg.APIVersion)
	}
	if !equalStrings(globalCfg.Harnesses, []string{"factory"}) {
		t.Fatalf("expected global harnesses [factory], got %v", globalCfg.Harnesses)
	}
}

func TestMigrateCommandUsesExplicitHarnesses(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := t.TempDir()
	agentsDir := filepath.Join(repoRoot, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir repo agents: %v", err)
	}
	repoConfig := `apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra.git
    ref: main
`
	if err := os.WriteFile(filepath.Join(agentsDir, "config.yaml"), []byte(repoConfig), 0o644); err != nil {
		t.Fatalf("write v2 repo config: %v", err)
	}

	withWorkingDir(t, repoRoot)

	cmd := newRootCmd()
	cmd.SetArgs([]string{"migrate", "--harnesses", "factory,cursor"})
	var out strings.Builder
	cmd.SetOut(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected migrate command to succeed: %v", err)
	}

	globalPath := filepath.Join(homeDir, ".agents", "config.yaml")
	globalCfg, err := config.LoadConfigFromPath(globalPath)
	if err != nil {
		t.Fatalf("expected global config to load: %v", err)
	}
	if !equalStrings(globalCfg.Harnesses, []string{"factory", "cursor"}) {
		t.Fatalf("expected global harnesses [factory, cursor], got %v", globalCfg.Harnesses)
	}
}

func TestMigrateCommandRejectsV1(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	globalAgentsDir := filepath.Join(homeDir, ".agents")
	if err := os.MkdirAll(globalAgentsDir, 0o755); err != nil {
		t.Fatalf("mkdir global agents: %v", err)
	}
	globalPath := filepath.Join(globalAgentsDir, "config.yaml")
	if err := os.WriteFile(globalPath, []byte("apiVersion: agent-marketplace/v2\nharnesses:\n  - factory\nregistries: []\n"), 0o644); err != nil {
		t.Fatalf("write v2 global config: %v", err)
	}

	repoRoot := t.TempDir()
	agentsDir := filepath.Join(repoRoot, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir repo agents: %v", err)
	}
	v1Config := `registries:
  - url: git@example.com:infra.git
    ref: main
`
	if err := os.WriteFile(filepath.Join(agentsDir, "config.yaml"), []byte(v1Config), 0o644); err != nil {
		t.Fatalf("write v1 repo config: %v", err)
	}

	withWorkingDir(t, repoRoot)

	cmd := newRootCmd()
	cmd.SetArgs([]string{"migrate"})
	var out strings.Builder
	cmd.SetOut(&out)

	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected migrate command to fail for v1 input")
	}

	// Original repo file should remain unmodified.
	data, err := os.ReadFile(filepath.Join(agentsDir, "config.yaml"))
	if err != nil {
		t.Fatalf("read repo config: %v", err)
	}
	if strings.Contains(string(data), "apiVersion") {
		t.Fatalf("expected v1 repo file to remain unmodified")
	}
	// Original global file should remain unmodified.
	globalData, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("read global config: %v", err)
	}
	if !strings.Contains(string(globalData), "agent-marketplace/v2") {
		t.Fatalf("expected global file to remain unmodified")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestMigrateCommandRequiresHarnessesNonInteractive(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := t.TempDir()
	agentsDir := filepath.Join(repoRoot, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir repo agents: %v", err)
	}
	repoConfig := `apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra.git
    ref: main
`
	if err := os.WriteFile(filepath.Join(agentsDir, "config.yaml"), []byte(repoConfig), 0o644); err != nil {
		t.Fatalf("write v2 repo config: %v", err)
	}

	withWorkingDir(t, repoRoot)

	oldIsTerminal := isTerminal
	isTerminal = func(fd uintptr) bool { return false }
	defer func() { isTerminal = oldIsTerminal }()

	cmd := newRootCmd()
	cmd.SetArgs([]string{"migrate"})
	var out strings.Builder
	cmd.SetOut(&out)

	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected migrate command to fail without harnesses")
	} else if !strings.Contains(err.Error(), "--harnesses") {
		t.Fatalf("expected error to mention --harnesses, got %q", err.Error())
	}
}
