package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"gopkg.in/yaml.v3"
)

// loadProjectConfig loads a project (repo) config without requiring harnesses.
func loadProjectConfig(t testing.TB, path string) (*Config, error) {
	t.Helper()
	return loadConfig(path, false)
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

func TestLayerEditorEnsureRegistryEntriesAndPaths(t *testing.T) {
	for _, tc := range []struct {
		name, initialSources string
		entry                RegistryEntry
		wantExistingPath     string
		wantCount            int
		wantChanged          bool
	}{
		{name: "adds entry", initialSources: "  existing: {git: 'git@example.com:existing.git', ref: main}\n", entry: RegistryEntry{URL: "git@example.com:new.git", Ref: "v2"}, wantCount: 2, wantChanged: true},
		{name: "idempotent", initialSources: "  existing: {git: 'git@example.com:existing.git', ref: main}\n", entry: RegistryEntry{URL: "git@example.com:existing.git", Ref: "main"}, wantCount: 1},
		{name: "adds path", initialSources: "  existing: {git: 'git@example.com:existing.git', ref: main}\n", entry: RegistryEntry{URL: "git@example.com:new.git", Ref: "main", Path: "skills"}, wantCount: 2, wantChanged: true},
		{name: "omits empty path", entry: RegistryEntry{URL: "git@example.com:new.git", Ref: "main"}, wantCount: 1, wantChanged: true},
		{name: "idempotent same path", initialSources: "  existing: {git: 'git@example.com:existing.git', ref: main, path: skills}\n", entry: RegistryEntry{URL: "git@example.com:existing.git", Ref: "main", Path: "skills"}, wantExistingPath: "skills", wantCount: 1},
		{name: "different paths coexist", initialSources: "  existing: {git: 'git@example.com:existing.git', ref: main, path: skills}\n", entry: RegistryEntry{URL: "git@example.com:existing.git", Ref: "main", Path: "commands"}, wantExistingPath: "skills", wantCount: 2, wantChanged: true},
		{name: "root and subpath coexist", initialSources: "  existing: {git: 'git@example.com:existing.git', ref: main, path: skills/firewall-rules-management}\n", entry: RegistryEntry{URL: "git@example.com:existing.git", Ref: "main"}, wantExistingPath: "skills/firewall-rules-management", wantCount: 2, wantChanged: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
			original := "apiVersion: agent-marketplace/v4\n# source comment\n"
			if tc.initialSources != "" {
				original += "sources:\n" + tc.initialSources
			}
			layerEditorWrite(t, configPath, original)
			editor := openTestEditor(t, RepoCommitted, configPath, configPath)
			binding, err := editor.EnsureRegistry(tc.entry)
			if err != nil {
				t.Fatalf("EnsureRegistry: %v", err)
			}
			candidate, err := editor.Render()
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if candidate.Changed != tc.wantChanged || candidate.Mode.Perm() != 0o640 {
				t.Fatalf("candidate changed=%v mode=%o", candidate.Changed, candidate.Mode.Perm())
			}
			cfg, err := ParseV4Config(candidate.Bytes, configPath, true)
			if err != nil {
				t.Fatalf("parse candidate: %v", err)
			}
			if len(cfg.Sources) != tc.wantCount {
				t.Fatalf("sources = %#v, want count %d", cfg.Sources, tc.wantCount)
			}
			got := cfg.Sources[binding.Name]
			if got.Git != tc.entry.URL || got.Ref != tc.entry.Ref || got.Path != tc.entry.Path {
				t.Fatalf("bound source = %#v, want %#v", got, tc.entry)
			}
			if tc.initialSources != "" {
				existing := cfg.Sources["existing"]
				if existing.Git != "git@example.com:existing.git" || existing.Ref != "main" || existing.Path != tc.wantExistingPath {
					t.Fatalf("existing source changed: %#v", existing)
				}
			}
			if tc.entry.Path == "" && strings.Contains(string(candidate.Bytes), "path:") && tc.wantExistingPath == "" {
				t.Fatalf("empty path was authored:\n%s", candidate.Bytes)
			}
			if !strings.Contains(string(candidate.Bytes), "# source comment") {
				t.Fatalf("comment lost:\n%s", candidate.Bytes)
			}
			if !tc.wantChanged && string(candidate.Bytes) != original {
				t.Fatal("idempotent ensure changed bytes")
			}
			if data, err := os.ReadFile(configPath); err != nil || string(data) != original {
				t.Fatalf("editor wrote source file: %s, %v", data, err)
			}
			layerEditorWrite(t, configPath, string(candidate.Bytes))
			editor = openTestEditor(t, RepoCommitted, configPath, configPath)
			if _, err := editor.EnsureRegistry(tc.entry); err != nil {
				t.Fatalf("repeat ensure: %v", err)
			}
			noop, err := editor.Render()
			if err != nil || noop.Changed || string(noop.Bytes) != string(candidate.Bytes) {
				t.Fatalf("repeat ensure changed bytes: %#v, %v", noop, err)
			}
		})
	}
}

func TestLayerEditorEnsureRegistryRejectsRepoRelativeGlobalWrite(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	configPath, err := GlobalConfigPath()
	if err != nil {
		t.Fatalf("GlobalConfigPath: %v", err)
	}
	// Editors operate on a staged root; a refusal must not create the missing
	// logical user root or alter the stage.
	staged := filepath.Join(t.TempDir(), "config.yaml")
	const original = "apiVersion: agent-marketplace/v4\n"
	layerEditorWrite(t, staged, original)
	editor := openTestEditor(t, UserPersonal, staged, configPath)
	_, err = editor.EnsureRegistry(RegistryEntry{URL: "./relative", Path: "skills", V4Kind: V4SourceDirectory})
	var boundary *GlobalRelativeRegistryError
	if !errors.As(err, &boundary) {
		t.Fatalf("expected GlobalRelativeRegistryError, got %T: %v", err, err)
	}
	if candidate, err := editor.Render(); err != nil || candidate.Changed || string(candidate.Bytes) != original {
		t.Fatalf("refusal changed candidate: %#v, %v", candidate, err)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("global config was written: %v", err)
	}
	if data, err := os.ReadFile(staged); err != nil || string(data) != original {
		t.Fatalf("staged config changed: %s, %v", data, err)
	}
}

func TestSetHarnessesInConfigReplacesHarnessesAndPreservesComments(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	configPath := filepath.Join(homeDir, ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`# user config
apiVersion: agent-marketplace/v4
harnesses:
  - factory # current default
sources:
  infra:
    git: git@example.com:infra.git
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

func TestSetUserMarketplaceExclusionCreatesV4UserConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".agents", "config.yaml")
	if err := SetUserMarketplaceExclusion(path, "https://market.example", "tools", ir.KindSkill, "review", true); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfigFromPath(path)
	if err != nil || cfg == nil || cfg.APIVersion != APIVersionV4 || len(cfg.MarketplaceExclusions) != 1 {
		t.Fatalf("new user config = %#v, %v", cfg, err)
	}
}

func TestSetUserMarketplaceExclusionRejectsLegacyConfigWithoutWriting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(path))
	original := []byte("apiVersion: agent-marketplace/v2\nharnesses: [factory]\n")
	mustWriteFile(t, path, original)
	err := SetUserMarketplaceExclusion(path, "https://market.example", "tools", ir.KindSkill, "review", true)
	var v4Err *V4ValidationError
	if !errors.As(err, &v4Err) || v4Err.Code != "v4.api-version" {
		t.Fatalf("legacy config rejection = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != string(original) {
		t.Fatalf("legacy config mutated: %s, %v", data, err)
	}
}

func TestSetUserMarketplaceExclusionEditsV4AndPreservesSelections(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(path))
	mustWriteFile(t, path, []byte(`apiVersion: agent-marketplace/v4
sources:
  company: {marketplace: https://market.example}
resources:
  company:
    include: [tools/skill/audit]
`))
	if err := SetUserMarketplaceExclusion(path, "https://market.example", "tools", ir.KindSkill, "review", true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "include: [tools/skill/audit]") || !strings.Contains(string(data), "tools/skill/review") {
		t.Fatalf("edited config lost original selection or exclusion: %s", data)
	}
	if err := SetUserMarketplaceExclusion(path, "https://market.example", "tools", ir.KindSkill, "review", false); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil || strings.Contains(string(data), "tools/skill/review") {
		t.Fatalf("exclusion not removed: %s, %v", data, err)
	}
}

// A removal of an exclusion that is not recorded, and a repeated exclusion,
// are no-ops: the authored bytes, mode and modification time stay untouched.
func TestSetUserMarketplaceExclusionNoOpLeavesFileUntouched(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(path))
	original := []byte("apiVersion: agent-marketplace/v4\nsources:\n  company:   {marketplace: https://market.example}\nresources:\n  company:\n    exclude:   [tools/skill/audit]   # keep\n")
	mustWriteFile(t, path, original)
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	frozen := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(path, frozen, frozen); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, excluded := range []bool{false, true} {
		name := "review"
		if excluded {
			name = "audit"
		}
		if err := SetUserMarketplaceExclusion(path, "https://market.example", "tools", ir.KindSkill, name, excluded); err != nil {
			t.Fatalf("no-op edit (excluded=%t): %v", excluded, err)
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != string(original) {
			t.Fatalf("no-op edit (excluded=%t) rewrote bytes: %s, %v", excluded, data, err)
		}
		after, err := os.Stat(path)
		if err != nil || after.Mode() != before.Mode() || !after.ModTime().Equal(before.ModTime()) {
			t.Fatalf("no-op edit (excluded=%t) changed mode or mtime: %v", excluded, err)
		}
	}
	// Control: a real removal still writes.
	if err := SetUserMarketplaceExclusion(path, "https://market.example", "tools", ir.KindSkill, "audit", false); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || strings.Contains(string(data), "tools/skill/audit") {
		t.Fatalf("control removal did not drop the exclusion: %s, %v", data, err)
	}
}

// TestSetHarnessesInConfigEditsV4Roots pins the TUI harness-modal save path:
// sources and resources in supported user and project roots survive the edit.
func TestSetHarnessesInConfigEditsV4Roots(t *testing.T) {
	const v4Body = `apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  agents:
    git: https://example.test/agents.git
    ref: main
resources:
  agents:
    include: [skill/review]
`

	assertHarnessesAndSource := func(t *testing.T, path string, want []string) {
		t.Helper()
		cfg, err := LoadConfigFromPath(path)
		if err != nil {
			t.Fatalf("reload edited v4 config: %v", err)
		}
		if cfg.APIVersion != APIVersionV4 {
			t.Fatalf("apiVersion = %q, want %q", cfg.APIVersion, APIVersionV4)
		}
		if !equalStrings(cfg.Harnesses, want) {
			t.Fatalf("harnesses = %v, want %v", cfg.Harnesses, want)
		}
		if len(cfg.Registries) != 1 || len(cfg.Registries[0].Skills.Include) == 0 || cfg.Registries[0].Skills.Include[0] != "review" {
			t.Fatalf("expected v4 source group preserved after harness edit, got %#v", cfg.Registries)
		}
	}

	t.Run("user config", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		configPath, err := GlobalConfigPath()
		if err != nil {
			t.Fatalf("GlobalConfigPath: %v", err)
		}
		mustMkdirAll(t, filepath.Dir(configPath))
		mustWriteFile(t, configPath, []byte(v4Body))

		if err := SetHarnessesInConfig(configPath, []string{"cursor", "factory"}); err != nil {
			t.Fatalf("SetHarnessesInConfig on v4 user config: %v", err)
		}
		assertHarnessesAndSource(t, configPath, []string{"cursor", "factory"})
	})

	t.Run("project config", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		repoPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
		mustMkdirAll(t, filepath.Dir(repoPath))
		mustWriteFile(t, repoPath, []byte(v4Body))

		if err := SetHarnessesInConfig(repoPath, []string{"factory"}); err != nil {
			t.Fatalf("SetHarnessesInConfig on v4 project config: %v", err)
		}
		assertHarnessesAndSource(t, repoPath, []string{"factory"})
	})
}

// TestSetHarnessesInConfigEditsV4MarketplaceRootWithoutLock pins the
// separation between document validity and Marketplace lock policy: the
// harness edit is a document-level operation and must not consult
// acquisition locks. A marketplace-source v4 root whose lock is absent
// still accepts a harness save, and the save must not create a lock.
func TestSetHarnessesInConfigEditsV4MarketplaceRootWithoutLock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configPath, err := GlobalConfigPath()
	if err != nil {
		t.Fatalf("GlobalConfigPath: %v", err)
	}
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(v4MarketplaceConfigYAML))

	if err := SetHarnessesInConfig(configPath, []string{"factory", "claude"}); err != nil {
		t.Fatalf("harness edit must not consult Marketplace lock policy: %v", err)
	}

	// The document is edited and still parses as v4 with the source intact.
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	v4, err := ParseV4(data)
	if err != nil {
		t.Fatalf("parse edited config: %v", err)
	}
	if !equalStrings(v4.Harnesses, []string{"factory", "claude"}) {
		t.Fatalf("harnesses = %v, want [factory claude]", v4.Harnesses)
	}
	if _, ok := v4.Sources["m"]; !ok {
		t.Fatalf("marketplace source was not preserved: %#v", v4.Sources)
	}

	// Non-vacuity control: the root stays in the exact lock-required state
	// a full load rejects, and the save neither created nor touched a lock.
	var v4Err *V4ValidationError
	if _, err := LoadConfigFromPath(configPath); !errors.As(err, &v4Err) || v4Err.Code != "v4.marketplace-lock-required" {
		t.Fatalf("full load error = %v, want v4.marketplace-lock-required", err)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".agents", "lock.yaml")); !os.IsNotExist(statErr) {
		t.Fatalf("harness save must not create an acquisition lock")
	}
	entries, err := os.ReadDir(filepath.Dir(configPath))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.yaml" {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("config directory must contain only config.yaml, got %v", names)
	}
}

// TestSetHarnessesInConfigRejectsInvalidV4DocumentWithoutWriting pins the
// fail-closed ordering: the harness edit validates the encoded candidate
// before writing, so a schema-invalid destination is refused with its
// stable diagnostic identity and no byte changes. (The earlier
// write-then-reload design left the written file behind the error.)
func TestSetHarnessesInConfigRejectsInvalidV4DocumentWithoutWriting(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	configPath, err := GlobalConfigPath()
	if err != nil {
		t.Fatalf("GlobalConfigPath: %v", err)
	}
	mustMkdirAll(t, filepath.Dir(configPath))
	original := "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nbogus: {}\n"
	mustWriteFile(t, configPath, []byte(original))

	err = SetHarnessesInConfig(configPath, []string{"claude"})
	var v4Err *V4ValidationError
	if !errors.As(err, &v4Err) || v4Err.Code != "v4.unknown-field" {
		t.Fatalf("error = %v, want v4.unknown-field", err)
	}

	after, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(after) != original {
		t.Fatalf("refused edit must not change bytes, got:\n%s", after)
	}
	entries, dirErr := os.ReadDir(filepath.Dir(configPath))
	if dirErr != nil {
		t.Fatal(dirErr)
	}
	if len(entries) != 1 || entries[0].Name() != "config.yaml" {
		t.Fatalf("refused edit must leave no staging residue, got %d entries", len(entries))
	}
}

func TestEnsureGlobalConfigLeavesAnExistingConfigAlone(t *testing.T) {
	for _, tc := range []struct {
		name, existing, wantCode string
	}{
		{name: "supported", existing: "apiVersion: agent-marketplace/v4\nharnesses: [cursor]\n"},
		{name: "unsupported", existing: "apiVersion: agent-marketplace/v2\nharnesses: [claude]\nregistries: []\n", wantCode: "v4.api-version"},
		{name: "empty", wantCode: "v4.api-version-required"},
	} {
		existing := tc.existing
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		globalPath := filepath.Join(homeDir, ".agents", "config.yaml")
		if err := os.MkdirAll(filepath.Dir(globalPath), 0o755); err != nil {
			t.Fatal(err)
		}
		mustWriteFile(t, globalPath, []byte(existing))
		if err := EnsureGlobalConfig(); layerEditorErrCode(err) != tc.wantCode || (tc.wantCode == "" && err != nil) {
			t.Fatalf("EnsureGlobalConfig %s = %v, want code %q", tc.name, err, tc.wantCode)
		}
		data, err := os.ReadFile(globalPath)
		if err != nil || string(data) != existing {
			t.Fatalf("EnsureGlobalConfig rewrote an existing config to %q (err %v)", data, err)
		}
	}

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	globalPath := filepath.Join(homeDir, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(globalPath), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(homeDir, "dotfiles", "agents.yaml")
	if err := os.Symlink(target, globalPath); err != nil {
		t.Fatal(err)
	}
	if err := EnsureGlobalConfig(); err != nil {
		t.Fatalf("EnsureGlobalConfig with a dangling symlink: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("EnsureGlobalConfig wrote through a dangling symlink: %v", err)
	}
}

// spec: logging/new-user-configuration-omits-logging-overrides
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
		"apiVersion: agent-marketplace/v4",
		"harnesses:",
	} {
		if !strings.Contains(content, needle) {
			t.Fatalf("expected skeleton to contain %q, got:\n%s", needle, content)
		}
	}
	if _, err := LoadV4ConfigFromPath(globalPath); err != nil {
		t.Fatalf("generated skeleton is not a valid v4 config: %v\n%s", err, content)
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
		if root.Content[i].Value == "apiVersion" && root.Content[i+1].Value == APIVersionV4 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("skeleton config must contain apiVersion %q", APIVersionV4)
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

// The removed URL/path-alias writer must not be replaced by schema guessing.
func TestLayerEditorRefusesLegacyRegistryLocationAliases(t *testing.T) {
	for _, entry := range []string{
		"{url: 'git@example.com:infra.git', path: skills, ref: main}",
		"{path: /local/skills}",
		"{url: '', path: /local/skills}",
		"{url: '   ', path: /local/skills}",
		"{ref: main}",
		"{url: '', ref: main}",
	} {
		t.Run(entry, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
			original := "apiVersion: agent-marketplace/v2\nregistries:\n  - " + entry + "\n"
			layerEditorWrite(t, configPath, original)
			_, err := OpenLayerEditor(LayerTarget{Source: RepoCommitted, StagedPath: configPath, DeclaringPath: configPath})
			var validation *V4ValidationError
			if !errors.As(err, &validation) || validation.Code != "v4.api-version" || !strings.Contains(validation.Path, configPath) || !strings.Contains(validation.Msg, "apiVersion") {
				t.Fatalf("legacy alias root error = %v", err)
			}
			if data, err := os.ReadFile(configPath); err != nil || string(data) != original {
				t.Fatalf("refusal changed bytes: %s, %v", data, err)
			}
		})
	}
}

func TestLayerEditorEnsureRegistryMatchesDirectorySource(t *testing.T) {
	repo := canonicalDirectoryTestRoot(t)
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	const original = "apiVersion: agent-marketplace/v4\nsources:\n  local: {directory: ./local/skills}\nresources:\n  local: [skill/review]\n"
	layerEditorWrite(t, configPath, original)
	editor := openTestEditor(t, RepoCommitted, configPath, configPath)
	binding, err := editor.EnsureRegistry(RegistryEntry{URL: filepath.Join(repo, "local", "skills"), V4Kind: V4SourceDirectory})
	if err != nil || binding.Name != "local" {
		t.Fatalf("directory source binding = %#v, %v", binding, err)
	}
	candidate, err := editor.Render()
	if err != nil || candidate.Changed || string(candidate.Bytes) != original {
		t.Fatalf("directory source duplicated or rewritten: %#v, %v", candidate, err)
	}
	cfg, err := loadProjectConfig(t, configPath)
	if err != nil || len(cfg.Registries) != 1 || cfg.Registries[0].URL != filepath.Join(repo, "local", "skills") || !reflect.DeepEqual(cfg.Registries[0].Skills.Include, []string{"review"}) {
		t.Fatalf("directory source/selection = %#v, %v", cfg, err)
	}
}
