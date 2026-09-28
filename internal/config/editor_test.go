package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
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
	if !errors.As(err, &v4Err) || v4Err.Code != "v4.writer-opt-in" {
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

// TestSetHarnessesInConfigEditsV4Roots pins the TUI harness-modal save path
// against the v4.writer-opt-in regression: init writes the user-personal
// root as v4 directly and the TUI startup offers migration, so harness
// editing must be an explicit v4-aware operation rather than a rejected
// legacy write. Sources and resources must survive the edit.
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
