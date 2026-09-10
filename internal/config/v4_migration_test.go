package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyLoaderRejectsV4InstallationKeysWithMigrationHint(t *testing.T) {
	data := []byte(`apiVersion: agent-marketplace/v2
harnesses: [factory]
sources: {shared: {git: https://example.test/a.git, ref: main}}
resources: {shared: [skill/review]}
`)
	_, errs := loadV3ConfigWithAPIVersion(data, repoLayer, false, true)
	if len(errs) == 0 {
		t.Fatal("expected v2 loader to reject v4 installation keys")
	}
	joined := ""
	for _, err := range errs {
		joined += err.Error() + "\n"
	}
	if !strings.Contains(joined, "v4 installation field") || !strings.Contains(joined, "agent-marketplace/v4") {
		t.Fatalf("missing migration-oriented diagnostic: %s", joined)
	}
}

func TestV4RootRejectsLegacyInstallationKeys(t *testing.T) {
	_, err := ParseV4([]byte("apiVersion: agent-marketplace/v4\nskills: {include: [review]}\n"))
	if err == nil || !strings.Contains(err.Error(), "legacy installation key") {
		t.Fatalf("error = %v, want legacy-key migration diagnostic", err)
	}
}

func TestV4VersionBoundaryRejectsExplicitNonV4Include(t *testing.T) {
	_, err := parseV4Document([]byte("apiVersion: agent-marketplace/v3\n"), "fragment.yaml", false)
	if err == nil || !strings.Contains(err.Error(), "agent-marketplace/v4") {
		t.Fatalf("error = %v, want v4 version boundary", err)
	}
}

func TestMigrateConfigFilesToV4PreservesLegacySelectorsAndPolicies(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeV2Config(t, path, `apiVersion: agent-marketplace/v2
harnesses: [claude, factory]
tags: [platform]
allowed_registries: ["^https://"]
registries:
  - url: https://example.test/agents.git
    ref: main
    path: skills
    source_harness: claude
    required: true
    harnesses: [claude]
    skills: {include: [review], exclude: [old], exclude_all: false}
    commands: {exclude_all: true}
`)
	result, err := MigrateConfigFilesToV4(MigratePaths{User: path}, V4MigrationUser, false)
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	if !result.UserMigrated {
		t.Fatalf("migration result = %+v", result)
	}
	cfg, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatalf("load migrated v4: %v", err)
	}
	source := cfg.Sources["registry"]
	if source.Git != "https://example.test/agents.git" || source.Ref != "main" || source.Path != "skills" || source.SourceHarness != "claude" {
		t.Fatalf("source = %#v", source)
	}
	group := cfg.Groups["registry"]
	if group.Required == nil || !*group.Required || len(group.Include) != 1 || group.Include[0].Locator != "skill/review" {
		t.Fatalf("group = %#v", group)
	}
	if len(group.Exclude) != 2 || group.Exclude[0] != "skill/old" || group.Exclude[1] != "command/*" {
		t.Fatalf("exclusions = %#v", group.Exclude)
	}
	if len(group.Tags) != 1 || group.Tags[0] != "platform" || len(group.Harnesses) != 1 || group.Harnesses[0] != "claude" {
		t.Fatalf("policy = %#v", group)
	}
	loaded, err := LoadConfigFromPath(path)
	if err != nil {
		t.Fatalf("normal v4 load failed: %v", err)
	}
	if loaded.APIVersion != APIVersionV4 || len(loaded.Registries) != 1 || loaded.Registries[0].Skills.Include[0] != "review" {
		t.Fatalf("adapted config = %#v", loaded)
	}
}

func TestMigrateConfigFilesToV4RefusesIncludeWithoutWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	before := "apiVersion: agent-marketplace/v2\ninclude: fragment.yaml\n"
	writeV2Config(t, path, before)
	writeV2Config(t, filepath.Join(dir, "fragment.yaml"), "apiVersion: agent-marketplace/v2\nregistries: []\n")
	_, err := MigrateConfigFilesToV4(MigratePaths{Repo: path}, V4MigrationRepo, false)
	if err == nil || !strings.Contains(err.Error(), "v4.migration-unsupported") {
		t.Fatalf("error = %v, want stable unsupported diagnostic", err)
	}
	if got := readConfigString(t, path); got != before {
		t.Fatalf("config changed after refused migration: %q", got)
	}
}

func TestMigrateConfigFilesToV4AllScopePublishesNothingWhenOneLayerIsUnsupported(t *testing.T) {
	dir := t.TempDir()
	userPath := filepath.Join(dir, "user.yaml")
	repoPath := filepath.Join(dir, "repo.yaml")
	userBefore := "apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries: []\n"
	repoBefore := "apiVersion: agent-marketplace/v2\nmarketplaces:\n  - url: https://market.example\n    instance_id: instance\n"
	writeV2Config(t, userPath, userBefore)
	writeV2Config(t, repoPath, repoBefore)
	userLockPath := filepath.Join(dir, "user-lock.yaml")
	repoLockPath := filepath.Join(dir, "repo-lock.yaml")
	writeV2Config(t, userLockPath, "lock-before-user")
	writeV2Config(t, repoLockPath, "lock-before-repo")
	_, err := MigrateConfigFilesToV4(MigratePaths{User: userPath, Repo: repoPath}, V4MigrationAll, false)
	if err == nil || !strings.Contains(err.Error(), "v4.migration-marketplace") {
		t.Fatalf("error = %v, want marketplace refusal", err)
	}
	if got := readConfigString(t, userPath); got != userBefore {
		t.Fatalf("user was published before validation completed: %q", got)
	}
	if got := readConfigString(t, repoPath); got != repoBefore {
		t.Fatalf("repo changed after refusal: %q", got)
	}
	if got := readConfigString(t, userLockPath); got != "lock-before-user" {
		t.Fatalf("user lock changed after refusal: %q", got)
	}
	if got := readConfigString(t, repoLockPath); got != "lock-before-repo" {
		t.Fatalf("repo lock changed after refusal: %q", got)
	}
}
