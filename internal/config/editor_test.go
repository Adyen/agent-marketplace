package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"gopkg.in/yaml.v3"
)

// loadProjectConfig loads a project (repo) config without requiring harnesses.
func loadProjectConfig(t testing.TB, path string) (*Config, error) {
	t.Helper()
	return loadConfig(path, false)
}

func TestAddSkillToConfigAppendsSkillAndPreservesComments(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`# repo config
apiVersion: agent-marketplace/v2
registries:
  # infra registry
  - url: git@example.com:infra.git
    ref: main

skills:
  - vm-lifecycle # existing skill

# keep tag comment
tags:
  - k8s
`))

	if err := AddSkillToConfig(configPath, "puppet-module-conventions"); err != nil {
		t.Fatalf("expected AddSkillToConfig to succeed: %v", err)
	}

	cfg, err := loadProjectConfig(t, configPath)
	if err != nil {
		t.Fatalf("expected edited config to load: %v", err)
	}

	if !reflect.DeepEqual(cfg.Tags, []string{"k8s"}) {
		t.Fatalf("expected tags to remain unchanged, got %#v", cfg.Tags)
	}

	// Verify skills were appended by checking raw YAML content,
	// since Config no longer has a top-level Skills field.
	edited, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected edited config to be readable: %v", err)
	}
	for _, needle := range []string{"vm-lifecycle", "puppet-module-conventions"} {
		if !strings.Contains(string(edited), needle) {
			t.Fatalf("expected edited config to contain skill %q, got:\n%s", needle, edited)
		}
	}

	for _, needle := range []string{"# repo config", "# infra registry", "# existing skill", "# keep tag comment"} {
		if !strings.Contains(string(edited), needle) {
			t.Fatalf("expected edited config to preserve comment %q, got:\n%s", needle, edited)
		}
	}
}

func TestAddTagToConfigCreatesTagsSectionWhenMissing(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra.git
    ref: main

skills:
  - vm-lifecycle
`))

	if err := AddTagToConfig(configPath, "network"); err != nil {
		t.Fatalf("expected AddTagToConfig to succeed: %v", err)
	}

	cfg, err := loadProjectConfig(t, configPath)
	if err != nil {
		t.Fatalf("expected edited config to load: %v", err)
	}

	if !reflect.DeepEqual(cfg.Tags, []string{"network"}) {
		t.Fatalf("expected tags to include appended value, got %#v", cfg.Tags)
	}

	edited, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected edited config to be readable: %v", err)
	}

	if !strings.Contains(string(edited), "tags:\n  - network\n") {
		t.Fatalf("expected edited config to include tags section, got:\n%s", edited)
	}
}

func TestValidateScopeBoundaryBlocksRepoScopeForMachineSkill(t *testing.T) {
	err := ValidateScopeBoundary(UserPersonal, RepoCommitted)
	if err == nil {
		t.Fatalf("expected scope boundary validation to fail")
	}
	boundaryErr, ok := err.(*ScopeBoundaryError)
	if !ok {
		t.Fatalf("expected ScopeBoundaryError, got %T", err)
	}
	if boundaryErr.Source != UserPersonal || boundaryErr.Target != RepoCommitted {
		t.Fatalf("unexpected boundary error payload: %#v", boundaryErr)
	}
}

func TestValidateScopeBoundaryAllowsUserScopeForMachineSkill(t *testing.T) {
	if err := ValidateScopeBoundary(UserPersonal, UserPersonal); err != nil {
		t.Fatalf("expected user scope to be allowed for machine-scope skill: %v", err)
	}
}

func TestAddSkillsToConfigAppendsMultipleSkillsAtomically(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`# repo config
apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra.git
    ref: main

skills:
  - vm-lifecycle
`))

	if err := AddSkillsToConfig(configPath, []string{"k8s-app-onboarding", "vm-lifecycle", "network-change-workflow"}); err != nil {
		t.Fatalf("expected AddSkillsToConfig to succeed: %v", err)
	}

	// Verify skills were appended by checking raw YAML content,
	// since Config no longer has a top-level Skills field.
	edited, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected edited config to be readable: %v", err)
	}
	for _, needle := range []string{"vm-lifecycle", "k8s-app-onboarding", "network-change-workflow"} {
		if !strings.Contains(string(edited), needle) {
			t.Fatalf("expected edited config to contain skill %q, got:\n%s", needle, edited)
		}
	}
}

func TestAddSkillsToRegistryInConfigCreatesRegistryOverride(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra.git
    ref: main
  - url: git@example.com:security.git
    ref: main

skills:
  - vm-lifecycle
`))

	if err := AddSkillsToRegistryInConfig(configPath, "git@example.com:security.git", "", []string{"secret-scanning", "vm-lifecycle"}); err != nil {
		t.Fatalf("expected AddSkillsToRegistryInConfig to succeed: %v", err)
	}

	cfg, err := loadProjectConfig(t, configPath)
	if err != nil {
		t.Fatalf("expected edited config to load: %v", err)
	}

	// Top-level skills must NOT leak into the registry entry's YAML.
	// Attribute inheritance is a resolver-time concern, not a config
	// persistence concern. The entry should only have the explicitly
	// added values.
	if got := cfg.Registries[1].Skills.Include; !reflect.DeepEqual(got, []string{"secret-scanning", "vm-lifecycle"}) {
		t.Fatalf("expected registry entry to contain only explicitly added skills, got %#v", got)
	}
}

func TestAddSkillsToRegistryInConfigPathVariants(t *testing.T) {
	tests := []struct {
		name         string
		configYAML   string
		registryURL  string
		registryPath string
		skills       []string
		wantErr      string   // non-empty: expect error containing this substring
		wantSkillsAt int      // on success: registry index to verify
		wantSkills   []string // on success: expected Skills.Include
	}{
		{
			name: "non_empty_path_selects_matching_entry",
			configYAML: `apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:shared.git
    ref: main
    path: commands
  - url: git@example.com:shared.git
    ref: main
    path: skills
`,
			registryURL:  "git@example.com:shared.git",
			registryPath: "skills",
			skills:       []string{"my-skill"},
			wantSkillsAt: 1,
			wantSkills:   []string{"my-skill"},
		},
		{
			name: "ambiguous_url_empty_path_returns_error",
			configYAML: `apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:shared.git
    ref: main
    path: commands
  - url: git@example.com:shared.git
    ref: main
    path: skills
`,
			registryURL:  "git@example.com:shared.git",
			registryPath: "",
			skills:       []string{"my-skill"},
			wantErr:      "matches multiple entries",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
			mustMkdirAll(t, filepath.Dir(configPath))
			mustWriteFile(t, configPath, []byte(tt.configYAML))

			err := AddSkillsToRegistryInConfig(configPath, tt.registryURL, tt.registryPath, tt.skills)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error to contain %q, got: %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected success, got: %v", err)
			}

			cfg, err := loadProjectConfig(t, configPath)
			if err != nil {
				t.Fatalf("load config: %v", err)
			}
			got := cfg.Registries[tt.wantSkillsAt].Skills.Include
			if !reflect.DeepEqual(got, tt.wantSkills) {
				t.Fatalf("registry[%d].Skills.Include: expected %#v, got %#v",
					tt.wantSkillsAt, tt.wantSkills, got)
			}
		})
	}
}

func TestAddSkillToLocalConfigCreatesFile(t *testing.T) {
	repoRoot := t.TempDir()

	if err := AddSkillToLocalConfig(repoRoot, "my-skill"); err != nil {
		t.Fatalf("AddSkillToLocalConfig: %v", err)
	}

	path := LocalConfigPath(repoRoot)
	cfg, err := loadLocalConfig(path)
	if err != nil {
		t.Fatalf("expected local config to load: %v", err)
	}
	if !reflect.DeepEqual(cfg.Skills.Include, []string{"my-skill"}) {
		t.Fatalf("expected Skills=[my-skill], got %#v", cfg.Skills.Include)
	}
	if len(cfg.Skills.Exclude) != 0 {
		t.Fatalf("expected empty Skills.Exclude, got %#v", cfg.Skills.Exclude)
	}
}

func TestAddSkillToLocalConfigIsIdempotent(t *testing.T) {
	repoRoot := t.TempDir()

	if err := AddSkillToLocalConfig(repoRoot, "my-skill"); err != nil {
		t.Fatalf("first AddSkillToLocalConfig: %v", err)
	}
	if err := AddSkillToLocalConfig(repoRoot, "my-skill"); err != nil {
		t.Fatalf("second AddSkillToLocalConfig: %v", err)
	}

	cfg, err := loadLocalConfig(LocalConfigPath(repoRoot))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !reflect.DeepEqual(cfg.Skills.Include, []string{"my-skill"}) {
		t.Fatalf("expected skill listed once after double add, got %#v", cfg.Skills.Include)
	}
}

func TestRemoveSkillFromLocalConfigIsIdempotent(t *testing.T) {
	repoRoot := t.TempDir()

	// Remove from nonexistent file — no-op.
	if err := RemoveSkillFromLocalConfig(repoRoot, "absent-skill"); err != nil {
		t.Fatalf("remove from missing file should succeed: %v", err)
	}

	// Add then remove twice — the second remove is a no-op.
	if err := AddSkillToLocalConfig(repoRoot, "my-skill"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := RemoveSkillFromLocalConfig(repoRoot, "my-skill"); err != nil {
		t.Fatalf("first remove: %v", err)
	}
	if err := RemoveSkillFromLocalConfig(repoRoot, "my-skill"); err != nil {
		t.Fatalf("second remove: %v", err)
	}

	cfg, err := loadLocalConfig(LocalConfigPath(repoRoot))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Skills.Include) != 0 {
		t.Fatalf("expected empty Skills after removes, got %#v", cfg.Skills.Include)
	}
}

func TestLocalConfigHoldsSkillsAndExcludesTogether(t *testing.T) {
	repoRoot := t.TempDir()

	if err := AddSkillToLocalConfig(repoRoot, "personal-skill"); err != nil {
		t.Fatalf("add skill: %v", err)
	}
	if err := AddToLocalExclude(repoRoot, "blocked-skill"); err != nil {
		t.Fatalf("add exclude: %v", err)
	}

	cfg, err := loadLocalConfig(LocalConfigPath(repoRoot))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if !reflect.DeepEqual(cfg.Skills.Include, []string{"personal-skill"}) {
		t.Fatalf("expected Skills.Include=[personal-skill], got %#v", cfg.Skills.Include)
	}
	if !reflect.DeepEqual(cfg.Skills.Exclude, []string{"blocked-skill"}) {
		t.Fatalf("expected Skills.Exclude=[blocked-skill], got %#v", cfg.Skills.Exclude)
	}
}

func TestRemoveFromLocalExcludeIdempotent(t *testing.T) {
	repoRoot := t.TempDir()

	if err := RemoveFromLocalExclude(repoRoot, "absent"); err != nil {
		t.Fatalf("remove from missing file: %v", err)
	}

	if err := AddToLocalExclude(repoRoot, "banned"); err != nil {
		t.Fatalf("add exclude: %v", err)
	}
	if err := RemoveFromLocalExclude(repoRoot, "banned"); err != nil {
		t.Fatalf("remove exclude: %v", err)
	}
	if err := RemoveFromLocalExclude(repoRoot, "banned"); err != nil {
		t.Fatalf("double remove: %v", err)
	}

	cfg, err := loadLocalConfig(LocalConfigPath(repoRoot))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Skills.Exclude) != 0 {
		t.Fatalf("expected empty Skills.Exclude, got %#v", cfg.Skills.Exclude)
	}
}

func TestAddSkillToGlobalConfigUsesHomeDir(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	if err := AddSkillToGlobalConfig("global-skill"); err != nil {
		t.Fatalf("AddSkillToGlobalConfig: %v", err)
	}

	// Verify the skill was written by checking raw YAML content,
	// since Config no longer has a top-level Skills field.
	path := filepath.Join(tmpHome, ".agents", "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read global config: %v", err)
	}
	if !strings.Contains(string(data), "global-skill") {
		t.Fatalf("expected global config to contain 'global-skill', got:\n%s", data)
	}
}

func TestRemoveSkillFromGlobalConfigIdempotent(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	// Remove from nonexistent file.
	if err := RemoveSkillFromGlobalConfig("absent"); err != nil {
		t.Fatalf("remove from missing global config: %v", err)
	}

	// Round-trip.
	if err := AddSkillToGlobalConfig("x"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := RemoveSkillFromGlobalConfig("x"); err != nil {
		t.Fatalf("first remove: %v", err)
	}
	if err := RemoveSkillFromGlobalConfig("x"); err != nil {
		t.Fatalf("second remove: %v", err)
	}
}

func TestRemoveSkillFromConfigWritesBlankWhenDocumentBecomesEmpty(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`skills:
  - only-skill
`))

	if err := RemoveSkillFromConfig(configPath, "only-skill"); err != nil {
		t.Fatalf("remove last skill: %v", err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.TrimSpace(string(content)) != "" {
		t.Fatalf("expected blank config file, got %q", string(content))
	}
}

func TestRemoveSkillFromRegistryInConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra.git
    ref: main
    skills:
      - audit
      - deploy
  - url: git@example.com:security.git
    ref: main
    skills:
      - secret-scanning
`))

	if err := RemoveSkillFromRegistryInConfig(configPath, "git@example.com:infra.git", "", "audit"); err != nil {
		t.Fatalf("RemoveSkillFromRegistryInConfig: %v", err)
	}

	cfg, err := loadProjectConfig(t, configPath)
	if err != nil {
		t.Fatalf("expected edited config to load: %v", err)
	}

	if !reflect.DeepEqual(cfg.Registries[0].Skills.Include, []string{"deploy"}) {
		t.Fatalf("expected registry skills to have audit removed, got %#v", cfg.Registries[0].Skills.Include)
	}
	if !reflect.DeepEqual(cfg.Registries[1].Skills.Include, []string{"secret-scanning"}) {
		t.Fatalf("expected other registry skills unchanged, got %#v", cfg.Registries[1].Skills.Include)
	}
}

func TestRemoveSkillFromRegistryInConfigRemovesEmptySkillsKey(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra.git
    ref: main
    skills:
      - audit
`))

	if err := RemoveSkillFromRegistryInConfig(configPath, "git@example.com:infra.git", "", "audit"); err != nil {
		t.Fatalf("RemoveSkillFromRegistryInConfig: %v", err)
	}

	cfg, err := loadProjectConfig(t, configPath)
	if err != nil {
		t.Fatalf("expected edited config to load: %v", err)
	}

	if !cfg.Registries[0].Skills.IsZero() {
		t.Fatalf("expected empty skills after removing last skill, got %#v", cfg.Registries[0].Skills)
	}
}

func TestRemoveSkillFromRegistryInConfigIdempotent(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra.git
    ref: main
    skills:
      - audit
`))

	// Remove twice: should be idempotent.
	if err := RemoveSkillFromRegistryInConfig(configPath, "git@example.com:infra.git", "", "audit"); err != nil {
		t.Fatalf("first remove: %v", err)
	}
	if err := RemoveSkillFromRegistryInConfig(configPath, "git@example.com:infra.git", "", "audit"); err != nil {
		t.Fatalf("second remove: %v", err)
	}
}

func TestEnsureRegistryInConfigAddsEntry(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:existing.git
    ref: main
`))

	if err := EnsureRegistryInConfig(configPath, "git@example.com:new.git", "v2"); err != nil {
		t.Fatalf("EnsureRegistryInConfig: %v", err)
	}

	cfg, err := loadProjectConfig(t, configPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Registries) != 2 {
		t.Fatalf("expected 2 registries, got %d", len(cfg.Registries))
	}
	if cfg.Registries[1].URL != "git@example.com:new.git" {
		t.Fatalf("expected second registry URL, got %q", cfg.Registries[1].URL)
	}
	if cfg.Registries[1].Ref != "v2" {
		t.Fatalf("expected ref=v2, got %q", cfg.Registries[1].Ref)
	}
}

func TestEnsureRegistryInConfigIdempotent(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:existing.git
    ref: main
`))

	if err := EnsureRegistryInConfig(configPath, "git@example.com:existing.git", "main"); err != nil {
		t.Fatalf("EnsureRegistryInConfig: %v", err)
	}

	cfg, err := loadProjectConfig(t, configPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Registries) != 1 {
		t.Fatalf("expected 1 registry (idempotent), got %d", len(cfg.Registries))
	}
}

func TestEnsureRegistryWithPathsInConfigRejectsRepoRelativeGlobalWrite(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	configPath, err := GlobalConfigPath()
	if err != nil {
		t.Fatalf("GlobalConfigPath: %v", err)
	}

	err = EnsureRegistryWithPathsInConfig(configPath, "./relative.git", "main", "skills")
	if _, ok := err.(*GlobalRelativeRegistryError); !ok {
		t.Fatalf("expected GlobalRelativeRegistryError, got %T: %v", err, err)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("global config was written: %v", err)
	}
}

func TestDisableResourceWithShadowFallbackRejectsRepoRelativeGlobalWrite(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	configPath, err := GlobalConfigPath()
	if err != nil {
		t.Fatalf("GlobalConfigPath: %v", err)
	}

	err = DisableResourceWithShadowFallback(configPath, "./relative", "", "", ir.KindSkill, "audit")
	var globalRelativeErr *GlobalRelativeRegistryError
	if !errors.As(err, &globalRelativeErr) {
		t.Fatalf("expected GlobalRelativeRegistryError, got %T: %v", err, err)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("global config was written: %v", err)
	}
}

func TestEnsureRegistryWithPathsInConfigAddsPath(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:existing.git
    ref: main
`))

	if err := EnsureRegistryWithPathsInConfig(configPath, "git@example.com:new.git", "main", "skills"); err != nil {
		t.Fatalf("EnsureRegistryWithPathsInConfig: %v", err)
	}

	cfg, err := loadProjectConfig(t, configPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Registries) != 2 {
		t.Fatalf("expected 2 registries, got %d", len(cfg.Registries))
	}
	got := cfg.Registries[1]
	if got.URL != "git@example.com:new.git" {
		t.Fatalf("expected URL, got %q", got.URL)
	}
	if got.Ref != "main" {
		t.Fatalf("expected ref=main, got %q", got.Ref)
	}
	if got.Path != "skills" {
		t.Fatalf("expected path=skills, got %q", got.Path)
	}
}

func TestEnsureRegistryWithPathsInConfigOmitsEmptyPath(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries: []
`))

	if err := EnsureRegistryWithPathsInConfig(configPath, "git@example.com:new.git", "main", ""); err != nil {
		t.Fatalf("EnsureRegistryWithPathsInConfig: %v", err)
	}

	cfg, err := loadProjectConfig(t, configPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Registries[0].Path != "" {
		t.Fatalf("expected empty path when omitted, got %q", cfg.Registries[0].Path)
	}
}

func TestEnsureRegistryWithPathsInConfigIdempotentSameURLSamePath(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:existing.git
    ref: main
    path: skills
`))

	// Same URL and same path — must be a no-op.
	if err := EnsureRegistryWithPathsInConfig(configPath, "git@example.com:existing.git", "main", "skills"); err != nil {
		t.Fatalf("EnsureRegistryWithPathsInConfig: %v", err)
	}

	cfg, err := loadProjectConfig(t, configPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Registries) != 1 {
		t.Fatalf("expected 1 registry (idempotent same path), got %d", len(cfg.Registries))
	}
	if cfg.Registries[0].Path != "skills" {
		t.Fatalf("expected path=skills unchanged, got %q", cfg.Registries[0].Path)
	}
}

func TestEnsureRegistryWithPathsInConfigAddsDifferentPathEntry(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:existing.git
    ref: main
    path: skills
`))

	// Same URL but different path — must add a second entry so both sub-paths coexist.
	if err := EnsureRegistryWithPathsInConfig(configPath, "git@example.com:existing.git", "main", "commands"); err != nil {
		t.Fatalf("EnsureRegistryWithPathsInConfig: %v", err)
	}

	cfg, err := loadProjectConfig(t, configPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Registries) != 2 {
		t.Fatalf("expected 2 registries (different paths), got %d", len(cfg.Registries))
	}
	if cfg.Registries[0].Path != "skills" {
		t.Fatalf("expected first entry path=skills unchanged, got %q", cfg.Registries[0].Path)
	}
	if cfg.Registries[1].Path != "commands" {
		t.Fatalf("expected second entry path=commands, got %q", cfg.Registries[1].Path)
	}
}

func TestEnsureRegistryWithPathsInConfigAddsRootWhenSubPathExists(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:existing.git
    ref: main
    path: skills/firewall-rules-management
`))

	// Same URL but root-level (empty path) — must add a second entry.
	if err := EnsureRegistryWithPathsInConfig(configPath, "git@example.com:existing.git", "main", ""); err != nil {
		t.Fatalf("EnsureRegistryWithPathsInConfig: %v", err)
	}

	cfg, err := loadProjectConfig(t, configPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Registries) != 2 {
		t.Fatalf("expected 2 registries (sub-path + root), got %d", len(cfg.Registries))
	}
	if cfg.Registries[0].Path != "skills/firewall-rules-management" {
		t.Fatalf("expected first entry path unchanged, got %q", cfg.Registries[0].Path)
	}
	if cfg.Registries[1].Path != "" {
		t.Fatalf("expected second entry path empty (root), got %q", cfg.Registries[1].Path)
	}
}

func TestEnsureRegistryWithDefaultInConfigSetsDefault(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries: []
`))

	if err := EnsureRegistryWithDefaultInConfig(configPath, "git@example.com:new.git", "main", "skills"); err != nil {
		t.Fatalf("EnsureRegistryWithDefaultInConfig: %v", err)
	}

	cfg, err := loadProjectConfig(t, configPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Registries) != 1 {
		t.Fatalf("expected 1 registry, got %d", len(cfg.Registries))
	}
	got := cfg.Registries[0]
	if got.URL != "git@example.com:new.git" {
		t.Fatalf("expected URL, got %q", got.URL)
	}
	if !got.Default {
		t.Fatalf("expected default=true, got false")
	}
	if got.Path != "skills" {
		t.Fatalf("expected path=skills, got %q", got.Path)
	}
}

func TestEnsureRegistryWithDefaultInConfigIdempotent(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:existing.git
    ref: main
    path: skills
`))

	if err := EnsureRegistryWithDefaultInConfig(configPath, "git@example.com:existing.git", "main", "skills"); err != nil {
		t.Fatalf("EnsureRegistryWithDefaultInConfig: %v", err)
	}

	cfg, err := loadProjectConfig(t, configPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Registries) != 1 {
		t.Fatalf("expected 1 registry (idempotent), got %d", len(cfg.Registries))
	}
	// Should have set default:true on the existing entry.
	if !cfg.Registries[0].Default {
		t.Fatalf("expected default=true on existing entry, got false")
	}

	// Calling again should be a no-op.
	if err := EnsureRegistryWithDefaultInConfig(configPath, "git@example.com:existing.git", "main", "skills"); err != nil {
		t.Fatalf("EnsureRegistryWithDefaultInConfig (second call): %v", err)
	}
}

func TestSetRegistryExcludeAllInConfigCreatesShadowEntry(t *testing.T) {
	// A local config with no matching entry gets a new shadow entry carrying
	// exclude_all: true. This is how `remove --from <reg> --scope local`
	// suppresses a registry enabled at the project layer.
	configPath := filepath.Join(t.TempDir(), ".agents", "config.local.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte("registries: []\n"))

	if err := SetRegistryExcludeAllInConfig(configPath, "./plugins/database", "", ""); err != nil {
		t.Fatalf("SetRegistryExcludeAllInConfig: %v", err)
	}

	cfg, err := LoadLocalConfigFromPath(configPath)
	if err != nil {
		t.Fatalf("load local config: %v", err)
	}
	if len(cfg.Registries) != 1 {
		t.Fatalf("expected 1 registry, got %d", len(cfg.Registries))
	}
	if got := cfg.Registries[0]; got.URL != "./plugins/database" || !got.ExcludeAll {
		t.Fatalf("expected exclude_all shadow entry, got %#v", got)
	}
}

func TestSetRegistryExcludeAllInConfigUpdatesExistingEntry(t *testing.T) {
	// An existing entry (e.g. a per-resource exclude shadow) gets exclude_all
	// added without losing its other fields, and the call is idempotent.
	configPath := filepath.Join(t.TempDir(), ".agents", "config.local.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`registries:
  - url: ./plugins/database
    skills:
      exclude:
        - db-migrate
`))

	if err := SetRegistryExcludeAllInConfig(configPath, "./plugins/database", "", ""); err != nil {
		t.Fatalf("SetRegistryExcludeAllInConfig: %v", err)
	}

	cfg, err := LoadLocalConfigFromPath(configPath)
	if err != nil {
		t.Fatalf("load local config: %v", err)
	}
	if len(cfg.Registries) != 1 {
		t.Fatalf("expected 1 registry (idempotent on URL), got %d", len(cfg.Registries))
	}
	got := cfg.Registries[0]
	if !got.ExcludeAll {
		t.Fatalf("expected exclude_all=true on existing entry")
	}
	if !slices.Contains(got.Skills.Exclude, "db-migrate") {
		t.Fatalf("expected existing skills.exclude to be preserved, got %#v", got.Skills.Exclude)
	}

	// Second call is a no-op.
	if err := SetRegistryExcludeAllInConfig(configPath, "./plugins/database", "", ""); err != nil {
		t.Fatalf("SetRegistryExcludeAllInConfig (second call): %v", err)
	}
}

// TestAddResourceToRegistryClearsExcludeAll verifies the core reconciliation:
// adding an explicit include to an entry that has exclude_all removes the
// contradictory exclude_all (which would otherwise nullify the include since
// exclude_all always wins in the resolver).
func TestAddResourceToRegistryClearsExcludeAll(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.local.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`registries:
  - url: ./plugins/database
    exclude_all: true
`))

	if err := AddResourceToExactRegistryEntry(configPath, "./plugins/database", "", ir.KindSkill, "db-migrate"); err != nil {
		t.Fatalf("AddResourceToExactRegistryEntry: %v", err)
	}

	cfg, err := LoadLocalConfigFromPath(configPath)
	if err != nil {
		t.Fatalf("load local config: %v", err)
	}
	if len(cfg.Registries) != 1 {
		t.Fatalf("expected 1 registry, got %d", len(cfg.Registries))
	}
	got := cfg.Registries[0]
	if got.ExcludeAll {
		t.Fatalf("expected exclude_all to be cleared after adding an include")
	}
	if !slices.Contains(got.Skills.Include, "db-migrate") {
		t.Fatalf("expected db-migrate in include, got %#v", got.Skills.Include)
	}
}

// TestEnsureRegistryWithDefaultClearsExcludeAll verifies default and exclude_all
// are mutually exclusive: turning on default clears a pre-existing exclude_all.
func TestEnsureRegistryWithDefaultClearsExcludeAll(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.local.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`registries:
  - url: ./plugins/database
    exclude_all: true
`))

	if err := EnsureRegistryWithDefaultInConfig(configPath, "./plugins/database", "", ""); err != nil {
		t.Fatalf("EnsureRegistryWithDefaultInConfig: %v", err)
	}

	cfg, err := LoadLocalConfigFromPath(configPath)
	if err != nil {
		t.Fatalf("load local config: %v", err)
	}
	got := cfg.Registries[0]
	if got.ExcludeAll {
		t.Fatalf("expected exclude_all cleared when default is set")
	}
	if !got.Default {
		t.Fatalf("expected default=true")
	}
}

// TestSetRegistryExcludeAllClearsDefaultAndInclude verifies the reverse: setting
// exclude_all clears the contradictory default flag and per-kind include lists.
func TestSetRegistryExcludeAllClearsDefaultAndInclude(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.local.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`registries:
  - url: ./plugins/database
    default: true
    skills:
      include:
        - db-migrate
`))

	if err := SetRegistryExcludeAllInConfig(configPath, "./plugins/database", "", ""); err != nil {
		t.Fatalf("SetRegistryExcludeAllInConfig: %v", err)
	}

	cfg, err := LoadLocalConfigFromPath(configPath)
	if err != nil {
		t.Fatalf("load local config: %v", err)
	}
	got := cfg.Registries[0]
	if !got.ExcludeAll {
		t.Fatalf("expected exclude_all=true")
	}
	if got.Default {
		t.Fatalf("expected default cleared when exclude_all is set")
	}
	if len(got.Skills.Include) != 0 {
		t.Fatalf("expected skills.include cleared, got %#v", got.Skills.Include)
	}
}

func TestEnsureRegistryWithDefaultInConfigAddsRootWhenSubPathExists(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:existing.git
    ref: main
    path: skills/firewall-rules-management
`))

	// Same URL but root-level — must add a new entry with default:true.
	if err := EnsureRegistryWithDefaultInConfig(configPath, "git@example.com:existing.git", "main", ""); err != nil {
		t.Fatalf("EnsureRegistryWithDefaultInConfig: %v", err)
	}

	cfg, err := loadProjectConfig(t, configPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Registries) != 2 {
		t.Fatalf("expected 2 registries (sub-path + root with default), got %d", len(cfg.Registries))
	}
	if cfg.Registries[0].Path != "skills/firewall-rules-management" {
		t.Fatalf("expected first entry path unchanged, got %q", cfg.Registries[0].Path)
	}
	if cfg.Registries[1].Path != "" {
		t.Fatalf("expected second entry path empty (root), got %q", cfg.Registries[1].Path)
	}
	if !cfg.Registries[1].Default {
		t.Fatalf("expected second entry default=true, got false")
	}
}

func TestSetHarnessesInConfigReplacesHarnessesAndPreservesComments(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	configPath := filepath.Join(homeDir, ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`# user config
apiVersion: agent-marketplace/v2
harnesses:
  - factory # current default
registries:
  - url: git@example.com:infra.git
    ref: main
`))

	if err := SetHarnessesInConfig(configPath, []string{"cursor", "factory", "cursor"}); err != nil {
		t.Fatalf("SetHarnessesInConfig: %v", err)
	}

	cfg, err := loadProjectConfig(t, configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !reflect.DeepEqual(cfg.Harnesses, []string{"cursor", "factory"}) {
		t.Fatalf("expected harnesses to be replaced and deduplicated, got %#v", cfg.Harnesses)
	}

	edited, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read edited config: %v", err)
	}
	if !strings.Contains(string(edited), "# user config") {
		t.Fatalf("expected top-level comment to be preserved, got:\n%s", edited)
	}
}

func TestSetHarnessesInConfigAllowsProjectPath(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoConfigPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")

	if err := SetHarnessesInConfig(repoConfigPath, []string{"factory"}); err != nil {
		t.Fatalf("expected project harnesses to be writable: %v", err)
	}
	cfg, err := loadProjectConfig(t, repoConfigPath)
	if err != nil {
		t.Fatalf("load project config: %v", err)
	}
	if !reflect.DeepEqual(cfg.Harnesses, []string{"factory"}) {
		t.Fatalf("expected project harnesses [factory], got %#v", cfg.Harnesses)
	}
}

func TestSetHarnessesInConfigRejectsEmptyHarnesses(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	configPath := filepath.Join(homeDir, ".agents", "config.yaml")
	if err := SetHarnessesInConfig(configPath, nil); err == nil {
		t.Fatalf("expected empty harnesses to be rejected")
	}
}

// TestSetHarnessesInConfigCanonicalizesAndValidatesTargets pins normalization
// and validation on the mutation that writes a target harness set.
func TestSetHarnessesInConfigCanonicalizesAndValidatesTargets(t *testing.T) {
	t.Run("canonicalizes and dedupes", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		configPath, err := GlobalConfigPath()
		if err != nil {
			t.Fatalf("GlobalConfigPath: %v", err)
		}
		if err := SetHarnessesInConfig(configPath, []string{" cursor ", "factory", "cursor"}); err != nil {
			t.Fatalf("SetHarnessesInConfig: %v", err)
		}
		cfg, err := loadConfig(configPath, false)
		if err != nil {
			t.Fatalf("load config: %v", err)
		}
		if !equalStrings(cfg.Harnesses, []string{"cursor", "factory"}) {
			t.Fatalf("harnesses = %v, want [cursor factory]", cfg.Harnesses)
		}
	})

	t.Run("rejects an unknown target before writing", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		configPath, err := GlobalConfigPath()
		if err != nil {
			t.Fatalf("GlobalConfigPath: %v", err)
		}
		if err := SetHarnessesInConfig(configPath, []string{"devni"}); err == nil {
			t.Fatal("expected unknown harness to be rejected before writing")
		}
		if _, err := os.Stat(configPath); !os.IsNotExist(err) {
			t.Fatalf("invalid harness mutation created %q", configPath)
		}
	})

	t.Run("allows a project config", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		repoPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
		if err := SetHarnessesInConfig(repoPath, []string{"factory"}); err != nil {
			t.Fatalf("expected project harnesses to be writable: %v", err)
		}
		cfg, err := loadProjectConfig(t, repoPath)
		if err != nil {
			t.Fatalf("load project config: %v", err)
		}
		if !equalStrings(cfg.Harnesses, []string{"factory"}) {
			t.Fatalf("harnesses = %v, want [factory]", cfg.Harnesses)
		}
	})
}

func TestReEnableAfterExcludeCleansUpExclude(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra.git
    ref: main
    default: true
    skills:
      exclude:
        - smith
`))

	// Re-enable smith. The entry is default:true (auto-select-all), so this
	// must drop the exclude WITHOUT adding an explicit include — an include
	// would flip the entry out of auto-select-all mode and silently deselect
	// every other skill.
	if err := AddSkillToRegistryInConfig(configPath, "git@example.com:infra.git", "", "smith"); err != nil {
		t.Fatalf("AddSkillToRegistryInConfig: %v", err)
	}

	cfg, err := loadProjectConfig(t, configPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	entry := cfg.Registries[0]
	if !entry.Default {
		t.Fatalf("expected default:true to be preserved")
	}
	if len(entry.Skills.Include) != 0 {
		t.Fatalf("expected NO explicit include on auto-select entry, got %#v", entry.Skills.Include)
	}
	if stringSliceContains(entry.Skills.Exclude, "smith") {
		t.Fatalf("expected smith NOT in exclude after re-enable, got %#v", entry.Skills.Exclude)
	}
}

func TestEnableAfterLocalExcludeCleansUpExclude(t *testing.T) {
	repoRoot := t.TempDir()

	// Add to local exclude first.
	if err := AddToLocalExclude(repoRoot, "smith"); err != nil {
		t.Fatalf("AddToLocalExclude: %v", err)
	}

	// Now enable via local include — should clean up the exclude.
	if err := AddSkillToLocalConfig(repoRoot, "smith"); err != nil {
		t.Fatalf("AddSkillToLocalConfig: %v", err)
	}

	cfg, err := loadLocalConfig(LocalConfigPath(repoRoot))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if !stringSliceContains(cfg.Skills.Include, "smith") {
		t.Fatalf("expected smith in Skills.Include, got %#v", cfg.Skills.Include)
	}
	if stringSliceContains(cfg.Skills.Exclude, "smith") {
		t.Fatalf("expected smith NOT in Skills.Exclude after enable, got %#v", cfg.Skills.Exclude)
	}
}

func TestAddResourcesToConfigCleansMultipleExcludes(t *testing.T) {
	repoRoot := t.TempDir()
	configPath := LocalConfigPath(repoRoot)
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`skills:
  exclude:
    - smith
    - deploy
`))

	if err := AddResourcesToConfig(configPath, ir.KindSkill, []string{"smith", "deploy"}); err != nil {
		t.Fatalf("AddResourcesToConfig: %v", err)
	}

	cfg, err := loadLocalConfig(configPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	for _, name := range []string{"smith", "deploy"} {
		if !stringSliceContains(cfg.Skills.Include, name) {
			t.Fatalf("expected %q in Skills.Include, got %#v", name, cfg.Skills.Include)
		}
		if stringSliceContains(cfg.Skills.Exclude, name) {
			t.Fatalf("expected %q NOT in Skills.Exclude, got %#v", name, cfg.Skills.Exclude)
		}
	}
}

func TestAddResourcesToRegistryInConfigCleansMultipleExcludes(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra.git
    ref: main
    default: true
    skills:
      exclude:
        - smith
        - deploy
`))

	if err := AddResourcesToRegistryInConfig(configPath, "git@example.com:infra.git", "", ir.KindSkill, []string{"smith", "deploy"}); err != nil {
		t.Fatalf("AddResourcesToRegistryInConfig: %v", err)
	}

	cfg, err := loadProjectConfig(t, configPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// The entry is default:true (auto-select-all). Re-adding the previously
	// excluded skills must drop the excludes WITHOUT introducing an explicit
	// include — an include would flip the entry out of auto-select-all mode and
	// silently deselect every other skill in the registry.
	entry := cfg.Registries[0]
	if !entry.Default {
		t.Fatalf("expected default:true to be preserved")
	}
	if len(entry.Skills.Include) != 0 {
		t.Fatalf("expected NO explicit include on auto-select entry, got %#v", entry.Skills.Include)
	}
	for _, name := range []string{"smith", "deploy"} {
		if stringSliceContains(entry.Skills.Exclude, name) {
			t.Fatalf("expected %q NOT in Skills.Exclude, got %#v", name, entry.Skills.Exclude)
		}
	}
}

func TestDisableSkillInRegistryPolicyMatrix(t *testing.T) {
	tests := []struct {
		name          string
		registryYAML  string
		wantInExclude bool
	}{
		{
			name: "default_true_adds_exclude",
			registryYAML: `    default: true
    skills:
      include:
        - smith
`,
			wantInExclude: true,
		},
		{
			name: "no_default_no_tags_no_exclude",
			registryYAML: `    skills:
      include:
        - smith
`,
			wantInExclude: false,
		},
		{
			name: "tags_adds_exclude",
			registryYAML: `    tags:
      - security
    skills:
      include:
        - smith
`,
			wantInExclude: true,
		},
		{
			name: "required_true_adds_exclude",
			registryYAML: `    required: true
    skills:
      include:
        - smith
`,
			wantInExclude: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
			mustMkdirAll(t, filepath.Dir(configPath))
			mustWriteFile(t, configPath, []byte(fmt.Sprintf(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra.git
    ref: main
%s`, tt.registryYAML)))

			if err := DisableSkillInRegistryInConfig(configPath, "git@example.com:infra.git", "", "smith"); err != nil {
				t.Fatalf("DisableSkillInRegistryInConfig: %v", err)
			}

			cfg, err := loadProjectConfig(t, configPath)
			if err != nil {
				t.Fatalf("load: %v", err)
			}

			entry := cfg.Registries[0]
			if stringSliceContains(entry.Skills.Include, "smith") {
				t.Fatalf("expected smith removed from include, got %#v", entry.Skills.Include)
			}
			if got := stringSliceContains(entry.Skills.Exclude, "smith"); got != tt.wantInExclude {
				t.Fatalf("expected smith in exclude=%v, got %v (exclude=%#v)", tt.wantInExclude, got, entry.Skills.Exclude)
			}
		})
	}
}

func TestDisableResourceWithShadowFallbackRegistryInConfig(t *testing.T) {
	for _, defaultValue := range []string{"true", "yes"} {
		t.Run("existing_registry_default_"+defaultValue, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
			mustMkdirAll(t, filepath.Dir(configPath))
			mustWriteFile(t, configPath, []byte(fmt.Sprintf(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra.git
    ref: main
    default: %s
    skills:
      include:
        - smith
`, defaultValue)))
			if err := DisableResourceWithShadowFallback(configPath, "git@example.com:infra.git", "", "main", ir.KindSkill, "smith"); err != nil {
				t.Fatalf("DisableResourceWithShadowFallback: %v", err)
			}
			cfg, err := loadProjectConfig(t, configPath)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if len(cfg.Registries) != 1 {
				t.Fatalf("expected 1 registry, got %d", len(cfg.Registries))
			}
			if stringSliceContains(cfg.Registries[0].Skills.Include, "smith") {
				t.Fatalf("expected smith removed from include")
			}
			if !stringSliceContains(cfg.Registries[0].Skills.Exclude, "smith") {
				t.Fatalf("expected smith in exclude (default:%s triggers exclude)", defaultValue)
			}
		})
	}

	t.Run("missing_registry_creates_shadow_entry", func(t *testing.T) {
		// Simulates the case where the registry is in the org config but
		// the user config does not have it. The function should create a
		// shadow entry with just the exclude.
		configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
		mustMkdirAll(t, filepath.Dir(configPath))
		mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:other.git
    ref: main
    skills:
      include:
        - other-skill
`))
		if err := DisableResourceWithShadowFallback(configPath, "git@example.com:infra.git", "", "v2", ir.KindSkill, "smith"); err != nil {
			t.Fatalf("DisableResourceWithShadowFallback: %v", err)
		}
		cfg, err := loadProjectConfig(t, configPath)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if len(cfg.Registries) != 2 {
			t.Fatalf("expected 2 registries (1 existing + 1 shadow), got %d", len(cfg.Registries))
		}
		// Find the shadow entry
		var shadow *RegistryEntry
		for i := range cfg.Registries {
			if cfg.Registries[i].URL == "git@example.com:infra.git" {
				shadow = &cfg.Registries[i]
				break
			}
		}
		if shadow == nil {
			t.Fatalf("expected shadow entry for infra.git, not found")
		}
		if shadow.Ref != "v2" {
			t.Fatalf("expected shadow ref=v2, got %q", shadow.Ref)
		}
		if !stringSliceContains(shadow.Skills.Exclude, "smith") {
			t.Fatalf("expected smith in shadow exclude, got %#v", shadow.Skills.Exclude)
		}
		if len(shadow.Skills.Include) != 0 {
			t.Fatalf("expected no includes in shadow entry, got %#v", shadow.Skills.Include)
		}
	})

	t.Run("missing_registry_with_path_creates_shadow_with_path", func(t *testing.T) {
		configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
		mustMkdirAll(t, filepath.Dir(configPath))
		mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
`))
		if err := DisableResourceWithShadowFallback(configPath, "git@example.com:infra.git", "skills", "main", ir.KindSkill, "smith"); err != nil {
			t.Fatalf("DisableResourceWithShadowFallback: %v", err)
		}
		cfg, err := loadProjectConfig(t, configPath)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if len(cfg.Registries) != 1 {
			t.Fatalf("expected 1 registry (shadow), got %d", len(cfg.Registries))
		}
		if cfg.Registries[0].Path != "skills" {
			t.Fatalf("expected shadow path=skills, got %q", cfg.Registries[0].Path)
		}
		if !stringSliceContains(cfg.Registries[0].Skills.Exclude, "smith") {
			t.Fatalf("expected smith in shadow exclude")
		}
	})

	t.Run("no_config_file_creates_new_with_shadow", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, ".agents", "config.yaml")
		// File does not exist yet
		if err := DisableResourceWithShadowFallback(configPath, "git@example.com:infra.git", "", "main", ir.KindSkill, "smith"); err != nil {
			t.Fatalf("DisableResourceWithShadowFallback: %v", err)
		}
		// Project-level configs don't require harnesses; they inherit from
		// the org/global layer during merge.
		cfg, err := loadProjectConfig(t, configPath)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if cfg.APIVersion != APIVersionV2 {
			t.Fatalf("expected apiVersion %q, got %q", APIVersionV2, cfg.APIVersion)
		}
		if len(cfg.Registries) != 1 {
			t.Fatalf("expected 1 registry (shadow), got %d", len(cfg.Registries))
		}
		if !stringSliceContains(cfg.Registries[0].Skills.Exclude, "smith") {
			t.Fatalf("expected smith in shadow exclude")
		}
	})

	t.Run("idempotent_shadow_entry", func(t *testing.T) {
		configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
		mustMkdirAll(t, filepath.Dir(configPath))
		mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
`))
		// Call twice — should not duplicate the entry or the exclude.
		if err := DisableResourceWithShadowFallback(configPath, "git@example.com:infra.git", "", "main", ir.KindSkill, "smith"); err != nil {
			t.Fatalf("first call: %v", err)
		}
		if err := DisableResourceWithShadowFallback(configPath, "git@example.com:infra.git", "", "main", ir.KindSkill, "smith"); err != nil {
			t.Fatalf("second call: %v", err)
		}
		cfg, err := loadProjectConfig(t, configPath)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if len(cfg.Registries) != 1 {
			t.Fatalf("expected 1 registry (shadow not duplicated), got %d", len(cfg.Registries))
		}
		smithCount := 0
		for _, name := range cfg.Registries[0].Skills.Exclude {
			if name == "smith" {
				smithCount++
			}
		}
		if smithCount != 1 {
			t.Fatalf("expected smith to appear once in exclude, got %d times", smithCount)
		}
	})

	t.Run("normalized_url_match_adds_exclude_to_existing_entry", func(t *testing.T) {
		// Simulates the case where the org config uses "infra.git" but the
		// user config already has "infra" (without .git). The function should
		// add the exclude to the existing entry rather than creating a new
		// shadow entry with a different URL suffix.
		configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
		mustMkdirAll(t, filepath.Dir(configPath))
		mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra
    ref: main
    skills:
      include:
        - brainstorming
`))
		if err := DisableResourceWithShadowFallback(configPath, "git@example.com:infra.git", "", "main", ir.KindSkill, "smith"); err != nil {
			t.Fatalf("DisableResourceWithShadowFallback: %v", err)
		}
		cfg, err := loadProjectConfig(t, configPath)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		// Should NOT create a second registry entry — reuse the existing one.
		if len(cfg.Registries) != 1 {
			t.Fatalf("expected 1 registry (reused existing), got %d", len(cfg.Registries))
		}
		if cfg.Registries[0].URL != "git@example.com:infra" {
			t.Fatalf("expected URL preserved as git@example.com:infra, got %q", cfg.Registries[0].URL)
		}
		if !stringSliceContains(cfg.Registries[0].Skills.Include, "brainstorming") {
			t.Fatalf("expected brainstorming still in include")
		}
		if !stringSliceContains(cfg.Registries[0].Skills.Exclude, "smith") {
			t.Fatalf("expected smith added to exclude of existing entry")
		}
	})

	t.Run("normalized_url_match_with_path_prefers_matching_path", func(t *testing.T) {
		// When the user config has two entries with the same normalized URL
		// but different paths, the function should add the exclude to the
		// entry whose path matches the resolved artifact's path.
		configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
		mustMkdirAll(t, filepath.Dir(configPath))
		mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra
    ref: main
    skills:
      include:
        - brainstorming
  - url: git@example.com:infra
    ref: main
    path: .factory
    skills:
      include:
        - openspec-apply-change
`))
		// The resolved artifact comes from the root (no path). The function
		// should add the exclude to the root entry, not the .factory entry.
		if err := DisableResourceWithShadowFallback(configPath, "git@example.com:infra.git", "", "main", ir.KindSkill, "smith"); err != nil {
			t.Fatalf("DisableResourceWithShadowFallback: %v", err)
		}
		cfg, err := loadProjectConfig(t, configPath)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if len(cfg.Registries) != 2 {
			t.Fatalf("expected 2 registries (unchanged), got %d", len(cfg.Registries))
		}
		// Root entry should have the exclude
		var rootEntry, factoryEntry *RegistryEntry
		for i := range cfg.Registries {
			if cfg.Registries[i].Path == "" {
				rootEntry = &cfg.Registries[i]
			} else {
				factoryEntry = &cfg.Registries[i]
			}
		}
		if rootEntry == nil || !stringSliceContains(rootEntry.Skills.Exclude, "smith") {
			t.Fatalf("expected smith in root entry's exclude")
		}
		if factoryEntry != nil && stringSliceContains(factoryEntry.Skills.Exclude, "smith") {
			t.Fatalf("smith should NOT be in .factory entry's exclude")
		}
	})

	t.Run("normalized_url_no_matching_path_creates_shadow", func(t *testing.T) {
		// When the user config has an entry for the same normalized URL but
		// with a DIFFERENT path, and the resolved artifact has a non-empty
		// path that doesn't match, a shadow entry should be created.
		configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
		mustMkdirAll(t, filepath.Dir(configPath))
		mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra
    ref: main
    path: .factory
    skills:
      include:
        - brainstorming
`))
		// Resolved artifact comes from "skills" path, which doesn't match .factory
		if err := DisableResourceWithShadowFallback(configPath, "git@example.com:infra.git", "skills", "main", ir.KindSkill, "smith"); err != nil {
			t.Fatalf("DisableResourceWithShadowFallback: %v", err)
		}
		cfg, err := loadProjectConfig(t, configPath)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if len(cfg.Registries) != 2 {
			t.Fatalf("expected 2 registries (1 existing + 1 shadow), got %d", len(cfg.Registries))
		}
		// Find the shadow entry (with path "skills")
		var shadow *RegistryEntry
		for i := range cfg.Registries {
			if cfg.Registries[i].Path == "skills" {
				shadow = &cfg.Registries[i]
			}
		}
		if shadow == nil {
			t.Fatalf("expected shadow entry with path=skills")
		}
		if !stringSliceContains(shadow.Skills.Exclude, "smith") {
			t.Fatalf("expected smith in shadow exclude")
		}
	})
}

func TestDisableResourceWithShadowFallbackTagSelectedEntry(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:shared.git
    ref: main
    tags: [security]
  - url: git@example.com:shared.git
    ref: main
`))

	if err := DisableResourceWithShadowFallback(configPath, "git@example.com:shared.git", "", "main", ir.KindSkill, "secret-scanning"); err != nil {
		t.Fatalf("DisableResourceWithShadowFallback: %v", err)
	}

	cfg, err := loadProjectConfig(t, configPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Registries) != 2 {
		t.Fatalf("expected 2 registries, got %d", len(cfg.Registries))
	}
	if !stringSliceContains(cfg.Registries[0].Skills.Exclude, "secret-scanning") {
		t.Fatalf("expected tag-selected entry to receive exclude, got %#v", cfg.Registries[0].Skills.Exclude)
	}
	if stringSliceContains(cfg.Registries[1].Skills.Exclude, "secret-scanning") {
		t.Fatalf("unexpected exclude on unrelated entry: %#v", cfg.Registries[1].Skills.Exclude)
	}
}

// TestDisableResourcePrefixPathAmbiguity verifies that when a config has two
// entries for the same URL — one with path "plugins" and one with
// "plugins/personal" — disabling a resource whose RegistryPath is
// "plugins/personal" targets only the "plugins/personal" entry, not the
// parent "plugins" entry.
// This covers both the include-removal path (resource in include list) and the
// exclude-addition path (resource auto-selected via default:true).
func TestDisableResourcePrefixPathAmbiguity(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	const (
		url          = "git@example.com:org/registry"
		parentPath   = "plugins"
		specificPath = "plugins/personal"
		resName      = "my-droid"
	)
	initial := `apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:org/registry
    ref: main
    path: plugins
    subagents:
      include: [other-droid]
  - url: git@example.com:org/registry
    ref: main
    path: plugins/personal
    subagents:
      include: [my-droid]
`
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(initial), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := DisableResourceWithShadowFallback(configPath, url, specificPath, "main", ir.KindSubagent, resName); err != nil {
		t.Fatalf("DisableResourceWithShadowFallback: %v", err)
	}

	cfg, err := LoadConfigFromPath(configPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}

	// The "plugins/personal" entry should have my-droid in its exclude list.
	var personalEntry, parentEntry *RegistryEntry
	for i := range cfg.Registries {
		e := &cfg.Registries[i]
		if strings.TrimSpace(e.Path) == specificPath {
			personalEntry = e
		}
		if strings.TrimSpace(e.Path) == parentPath {
			parentEntry = e
		}
	}
	if personalEntry == nil {
		t.Fatal("plugins/personal entry not found after edit")
	}
	if parentEntry == nil {
		t.Fatal("plugins entry not found after edit")
	}
	// The resource should no longer be in the plugins/personal include list.
	sel := personalEntry.SelectorForKind(ir.KindSubagent)
	if slices.Contains(sel.Include, resName) {
		t.Errorf("expected %q removed from plugins/personal include list, still present", resName)
	}
	// The plugins (parent) entry must be completely untouched.
	parentSel := parentEntry.SelectorForKind(ir.KindSubagent)
	if slices.Contains(parentSel.Exclude, resName) {
		t.Errorf("expected %q NOT written to plugins exclude list, but it was added there", resName)
	}
	if slices.Contains(parentSel.Include, resName) {
		t.Errorf("expected plugins include to be unchanged, but %q appeared there", resName)
	}
}

// TestDisableResourcePrefixPathAmbiguityDefaultEntry verifies that when an
// entry has default:true and the resource is NOT in its include list,
// adding an exclude to registryPath="plugins/personal" does not accidentally
// match the parent "plugins" entry via prefix semantics.
func TestDisableResourcePrefixPathAmbiguityDefaultEntry(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	const (
		url          = "git@example.com:org/registry"
		parentPath   = "plugins"
		specificPath = "plugins/personal"
		resName      = "my-droid"
	)
	initial := `apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:org/registry
    ref: main
    path: plugins
    subagents:
      include: [other-droid]
  - url: git@example.com:org/registry
    ref: main
    path: plugins/personal
    default: true
`
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(initial), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := DisableResourceWithShadowFallback(configPath, url, specificPath, "main", ir.KindSubagent, resName); err != nil {
		t.Fatalf("DisableResourceWithShadowFallback: %v", err)
	}

	cfg, err := LoadConfigFromPath(configPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}

	var personalEntry, parentEntry *RegistryEntry
	for i := range cfg.Registries {
		e := &cfg.Registries[i]
		if strings.TrimSpace(e.Path) == specificPath {
			personalEntry = e
		}
		if strings.TrimSpace(e.Path) == parentPath {
			parentEntry = e
		}
	}
	if personalEntry == nil {
		t.Fatal("plugins/personal entry not found after edit")
	}
	if parentEntry == nil {
		t.Fatal("plugins entry not found after edit")
	}
	// The exclude must be written to plugins/personal, not to the parent entry.
	personalSel := personalEntry.SelectorForKind(ir.KindSubagent)
	if !slices.Contains(personalSel.Exclude, resName) {
		t.Errorf("expected %q in plugins/personal exclude list, got %v", resName, personalSel.Exclude)
	}
	parentSel := parentEntry.SelectorForKind(ir.KindSubagent)
	if slices.Contains(parentSel.Exclude, resName) {
		t.Errorf("expected %q NOT in plugins exclude list, but it appeared there", resName)
	}
}

func TestAddResourceToExactRegistryEntry(t *testing.T) {
	t.Run("exact_path_matches_root_entry", func(t *testing.T) {
		configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
		mustMkdirAll(t, filepath.Dir(configPath))
		mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra
    ref: main
    skills:
      include:
        - brainstorming
  - url: git@example.com:infra
    ref: main
    path: .factory
    skills:
      include:
        - openspec-apply-change
`))
		if err := AddResourceToExactRegistryEntry(configPath, "git@example.com:infra", "", ir.KindSkill, "smith"); err != nil {
			t.Fatalf("AddResourceToExactRegistryEntry: %v", err)
		}
		cfg, err := loadProjectConfig(t, configPath)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if len(cfg.Registries) != 2 {
			t.Fatalf("expected 2 registries, got %d", len(cfg.Registries))
		}
		var rootEntry, factoryEntry *RegistryEntry
		for i := range cfg.Registries {
			if cfg.Registries[i].Path == "" {
				rootEntry = &cfg.Registries[i]
			} else {
				factoryEntry = &cfg.Registries[i]
			}
		}
		if rootEntry == nil || !stringSliceContains(rootEntry.Skills.Include, "smith") {
			t.Fatalf("expected smith in root entry's include")
		}
		if factoryEntry != nil && stringSliceContains(factoryEntry.Skills.Include, "smith") {
			t.Fatalf("smith should NOT be in .factory entry's include")
		}
	})

	t.Run("exact_path_matches_subdir_entry", func(t *testing.T) {
		configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
		mustMkdirAll(t, filepath.Dir(configPath))
		mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra
    ref: main
    skills:
      include:
        - brainstorming
  - url: git@example.com:infra
    ref: main
    path: .factory
    skills:
      include:
        - openspec-apply-change
`))
		if err := AddResourceToExactRegistryEntry(configPath, "git@example.com:infra", ".factory", ir.KindSkill, "smith"); err != nil {
			t.Fatalf("AddResourceToExactRegistryEntry: %v", err)
		}
		cfg, err := loadProjectConfig(t, configPath)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		var factoryEntry *RegistryEntry
		for i := range cfg.Registries {
			if cfg.Registries[i].Path == ".factory" {
				factoryEntry = &cfg.Registries[i]
			}
		}
		if factoryEntry == nil || !stringSliceContains(factoryEntry.Skills.Include, "smith") {
			t.Fatalf("expected smith in .factory entry's include")
		}
	})

	t.Run("cleans_up_exclude_from_same_entry", func(t *testing.T) {
		configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
		mustMkdirAll(t, filepath.Dir(configPath))
		mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra
    ref: main
    skills:
      include:
        - brainstorming
      exclude:
        - smith
`))
		if err := AddResourceToExactRegistryEntry(configPath, "git@example.com:infra", "", ir.KindSkill, "smith"); err != nil {
			t.Fatalf("AddResourceToExactRegistryEntry: %v", err)
		}
		cfg, err := loadProjectConfig(t, configPath)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if !stringSliceContains(cfg.Registries[0].Skills.Include, "smith") {
			t.Fatalf("expected smith in include")
		}
		if stringSliceContains(cfg.Registries[0].Skills.Exclude, "smith") {
			t.Fatalf("expected smith removed from exclude after re-include")
		}
	})

	// Regression: when the same URL has both a parent ("plugins") and a child
	// ("plugins/personal") entry, the parent's path is a prefix of the child's.
	// A first-match-on-prefix scan would write to the parent even though the
	// caller asked for the exact child path. The parent is listed FIRST so the
	// prefix match would win without preferExactPathMatch.
	t.Run("prefers_exact_child_path_over_parent_prefix", func(t *testing.T) {
		configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
		mustMkdirAll(t, filepath.Dir(configPath))
		mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra
    ref: main
    path: plugins
    skills:
      include:
        - brainstorming
  - url: git@example.com:infra
    ref: main
    path: plugins/personal
    skills:
      include:
        - openspec-apply-change
`))
		if err := AddResourceToExactRegistryEntry(configPath, "git@example.com:infra", "plugins/personal", ir.KindSkill, "smith"); err != nil {
			t.Fatalf("AddResourceToExactRegistryEntry: %v", err)
		}
		cfg, err := loadProjectConfig(t, configPath)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		var parentEntry, childEntry *RegistryEntry
		for i := range cfg.Registries {
			switch cfg.Registries[i].Path {
			case "plugins":
				parentEntry = &cfg.Registries[i]
			case "plugins/personal":
				childEntry = &cfg.Registries[i]
			}
		}
		if childEntry == nil || !stringSliceContains(childEntry.Skills.Include, "smith") {
			t.Fatalf("expected smith in exact child entry (plugins/personal), got %#v", childEntry)
		}
		if parentEntry != nil && stringSliceContains(parentEntry.Skills.Include, "smith") {
			t.Fatalf("smith must NOT be written to the parent prefix entry (plugins), got %#v", parentEntry)
		}
	})

	// Symmetric removal guard: removeValuesFromExactRegistryEntry got the same
	// exact-path preference, so removal targeting the child path must not
	// silently strip the value from the parent prefix entry.
	t.Run("remove_prefers_exact_child_path_over_parent_prefix", func(t *testing.T) {
		configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
		mustMkdirAll(t, filepath.Dir(configPath))
		mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra
    ref: main
    path: plugins
    skills:
      include:
        - smith
  - url: git@example.com:infra
    ref: main
    path: plugins/personal
    skills:
      include:
        - smith
`))
		if err := removeValuesFromExactRegistryEntry(configPath, "git@example.com:infra", "plugins/personal", ir.KindSkill, "include", []string{"smith"}); err != nil {
			t.Fatalf("removeValuesFromExactRegistryEntry: %v", err)
		}
		cfg, err := loadProjectConfig(t, configPath)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		var parentEntry, childEntry *RegistryEntry
		for i := range cfg.Registries {
			switch cfg.Registries[i].Path {
			case "plugins":
				parentEntry = &cfg.Registries[i]
			case "plugins/personal":
				childEntry = &cfg.Registries[i]
			}
		}
		if parentEntry == nil || !stringSliceContains(parentEntry.Skills.Include, "smith") {
			t.Fatalf("smith must remain in the parent prefix entry (plugins), got %#v", parentEntry)
		}
		if childEntry != nil && stringSliceContains(childEntry.Skills.Include, "smith") {
			t.Fatalf("expected smith removed from exact child entry (plugins/personal), got %#v", childEntry)
		}
	})

	// Regression (auto-select entries): re-installing a resource that was only
	// excluded on a default:true entry must just drop the exclude, NOT add an
	// explicit include — an include would flip the entry out of auto-select-all
	// mode and silently deselect every other resource.
	t.Run("default_entry_reinstall_only_drops_exclude", func(t *testing.T) {
		configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
		mustMkdirAll(t, filepath.Dir(configPath))
		mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra
    ref: main
    default: true
    skills:
      exclude:
        - cohort-analysis
`))
		if err := AddResourceToExactRegistryEntry(configPath, "git@example.com:infra", "", ir.KindSkill, "cohort-analysis"); err != nil {
			t.Fatalf("AddResourceToExactRegistryEntry: %v", err)
		}
		cfg, err := loadProjectConfig(t, configPath)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		entry := cfg.Registries[0]
		if !entry.Default {
			t.Fatalf("expected default:true to be preserved")
		}
		if len(entry.Skills.Include) != 0 {
			t.Fatalf("expected NO explicit include on auto-select entry, got %#v", entry.Skills.Include)
		}
		if stringSliceContains(entry.Skills.Exclude, "cohort-analysis") {
			t.Fatalf("expected cohort-analysis removed from exclude, got %#v", entry.Skills.Exclude)
		}
	})

	// A default:true entry with no exclude already selects the resource, so a
	// re-install is a no-op that must not introduce an include.
	t.Run("default_entry_reinstall_without_exclude_is_noop", func(t *testing.T) {
		configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
		mustMkdirAll(t, filepath.Dir(configPath))
		mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra
    ref: main
    default: true
`))
		if err := AddResourceToExactRegistryEntry(configPath, "git@example.com:infra", "", ir.KindSkill, "cohort-analysis"); err != nil {
			t.Fatalf("AddResourceToExactRegistryEntry: %v", err)
		}
		cfg, err := loadProjectConfig(t, configPath)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		entry := cfg.Registries[0]
		if !entry.Default {
			t.Fatalf("expected default:true to be preserved")
		}
		if len(entry.Skills.Include) != 0 {
			t.Fatalf("expected NO explicit include on auto-select entry, got %#v", entry.Skills.Include)
		}
	})

	// required:true entries also auto-select everything, so the same rule holds.
	t.Run("required_entry_reinstall_only_drops_exclude", func(t *testing.T) {
		configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
		mustMkdirAll(t, filepath.Dir(configPath))
		mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra
    ref: main
    required: true
    skills:
      exclude:
        - cohort-analysis
`))
		if err := AddResourceToExactRegistryEntry(configPath, "git@example.com:infra", "", ir.KindSkill, "cohort-analysis"); err != nil {
			t.Fatalf("AddResourceToExactRegistryEntry: %v", err)
		}
		cfg, err := loadProjectConfig(t, configPath)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		entry := cfg.Registries[0]
		if !entry.Required {
			t.Fatalf("expected required:true to be preserved")
		}
		if len(entry.Skills.Include) != 0 {
			t.Fatalf("expected NO explicit include on auto-select entry, got %#v", entry.Skills.Include)
		}
	})

	// Guard: a default:true entry that ALREADY has an explicit include is in
	// explicit-selection mode (auto-select-all is off), so a new install must
	// still be added to the include list.
	t.Run("default_entry_with_existing_include_adds_normally", func(t *testing.T) {
		configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
		mustMkdirAll(t, filepath.Dir(configPath))
		mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra
    ref: main
    default: true
    skills:
      include:
        - brainstorming
`))
		if err := AddResourceToExactRegistryEntry(configPath, "git@example.com:infra", "", ir.KindSkill, "smith"); err != nil {
			t.Fatalf("AddResourceToExactRegistryEntry: %v", err)
		}
		cfg, err := loadProjectConfig(t, configPath)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if !stringSliceContains(cfg.Registries[0].Skills.Include, "smith") {
			t.Fatalf("expected smith added to existing include list, got %#v", cfg.Registries[0].Skills.Include)
		}
	})

	// Guard: a default:true entry with tags selects by tag (auto-select-all is
	// off), so an explicit include is still required to add a resource.
	t.Run("default_entry_with_tags_adds_include", func(t *testing.T) {
		configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
		mustMkdirAll(t, filepath.Dir(configPath))
		mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra
    ref: main
    default: true
    tags:
      - k8s
`))
		if err := AddResourceToExactRegistryEntry(configPath, "git@example.com:infra", "", ir.KindSkill, "smith"); err != nil {
			t.Fatalf("AddResourceToExactRegistryEntry: %v", err)
		}
		cfg, err := loadProjectConfig(t, configPath)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if !stringSliceContains(cfg.Registries[0].Skills.Include, "smith") {
			t.Fatalf("expected smith added to include on tag-based entry, got %#v", cfg.Registries[0].Skills.Include)
		}
	})

	// Guard: a default:true entry with skills.exclude_all deselects every
	// skill, so it does NOT auto-select the kind. Re-installing must add an
	// explicit include AND clear the contradictory exclude_all rather than
	// no-op — otherwise the skill would stay excluded.
	t.Run("default_entry_with_kind_exclude_all_reenables", func(t *testing.T) {
		configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
		mustMkdirAll(t, filepath.Dir(configPath))
		mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra
    ref: main
    default: true
    skills:
      exclude_all: true
`))
		if err := AddResourceToExactRegistryEntry(configPath, "git@example.com:infra", "", ir.KindSkill, "smith"); err != nil {
			t.Fatalf("AddResourceToExactRegistryEntry: %v", err)
		}
		cfg, err := loadProjectConfig(t, configPath)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		entry := cfg.Registries[0]
		if !entry.Default {
			t.Fatalf("expected default:true to be preserved")
		}
		if entry.Skills.ExcludeAll {
			t.Fatalf("expected skills.exclude_all cleared so the reinstall is not a no-op")
		}
		if !stringSliceContains(entry.Skills.Include, "smith") {
			t.Fatalf("expected smith re-enabled via include, got %#v", entry.Skills.Include)
		}
	})

	// Guard (kind-aware): a default:true entry with mcp.exclude_all still
	// auto-selects skills, so re-installing a SKILL must stay a no-op (no
	// include) and must not disturb the unrelated mcp.exclude_all.
	t.Run("default_entry_other_kind_exclude_all_skill_is_noop", func(t *testing.T) {
		configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
		mustMkdirAll(t, filepath.Dir(configPath))
		mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra
    ref: main
    default: true
    mcp:
      exclude_all: true
`))
		if err := AddResourceToExactRegistryEntry(configPath, "git@example.com:infra", "", ir.KindSkill, "smith"); err != nil {
			t.Fatalf("AddResourceToExactRegistryEntry: %v", err)
		}
		cfg, err := loadProjectConfig(t, configPath)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		entry := cfg.Registries[0]
		if len(entry.Skills.Include) != 0 {
			t.Fatalf("expected NO skills.include (skill auto-selected), got %#v", entry.Skills.Include)
		}
		if !entry.MCP.ExcludeAll {
			t.Fatalf("expected unrelated mcp.exclude_all to be preserved")
		}
	})

	// Guard: YAML 1.1 boolean spellings (e.g. `yes`) must be treated as
	// default:true, matching how the loader decodes RegistryEntry.Default.
	// Otherwise the reinstall would wrongly add an include.
	t.Run("default_yaml_bool_spelling_respected", func(t *testing.T) {
		configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
		mustMkdirAll(t, filepath.Dir(configPath))
		mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra
    ref: main
    default: yes
    skills:
      exclude:
        - cohort-analysis
`))
		if err := AddResourceToExactRegistryEntry(configPath, "git@example.com:infra", "", ir.KindSkill, "cohort-analysis"); err != nil {
			t.Fatalf("AddResourceToExactRegistryEntry: %v", err)
		}
		cfg, err := loadProjectConfig(t, configPath)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		entry := cfg.Registries[0]
		if !entry.Default {
			t.Fatalf("expected default: yes to load as default:true")
		}
		if len(entry.Skills.Include) != 0 {
			t.Fatalf("expected NO explicit include (default: yes auto-selects), got %#v", entry.Skills.Include)
		}
		if stringSliceContains(entry.Skills.Exclude, "cohort-analysis") {
			t.Fatalf("expected cohort-analysis removed from exclude, got %#v", entry.Skills.Exclude)
		}
	})
}

func TestDisableResourceWithShadowFallbackTargetsRefAndAllExactDuplicates(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra
    ref: main
    default: true
    skills:
      include: [gone]
  - url: git@example.com:infra
    ref: release
    default: true
    skills: [gone] # keep release shorthand
  - url: git@example.com:infra
    ref: main
    default: true
    skills:
      include: [gone]
`))

	if err := DisableResourceWithShadowFallback(configPath, "git@example.com:infra", "", "main", ir.KindSkill, "gone"); err != nil {
		t.Fatalf("DisableResourceWithShadowFallback: %v", err)
	}
	cfg, err := loadProjectConfig(t, configPath)
	if err != nil {
		t.Fatal(err)
	}
	for i, entry := range cfg.Registries {
		selected := slices.Contains(entry.Skills.Include, "gone")
		excluded := slices.Contains(entry.Skills.Exclude, "gone")
		if entry.Ref == "main" && (selected || !excluded) {
			t.Fatalf("main duplicate %d was not disabled: %#v", i, entry.Skills)
		}
		if entry.Ref == "release" && (!selected || excluded) {
			t.Fatalf("different ref was mutated: %#v", entry.Skills)
		}
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("skills: [gone] # keep release shorthand")) {
		t.Fatalf("untouched ref lost its comment or shorthand selector:\n%s", data)
	}
}

func TestRemoveRegistryEntryFromConfig(t *testing.T) {
	t.Run("removes_matching_entry_by_url_and_path", func(t *testing.T) {
		configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
		mustMkdirAll(t, filepath.Dir(configPath))
		mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra
    ref: main
    skills:
      include:
        - brainstorming
  - url: git@example.com:security
    ref: main
    skills:
      include:
        - secret-scanning
`))
		if err := RemoveRegistryEntryFromConfig(configPath, "git@example.com:security", ""); err != nil {
			t.Fatalf("RemoveRegistryEntryFromConfig: %v", err)
		}
		cfg, err := loadProjectConfig(t, configPath)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if len(cfg.Registries) != 1 {
			t.Fatalf("expected 1 registry, got %d", len(cfg.Registries))
		}
		if cfg.Registries[0].URL != "git@example.com:infra" {
			t.Fatalf("expected infra registry to remain, got %q", cfg.Registries[0].URL)
		}
	})

	t.Run("removes_only_entry_with_matching_path", func(t *testing.T) {
		configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
		mustMkdirAll(t, filepath.Dir(configPath))
		mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra
    ref: main
  - url: git@example.com:infra
    ref: main
    path: .factory
`))
		if err := RemoveRegistryEntryFromConfig(configPath, "git@example.com:infra", ".factory"); err != nil {
			t.Fatalf("RemoveRegistryEntryFromConfig: %v", err)
		}
		cfg, err := loadProjectConfig(t, configPath)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if len(cfg.Registries) != 1 {
			t.Fatalf("expected 1 registry, got %d", len(cfg.Registries))
		}
		if cfg.Registries[0].Path != "" {
			t.Fatalf("expected root entry to remain, got path=%q", cfg.Registries[0].Path)
		}
	})

	t.Run("idempotent_when_entry_not_found", func(t *testing.T) {
		configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
		mustMkdirAll(t, filepath.Dir(configPath))
		mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra
    ref: main
`))
		if err := RemoveRegistryEntryFromConfig(configPath, "git@example.com:other", ""); err != nil {
			t.Fatalf("RemoveRegistryEntryFromConfig should be idempotent: %v", err)
		}
		cfg, err := loadProjectConfig(t, configPath)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if len(cfg.Registries) != 1 {
			t.Fatalf("expected 1 registry unchanged, got %d", len(cfg.Registries))
		}
	})

	t.Run("removes_registries_key_when_last_entry_removed", func(t *testing.T) {
		configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
		mustMkdirAll(t, filepath.Dir(configPath))
		mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra
    ref: main
`))
		if err := RemoveRegistryEntryFromConfig(configPath, "git@example.com:infra", ""); err != nil {
			t.Fatalf("RemoveRegistryEntryFromConfig: %v", err)
		}
		cfg, err := loadProjectConfig(t, configPath)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if len(cfg.Registries) != 0 {
			t.Fatalf("expected 0 registries, got %d", len(cfg.Registries))
		}
	})
}

func TestEnsureGlobalConfigCreatesSkeletonWithDefaults(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	globalPath := filepath.Join(homeDir, ".agents", "config.yaml")
	if err := EnsureGlobalConfig(); err != nil {
		t.Fatalf("EnsureGlobalConfig: %v", err)
	}

	if _, err := os.Stat(globalPath); err != nil {
		t.Fatalf("expected global config to be created: %v", err)
	}

	data, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatalf("read global config: %v", err)
	}
	content := string(data)
	for _, needle := range []string{
		"apiVersion: agent-marketplace/v2",
		"harnesses:",
	} {
		if !strings.Contains(content, needle) {
			t.Fatalf("expected skeleton to contain %q, got:\n%s", needle, content)
		}
	}
	// Logging is intentionally omitted from the skeleton; the runtime falls
	// back to code defaults unless the user opts in.
	if strings.Contains(content, "logging:") {
		t.Fatalf("skeleton must not contain a logging block, got:\n%s", content)
	}

	// Second call should be a no-op and not error.
	if err := EnsureGlobalConfig(); err != nil {
		t.Fatalf("EnsureGlobalConfig second call: %v", err)
	}

	info, err := os.Stat(globalPath)
	if err != nil {
		t.Fatalf("stat global config after second call: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("expected global config to remain non-empty")
	}
}

func TestReadOrInitConfigSkeletonOmitsHarnesses(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".agents", "config.yaml")

	doc, mode, err := readOrInitConfig(configPath)
	if err != nil {
		t.Fatalf("readOrInitConfig: %v", err)
	}
	if mode != defaultConfigMode {
		t.Fatalf("expected default mode, got %v", mode)
	}

	// Verify the skeleton does NOT contain a harnesses or logging key for project configs.
	root := doc.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "harnesses" {
			t.Fatalf("project skeleton config must not contain harnesses key; project configs inherit from org/global layer")
		}
		if root.Content[i].Value == "logging" {
			t.Fatalf("project skeleton config must not contain logging key; logging is user-level only")
		}
	}

	// Verify apiVersion is present.
	found := false
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "apiVersion" && root.Content[i+1].Value == APIVersionV2 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("skeleton config must contain apiVersion %q", APIVersionV2)
	}
}

func TestReadOrInitConfigGlobalSkeletonIncludesHarnesses(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	globalPath := filepath.Join(homeDir, ".agents", "config.yaml")
	doc, mode, err := readOrInitConfig(globalPath)
	if err != nil {
		t.Fatalf("readOrInitConfig: %v", err)
	}
	if mode != defaultConfigMode {
		t.Fatalf("expected default mode, got %v", mode)
	}

	// Verify the skeleton DOES contain a harnesses key for global configs.
	root := doc.Content[0]
	harnessesNode := findMappingValue(root, "harnesses")
	if harnessesNode == nil {
		t.Fatalf("global skeleton config must contain harnesses key; global configs require harnesses")
	}
	if harnessesNode.Kind != yaml.SequenceNode {
		t.Fatalf("expected harnesses to be a sequence, got kind %d", harnessesNode.Kind)
	}
	values := sequenceValues(harnessesNode)
	expected := DefaultHarnesses()
	if len(values) != len(expected) {
		t.Fatalf("expected global skeleton harnesses %v (from org config), got %v", expected, values)
	}
	for i := range values {
		if values[i] != expected[i] {
			t.Fatalf("expected global skeleton harnesses %v (from org config), got %v", expected, values)
		}
	}

	// The skeleton must NOT include a logging block; the logging runtime uses
	// code defaults unless the user explicitly opts in.
	if loggingNode := findMappingValue(root, "logging"); loggingNode != nil {
		t.Fatalf("global skeleton config must not contain a logging key")
	}
}

func TestFindRegistryURLNodeEmptyURLFallsBackToPath(t *testing.T) {
	tests := []struct {
		name      string
		entryYAML string
		wantKey   string // key whose value node should be returned; "" means expect nil
		wantValue string
	}{
		{
			name: "url_present",
			entryYAML: `url: git@example.com:infra.git
path: skills
`,
			wantKey:   "url",
			wantValue: "git@example.com:infra.git",
		},
		{
			name: "path_alias_when_url_absent",
			entryYAML: `path: /local/skills
`,
			wantKey:   "path",
			wantValue: "/local/skills",
		},
		{
			name: "path_alias_when_url_empty",
			entryYAML: `url: ""
path: /local/skills
`,
			wantKey:   "path",
			wantValue: "/local/skills",
		},
		{
			name: "path_alias_when_url_whitespace",
			entryYAML: `url: "   "
path: /local/skills
`,
			wantKey:   "path",
			wantValue: "/local/skills",
		},
		{
			name: "nil_when_neither_present",
			entryYAML: `ref: main
`,
			wantKey: "",
		},
		{
			name: "nil_when_url_empty_and_no_path",
			entryYAML: `url: ""
ref: main
`,
			wantKey: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var doc yaml.Node
			if err := yaml.Unmarshal([]byte(tt.entryYAML), &doc); err != nil {
				t.Fatalf("unmarshal entry: %v", err)
			}
			entryNode := doc.Content[0]

			got := findRegistryURLNode(entryNode)
			if tt.wantKey == "" {
				if got != nil {
					t.Fatalf("expected nil node, got value %q", got.Value)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected %q node with value %q, got nil", tt.wantKey, tt.wantValue)
			}
			if got.Value != tt.wantValue {
				t.Fatalf("expected node value %q, got %q", tt.wantValue, got.Value)
			}
			if want := findMappingValue(entryNode, tt.wantKey); want != got {
				t.Fatalf("expected returned node to be the %q value node", tt.wantKey)
			}
		})
	}
}

func TestEnsureRegistryInConfigMatchesEntryWithEmptyURLAndPathAlias(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v2
registries:
  - url: ""
    path: /local/skills
`))

	// The existing entry stores its location in "path" because "url" is
	// empty: RegistryEntry.UnmarshalYAML treats url: "" as absent and uses
	// "path" as the location alias. The editor must match that entry instead
	// of appending a duplicate.
	if err := EnsureRegistryInConfig(configPath, "/local/skills", ""); err != nil {
		t.Fatalf("expected EnsureRegistryInConfig to succeed: %v", err)
	}

	cfg, err := loadProjectConfig(t, configPath)
	if err != nil {
		t.Fatalf("expected edited config to load: %v", err)
	}
	if len(cfg.Registries) != 1 {
		t.Fatalf("expected the existing entry to be reused (1 registry), got %d", len(cfg.Registries))
	}
	if cfg.Registries[0].URL != "/local/skills" {
		t.Fatalf("expected registry URL /local/skills, got %q", cfg.Registries[0].URL)
	}
}
