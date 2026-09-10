package config

import (
	"errors"
	"fmt"
	"os"
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

func TestMigrateConfigFilesToV4ValidatesAlreadyV4LayerFieldsBeforePublishing(t *testing.T) {
	tests := []struct {
		name  string
		kind  V4MigrationScope
		paths func(string) MigratePaths
		body  string
		code  string
	}{
		{
			name:  "user marketplace",
			kind:  V4MigrationUser,
			paths: func(path string) MigratePaths { return MigratePaths{User: path} },
			body:  "apiVersion: agent-marketplace/v4\nsources: {market: {marketplace: https://market.example}}\nresources: {market: {include: [registry/skill/review]}}\n",
			code:  "v4.marketplace-lock-required",
		},
		{
			name:  "repo auxiliary",
			kind:  V4MigrationRepo,
			paths: func(path string) MigratePaths { return MigratePaths{Repo: path} },
			body:  "apiVersion: agent-marketplace/v4\nlogging: {level: debug}\nsources: {shared: {git: https://example.test/agents.git, ref: main}}\n",
			code:  "v4.layer-field",
		},
		{
			name:  "local harnesses",
			kind:  V4MigrationLocal,
			paths: func(path string) MigratePaths { return MigratePaths{Local: path} },
			body:  "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {shared: {git: https://example.test/agents.git, ref: main}}\n",
			code:  "v4.layer-field",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			writeV2Config(t, path, tt.body)
			before := readConfigString(t, path)
			_, err := MigrateConfigFilesToV4(tt.paths(path), tt.kind, false)
			var validation *V4ValidationError
			if !errors.As(err, &validation) || validation.Code != tt.code {
				t.Fatalf("error = %v, want %s", err, tt.code)
			}
			if got := readConfigString(t, path); got != before {
				t.Fatalf("already-v4 invalid config was written: %q", got)
			}
		})
	}
}

func TestMigrateConfigFilesToV4PreservesTenRegistryOrderAndPolicies(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	var input strings.Builder
	input.WriteString("apiVersion: agent-marketplace/v2\nregistries:\n")
	for i := 1; i <= 10; i++ {
		fmt.Fprintf(&input, "  - url: https://example.test/registry-%02d.git\n    ref: main\n    required: %t\n    skills: {include: [shared]}\n", i, i == 10)
	}
	writeV2Config(t, path, input.String())

	if _, err := MigrateConfigFilesToV4(MigratePaths{Repo: path}, V4MigrationRepo, false); err != nil {
		t.Fatal(err)
	}
	v4, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := v4.ToLegacyConfig(repoLayer)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(legacy.Registries), 10; got != want {
		t.Fatalf("registry count = %d, want %d", got, want)
	}
	for i, entry := range legacy.Registries {
		wantURL := fmt.Sprintf("https://example.test/registry-%02d.git", i+1)
		if entry.URL != wantURL {
			t.Fatalf("registry[%d] URL = %q, want %q", i, entry.URL, wantURL)
		}
		if entry.Required != (i == 9) {
			t.Fatalf("registry[%d] required = %t, want %t", i, entry.Required, i == 9)
		}
	}
}

func TestMigrateConfigFilesToV4LocalScopePreservesModeAndContent(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, ".agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	resourceDir := filepath.Join(dir, "resources")
	if err := os.MkdirAll(resourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(agentsDir, "config.local.yaml")
	before := `apiVersion: agent-marketplace/v2
materialization: {mode: file, targets: {factory: symlink}}
registries:
  - url: ./resources
    skills: {include: [review]}
`
	writeV2Config(t, path, before)
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}

	result, err := MigrateConfigFilesToV4(MigratePaths{Local: path}, V4MigrationLocal, false)
	if err != nil {
		t.Fatal(err)
	}
	if !result.LocalMigrated {
		t.Fatalf("result = %+v", result)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o640); got != want {
		t.Fatalf("mode = %o, want %o", got, want)
	}
	v4, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if v4.Materialization == nil || v4.Materialization.Mode != "file" || v4.Materialization.Targets["factory"] != "symlink" {
		t.Fatalf("materialization = %#v", v4.Materialization)
	}
	if got := v4.Groups["registry"].Include[0].Locator; got != "skill/review" {
		t.Fatalf("migrated content locator = %q", got)
	}
}

func TestMigrateConfigFilesToV4AllScopeLeavesLockfilesUnchanged(t *testing.T) {
	dir := t.TempDir()
	userPath := filepath.Join(dir, "user.yaml")
	repoPath := filepath.Join(dir, "repo.yaml")
	localPath := filepath.Join(dir, "local.yaml")
	for path, body := range map[string]string{
		userPath:  "apiVersion: agent-marketplace/v2\nharnesses: [factory]\nmaterialization: {mode: file}\nregistries:\n  - url: https://example.test/user.git\n    ref: main\n    skills: {include: [user-skill]}\n",
		repoPath:  "apiVersion: agent-marketplace/v2\nmaterialization: {mode: symlink}\nregistries:\n  - url: https://example.test/repo.git\n    ref: main\n    commands: {include: [repo-command]}\n",
		localPath: "apiVersion: agent-marketplace/v2\nmaterialization: {mode: file}\nregistries:\n  - url: /tmp/local-resources\n    skills: {include: [local-skill]}\n",
	} {
		writeV2Config(t, path, body)
	}
	lockPaths := []string{filepath.Join(dir, "user-lock.yaml"), filepath.Join(dir, "repo-lock.yaml"), filepath.Join(dir, "local-lock.yaml")}
	for _, path := range lockPaths {
		writeV2Config(t, path, "lock-before\n")
	}
	if _, err := MigrateConfigFilesToV4(MigratePaths{User: userPath, Repo: repoPath, Local: localPath}, V4MigrationAll, false); err != nil {
		t.Fatal(err)
	}
	for _, path := range lockPaths {
		if got := readConfigString(t, path); got != "lock-before\n" {
			t.Fatalf("lockfile %q changed to %q", path, got)
		}
	}
	for path, want := range map[string]string{
		userPath:  "user-skill",
		repoPath:  "repo-command",
		localPath: "local-skill",
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		if !strings.Contains(text, "apiVersion: agent-marketplace/v4") || !strings.Contains(text, want) || !strings.Contains(text, "materialization:") {
			t.Fatalf("%q lost migrated mode/content: %s", path, data)
		}
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
