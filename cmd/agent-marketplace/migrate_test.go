package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	mptestutil "github.com/Adyen/agent-marketplace/internal/testutil/marketplace"
)

func TestMigrateCommandMarketplaceEndToEndUsesVerifiedCache(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, ".cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, ".local", "state"))

	// Verified mirror artifact and revision metadata in the default cache root.
	seed := mptestutil.SeedVerifiedMarketplaceSkill(t, filepath.Join(cache.CacheDir(), "marketplace"),
		"https://market.example", "mp", "src", "review")
	contentDigest, archiveDigest := seed.ContentDigest, seed.ArchiveDigest

	repoRoot := t.TempDir()
	agentsDir := filepath.Join(repoRoot, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := "apiVersion: agent-marketplace/v2\nmarketplaces:\n" +
		"  - url: https://market.example\n    instance_id: mp\n    alias: adyen\n    resources:\n" +
		"      - registry_id: src\n        registry_slug: tools\n        kind: skill\n        name: review\n" +
		"        revision: " + contentDigest + "\n        digest: " + contentDigest + "\n        direct: true\n"
	if err := os.WriteFile(filepath.Join(agentsDir, "config.yaml"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	withWorkingDir(t, repoRoot)

	cmd := newRootCmd()
	cmd.SetArgs([]string{"migrate", "--scope", "repo"})
	var out strings.Builder
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}

	migrated, err := config.LoadConfigFromPath(filepath.Join(agentsDir, "config.yaml"))
	if err != nil {
		t.Fatalf("hydrate migrated config+lock: %v", err)
	}
	if len(migrated.Marketplaces) != 1 || len(migrated.Marketplaces[0].Resources) != 1 {
		t.Fatalf("hydrated marketplaces = %#v", migrated.Marketplaces)
	}
	pin := migrated.Marketplaces[0].Resources[0]
	if !pin.Direct || pin.RegistrySlug != "tools" || pin.LockArchiveDigest != archiveDigest {
		t.Fatalf("hydrated pin = %#v", pin)
	}
	if _, err := os.Stat(filepath.Join(agentsDir, "lock.yaml")); err != nil {
		t.Fatalf("expected generated lock: %v", err)
	}
}

func TestMigrateCommandDefaultsToV4ForUserAndRepo(t *testing.T) {
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
	if repoCfg.APIVersion != config.APIVersionV4 {
		t.Fatalf("expected repo apiVersion v4, got %q", repoCfg.APIVersion)
	}
	if !equalStrings(repoCfg.Harnesses, []string{"factory"}) {
		t.Fatalf("expected repo harnesses to be preserved, got %v", repoCfg.Harnesses)
	}
	v4Cfg, err := config.LoadV4ConfigFromPath(filepath.Join(agentsDir, "config.yaml"))
	if err != nil {
		t.Fatalf("expected migrated v4 config to load: %v", err)
	}
	if !equalStrings(v4Cfg.Groups["infra"].Tags, []string{"k8s"}) {
		t.Fatalf("expected tags preserved in v4 group, got %v", v4Cfg.Groups["infra"].Tags)
	}

	migratedData, err := os.ReadFile(filepath.Join(agentsDir, "config.yaml"))
	if err != nil {
		t.Fatalf("read migrated config: %v", err)
	}
	if !strings.Contains(string(migratedData), "skill/vm-lifecycle") {
		t.Fatalf("expected migrated config to preserve skills list, got:\n%s", migratedData)
	}

	globalCfg, err := config.LoadConfigFromPath(globalPath)
	if err != nil {
		t.Fatalf("expected migrated global config to load: %v", err)
	}
	if globalCfg.APIVersion != config.APIVersionV4 {
		t.Fatalf("expected global apiVersion v4, got %q", globalCfg.APIVersion)
	}
	if !equalStrings(globalCfg.Harnesses, []string{"factory"}) {
		t.Fatalf("expected global harnesses [factory], got %v", globalCfg.Harnesses)
	}
}

func TestReadOnlySyncCommandsPreserveLegacyConfigBytesAtRootBoundary(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}

	for _, args := range [][]string{{"sync", "--check"}, {"sync", "--locked"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, ".cache"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, ".local", "state"))
			repoRoot := t.TempDir()
			configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
			if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
				t.Fatal(err)
			}
			before := []byte("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries: []\n")
			if err := os.WriteFile(configPath, before, 0o644); err != nil {
				t.Fatal(err)
			}
			withWorkingDir(t, repoRoot)

			oldIsTerminal := isTerminal
			isTerminal = func(uintptr) bool { return true }
			t.Cleanup(func() { isTerminal = oldIsTerminal })
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := writer.WriteString("y\n"); err != nil {
				t.Fatal(err)
			}
			_ = writer.Close()
			t.Cleanup(func() { _ = reader.Close() })

			cmd := newRootCmd()
			cmd.SetArgs(args)
			cmd.SetIn(reader)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("%v failed: %v", args, err)
			}
			after, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatalf("%v changed legacy config bytes:\nbefore:\n%s\nafter:\n%s", args, before, after)
			}
		})
	}
}

func TestPromptForV4MigrationAcceptsAndMigrates(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	dir := t.TempDir()
	path := filepath.Join(dir, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("apiVersion: agent-marketplace/v2\nregistries: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := runV4MigrationPrompt(context.Background(), &out, strings.NewReader("\n"), true, config.MigratePaths{Repo: path}); err != nil {
		t.Fatalf("prompt migration failed: %v", err)
	}
	migrated, err := config.LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatalf("load migrated config: %v", err)
	}
	if migrated.APIVersion != config.APIVersionV4 || !strings.Contains(out.String(), "Migrate") {
		t.Fatalf("migration output/config = %q / %#v", out.String(), migrated)
	}
}

func TestPromptForV4MigrationDeclineLeavesBytesUnchanged(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	before := []byte("apiVersion: agent-marketplace/v2\nregistries: []\n")
	if err := os.WriteFile(path, before, 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := runV4MigrationPrompt(context.Background(), &out, strings.NewReader("n\n"), true, config.MigratePaths{Repo: path}); err != nil {
		t.Fatalf("prompt migration failed: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(before) {
		t.Fatalf("decline changed config: %q, %v", got, err)
	}
}

func TestPromptForV4MigrationNonInteractiveWarnsAndLeavesBytes(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	before := []byte("apiVersion: agent-marketplace/v2\nregistries: []\n")
	if err := os.WriteFile(path, before, 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := runV4MigrationPrompt(context.Background(), &out, strings.NewReader(""), false, config.MigratePaths{Repo: path}); err != nil {
		t.Fatalf("noninteractive migration failed: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("noninteractive prompt unexpectedly wrote output = %q", out.String())
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(before) {
		t.Fatalf("noninteractive path changed config: %q, %v", got, err)
	}
}

func TestMigrateCommandToV4IsExplicitAndSupportsDryRun(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()
	agentsDir := filepath.Join(repoRoot, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(agentsDir, "config.yaml")
	before := "apiVersion: agent-marketplace/v2\nregistries:\n  - url: https://example.test/agents.git\n    ref: main\n    skills: {include: [review]}\n"
	if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	withWorkingDir(t, repoRoot)

	cmd := newRootCmd()
	cmd.SetArgs([]string{"migrate", "--to", "v4", "--scope", "repo", "--dry-run"})
	var out strings.Builder
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	if !strings.Contains(out.String(), "Would migrate repository config") {
		t.Fatalf("dry-run output = %q", out.String())
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != before {
		t.Fatalf("dry-run changed config: %q, %v", got, err)
	}

	cmd = newRootCmd()
	cmd.SetArgs([]string{"migrate", "--to", "v4", "--scope", "repo"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("v4 migration failed: %v", err)
	}
	migrated, err := config.LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatalf("migrated config is not v4: %v", err)
	}
	if migrated.Sources["agents"].Git != "https://example.test/agents.git" {
		t.Fatalf("migrated source = %#v", migrated.Sources["agents"])
	}
}

func TestMigrateCommandToV4FailureLeavesBytesUnchanged(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()
	agentsDir := filepath.Join(repoRoot, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(agentsDir, "config.yaml")
	before := "apiVersion: agent-marketplace/v2\ninclude: child.yaml\n"
	if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsDir, "child.yaml"), []byte("apiVersion: agent-marketplace/v2\nregistries: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	withWorkingDir(t, repoRoot)
	cmd := newRootCmd()
	cmd.SetArgs([]string{"migrate", "--to", "v4", "--scope", "repo"})
	var out strings.Builder
	cmd.SetOut(&out)
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "v4.migration-unsupported") {
		t.Fatalf("error = %v, output = %q", err, out.String())
	}
	if got, readErr := os.ReadFile(path); readErr != nil || string(got) != before {
		t.Fatalf("failed migration changed config: %q, %v", got, readErr)
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
	cmd.SetArgs([]string{"migrate", "--to", "v2", "--harnesses", "factory,cursor"})
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
	cmd.SetArgs([]string{"migrate", "--to", "v2"})
	var out strings.Builder
	cmd.SetOut(&out)

	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected migrate command to fail without harnesses")
	} else if !strings.Contains(err.Error(), "--harnesses") {
		t.Fatalf("expected error to mention --harnesses, got %q", err.Error())
	}
}
