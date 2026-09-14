package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFindRepoRootWalksUpToAgentsConfig(t *testing.T) {
	repoRoot := filepath.Join(t.TempDir(), "repo")
	mustMkdirAll(t, filepath.Join(repoRoot, ".agents"))
	mustWriteFile(t, filepath.Join(repoRoot, ".agents", "config.yaml"), []byte("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries: []\n"))

	nestedDir := filepath.Join(repoRoot, "internal", "config")
	mustMkdirAll(t, nestedDir)
	withWorkingDir(t, nestedDir)

	if got := canonicalPath(t, FindRepoRoot()); got != canonicalPath(t, repoRoot) {
		t.Fatalf("expected repo root %q, got %q", canonicalPath(t, repoRoot), got)
	}
}

func TestFindRepoRootFallsBackToGitTopLevel(t *testing.T) {
	repoRoot := filepath.Join(t.TempDir(), "repo")
	mustMkdirAll(t, repoRoot)
	runGit(t, repoRoot, "init")

	nestedDir := filepath.Join(repoRoot, "internal", "config")
	mustMkdirAll(t, nestedDir)
	withWorkingDir(t, nestedDir)

	if got := canonicalPath(t, FindRepoRoot()); got != canonicalPath(t, repoRoot) {
		t.Fatalf("expected git top-level %q, got %q", canonicalPath(t, repoRoot), got)
	}
}

// TestFindRepoRootDoesNotCrossGitBoundary verifies that a .agents/config.yaml
// in a PARENT git repo does not "steal" FindRepoRoot for a nested child repo.
// This was the root cause of amd install writing to the wrong project when
// skill-eval was checked out inside agent-marketplace or a similarly
// structured parent workspace.
func TestFindRepoRootDoesNotCrossGitBoundary(t *testing.T) {
	tmp := t.TempDir()

	// Parent git repo with its own .agents/config.yaml.
	parentRepo := filepath.Join(tmp, "parent")
	mustMkdirAll(t, filepath.Join(parentRepo, ".agents"))
	mustWriteFile(t, filepath.Join(parentRepo, ".agents", "config.yaml"),
		[]byte("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries: []\n"))
	runGit(t, parentRepo, "init")

	// Child git repo nested inside the parent (no .agents/config.yaml of its own).
	childRepo := filepath.Join(parentRepo, "child")
	mustMkdirAll(t, childRepo)
	runGit(t, childRepo, "init")

	nestedDir := filepath.Join(childRepo, "pkg", "lib")
	mustMkdirAll(t, nestedDir)
	withWorkingDir(t, nestedDir)

	got := canonicalPath(t, FindRepoRoot())
	want := canonicalPath(t, childRepo)
	if got != want {
		t.Fatalf("FindRepoRoot() = %q; want child repo root %q (must not cross git boundary into parent)", got, want)
	}
}

// TestFindRepoRootMonorepoSubdirectoryConfig verifies that a subdirectory
// config within the SAME git repo is found before the root config. This
// ensures monorepos with per-service .agents/config.yaml files work correctly.
func TestFindRepoRootMonorepoSubdirectoryConfig(t *testing.T) {
	tmp := t.TempDir()

	// Single git repo (monorepo) with a root-level config.
	monoRepo := filepath.Join(tmp, "monorepo")
	mustMkdirAll(t, filepath.Join(monoRepo, ".agents"))
	mustWriteFile(t, filepath.Join(monoRepo, ".agents", "config.yaml"),
		[]byte("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries: []\n"))
	runGit(t, monoRepo, "init")

	// service-a has its own config within the same git repo.
	serviceA := filepath.Join(monoRepo, "services", "service-a")
	mustMkdirAll(t, filepath.Join(serviceA, ".agents"))
	mustWriteFile(t, filepath.Join(serviceA, ".agents", "config.yaml"),
		[]byte("apiVersion: agent-marketplace/v2\nregistries: []\n"))

	// cwd is a nested directory inside service-a.
	cwd := filepath.Join(serviceA, "src", "handler")
	mustMkdirAll(t, cwd)
	withWorkingDir(t, cwd)

	got := canonicalPath(t, FindRepoRoot())
	want := canonicalPath(t, serviceA)
	if got != want {
		t.Fatalf("FindRepoRoot() = %q; want service-a root %q (nearest config within same git repo)", got, want)
	}
}

func TestLoadGlobalConfigReturnsNilWhenMissing(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	cfg, err := LoadGlobalConfig()
	if err != nil {
		t.Fatalf("expected missing global config to return no error, got %v", err)
	}

	if cfg != nil {
		t.Fatalf("expected missing global config to return nil config, got %#v", cfg)
	}
}

func TestLoadGlobalConfigLoadsAndValidatesYAML(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	configPath := filepath.Join(homeDir, ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`
apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: git@example.com:security.git
    ref: main
    required: true
    skills:
      include:
        - no-curl-bash
      exclude:
        - legacy-security
    tags:
      - security
tags:
  - security
`))

	cfg, err := LoadGlobalConfig()
	if err != nil {
		t.Fatalf("expected global config to load: %v", err)
	}

	want := &Config{
		APIVersion: APIVersionV2,
		Harnesses:  []string{"factory"},
		Registries: []RegistryEntry{{
			URL:      "git@example.com:security.git",
			Ref:      "main",
			Skills:   KindSelector{Include: []string{"no-curl-bash"}, Exclude: []string{"legacy-security"}},
			Tags:     []string{"security"},
			Required: true,
		}},
		Tags: []string{"security"},
	}

	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("expected global config %#v, got %#v", want, cfg)
	}
}

func TestExplicitOrganizationLoadersUseOrganizationValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	mustWriteFile(t, path, []byte(`apiVersion: agent-marketplace/v3
telemetry:
  level: detailed
registries: []
`))
	SetTestOrganizationConfigPath(t, path)

	for _, load := range []struct {
		name string
		fn   func(string) (*Config, error)
	}{
		{name: "expanded", fn: LoadConfigFromPath},
		{name: "root", fn: LoadRootConfigFromPath},
	} {
		t.Run(load.name, func(t *testing.T) {
			cfg, err := load.fn(path)
			if err != nil {
				t.Fatalf("load organization config: %v", err)
			}
			if cfg == nil || cfg.Telemetry == nil || cfg.Telemetry.Level != "detailed" {
				t.Fatalf("telemetry = %#v, want detailed organization telemetry", cfg)
			}
		})
	}
}

func TestDefaultHarnesses(t *testing.T) {
	harnesses := DefaultHarnesses()
	if len(harnesses) == 0 {
		t.Fatalf("expected non-empty default harnesses, got %v", harnesses)
	}
	// Must match the DefaultHarnesses constant: [factory, windsurf]
	if !equalStrings(harnesses, []string{"factory", "windsurf"}) {
		t.Fatalf("expected default harnesses [factory, windsurf], got %v", harnesses)
	}
}

func TestLoadRepoConfigLoadsFromRepoRoot(t *testing.T) {
	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`
apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra.git
    ref: v1.0.0
    skills:
      include:
        - vm-lifecycle
      exclude:
        - firewall-rule-lifecycle
tags:
  - k8s
`))

	nestedDir := filepath.Join(repoRoot, "cmd", "agent-marketplace")
	mustMkdirAll(t, nestedDir)
	withWorkingDir(t, nestedDir)

	cfg, err := LoadRepoConfig()
	if err != nil {
		t.Fatalf("expected repo config to load: %v", err)
	}

	want := &Config{
		APIVersion: APIVersionV2,
		Registries: []RegistryEntry{{
			URL:    "git@example.com:infra.git",
			Ref:    "v1.0.0",
			Skills: KindSelector{Include: []string{"vm-lifecycle"}, Exclude: []string{"firewall-rule-lifecycle"}},
		}},
		Tags: []string{"k8s"},
	}

	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("expected repo config %#v, got %#v", want, cfg)
	}
}

func TestLoadRepoConfigAllowsTopLevelHarnesses(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`
apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: git@example.com:infra.git
    ref: v1.0.0
`))

	nestedDir := filepath.Join(repoRoot, "cmd", "agent-marketplace")
	mustMkdirAll(t, nestedDir)
	withWorkingDir(t, nestedDir)

	cfg, err := LoadRepoConfig()
	if err != nil {
		t.Fatalf("expected repo config with top-level harnesses to load: %v", err)
	}
	if !equalStrings(cfg.Harnesses, []string{"factory"}) {
		t.Fatalf("expected repo harnesses [factory], got %v", cfg.Harnesses)
	}
}

func TestLoadGlobalConfigSucceedsWithoutHarnesses(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	configPath := filepath.Join(homeDir, ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`
apiVersion: agent-marketplace/v2
registries:
  - url: git@example.com:infra.git
    ref: main
`))

	cfg, err := LoadGlobalConfig()
	if err != nil {
		t.Fatalf("expected global config without harnesses to load: %v", err)
	}
	if cfg == nil {
		t.Fatalf("expected non-nil config")
	}
	if len(cfg.Harnesses) != 0 {
		t.Fatalf("expected empty harnesses, got %v", cfg.Harnesses)
	}
}

func TestLoadRepoConfigReturnsNilWhenCwdIsHomeDir(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// Create ~/.agents/config.yaml — this is the user-personal config, NOT a project config.
	configPath := filepath.Join(homeDir, ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries: []\n"))

	withWorkingDir(t, homeDir)

	cfg, err := LoadRepoConfig()
	if err != nil {
		t.Fatalf("expected no error when cwd is home dir, got %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil repo config when cwd is home directory (to avoid duplicate loading), got %#v", cfg)
	}
}

func TestFindRepoRootSkipsHomeDirAgentsConfigButFindsNestedProject(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// Create ~/.agents/config.yaml (user-personal config).
	mustMkdirAll(t, filepath.Join(homeDir, ".agents"))
	mustWriteFile(t, filepath.Join(homeDir, ".agents", "config.yaml"), []byte("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries: []\n"))

	// Create a real project nested below home.
	projectRoot := filepath.Join(homeDir, "projects", "myrepo")
	mustMkdirAll(t, filepath.Join(projectRoot, ".agents"))
	mustWriteFile(t, filepath.Join(projectRoot, ".agents", "config.yaml"), []byte("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries: []\n"))

	nested := filepath.Join(projectRoot, "internal", "pkg")
	mustMkdirAll(t, nested)
	withWorkingDir(t, nested)

	if got := canonicalPath(t, FindRepoRoot()); got != canonicalPath(t, projectRoot) {
		t.Fatalf("expected project root %q, got %q", projectRoot, got)
	}
}

func TestLoadRepoConfigReturnsNilWhenFileMissing(t *testing.T) {
	repoRoot := filepath.Join(t.TempDir(), "repo")
	mustMkdirAll(t, repoRoot)
	runGit(t, repoRoot, "init")

	nestedDir := filepath.Join(repoRoot, "internal", "config")
	mustMkdirAll(t, nestedDir)
	withWorkingDir(t, nestedDir)

	cfg, err := LoadRepoConfig()
	if err != nil {
		t.Fatalf("expected missing repo config to return nil error, got: %v", err)
	}

	if cfg != nil {
		t.Fatalf("expected missing repo config to return nil config, got %#v", cfg)
	}
}

func TestLoadLocalConfigTypeIsDefined(t *testing.T) {
	cfg := LocalConfig{Skills: KindSelector{Exclude: []string{"local-only-skill"}}}

	if !reflect.DeepEqual(cfg.Skills.Exclude, []string{"local-only-skill"}) {
		t.Fatalf("expected local config skills exclude to round-trip, got %#v", cfg.Skills.Exclude)
	}
}

func TestLoadLocalConfigReturnsNilWhenMissing(t *testing.T) {
	repoRoot := filepath.Join(t.TempDir(), "repo")
	mustMkdirAll(t, repoRoot)
	runGit(t, repoRoot, "init")

	nestedDir := filepath.Join(repoRoot, "internal", "config")
	mustMkdirAll(t, nestedDir)
	withWorkingDir(t, nestedDir)

	cfg, err := LoadLocalConfig()
	if err != nil {
		t.Fatalf("expected missing local config to return no error, got %v", err)
	}

	if cfg != nil {
		t.Fatalf("expected missing local config to return nil config, got %#v", cfg)
	}
}

func TestLoadLocalConfigLoadsSkillsExcludeAndIgnoresOtherFields(t *testing.T) {
	repoRoot := filepath.Join(t.TempDir(), "repo")
	mustMkdirAll(t, filepath.Join(repoRoot, ".agents"))
	mustWriteFile(t, filepath.Join(repoRoot, ".agents", "config.yaml"), []byte(`apiVersion: agent-marketplace/v2
harnesses: [factory]
registries: []
`))
	configPath := filepath.Join(repoRoot, ".agents", "config.local.yaml")
	mustWriteFile(t, configPath, []byte(`
skills:
  include:
    - locally-enabled-skill
  exclude:
    - local-only-skill
    - another-local-skill
registries:
  - url: git@example.com:ignored.git
    ref: ignored
`))

	nestedDir := filepath.Join(repoRoot, "cmd", "agent-marketplace")
	mustMkdirAll(t, nestedDir)
	withWorkingDir(t, nestedDir)

	cfg, err := LoadLocalConfig()
	if err != nil {
		t.Fatalf("expected local config to load: %v", err)
	}

	want := &LocalConfig{
		Registries: []RegistryEntry{{URL: "git@example.com:ignored.git", Ref: "ignored"}},
		Skills:     KindSelector{Include: []string{"locally-enabled-skill"}, Exclude: []string{"local-only-skill", "another-local-skill"}},
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("expected local config %#v, got %#v", want, cfg)
	}
}

func TestLoadConfigFromPathReturnsNilWhenMissing(t *testing.T) {
	cfg, err := LoadConfigFromPath(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil config, got %#v", cfg)
	}
}

func TestLoadConfigFromPathLoadsValidFile(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	path := filepath.Join(homeDir, ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(path))
	mustWriteFile(t, path, []byte(`
apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: git@example.com:infra.git
    ref: v1.0.0
    required: true
    skills:
      - vm-lifecycle
`))

	cfg, err := LoadConfigFromPath(path)
	if err != nil {
		t.Fatalf("expected config to load: %v", err)
	}

	want := &Config{
		APIVersion: APIVersionV2,
		Harnesses:  []string{"factory"},
		Registries: []RegistryEntry{{
			URL:      "git@example.com:infra.git",
			Ref:      "v1.0.0",
			Required: true,
			Skills:   KindSelector{Include: []string{"vm-lifecycle"}},
		}},
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("expected %#v, got %#v", want, cfg)
	}
}

func TestLoadConfigFromPathReturnsValidationError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	mustWriteFile(t, path, []byte(`
registries:
  - url: ""
    ref: main
`))

	_, err := LoadConfigFromPath(path)
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "registries[0].url is required") {
		t.Fatalf("expected url validation error, got %v", err)
	}
}

func TestLoadConfigFromPathReturnsParseError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	mustWriteFile(t, path, []byte("not: [valid yaml"))

	_, err := LoadConfigFromPath(path)
	if err == nil {
		t.Fatalf("expected parse error")
	}
	if !strings.Contains(err.Error(), "parse config") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func TestLoadConfigFromPathAllowsRepoHarnesses(t *testing.T) {
	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`
apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: git@example.com:infra.git
    ref: main
`))

	cfg, err := LoadConfigFromPath(configPath)
	if err != nil {
		t.Fatalf("expected repo config with top-level harnesses to load: %v", err)
	}
	if !reflect.DeepEqual(cfg.Harnesses, []string{"factory"}) {
		t.Fatalf("expected repo harnesses [factory], got %v", cfg.Harnesses)
	}

	// Global and local configs should still load.
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	globalPath := filepath.Join(homeDir, ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(globalPath))
	mustWriteFile(t, globalPath, []byte(`
apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries: []
`))

	globalCfg, err := LoadConfigFromPath(globalPath)
	if err != nil {
		t.Fatalf("expected global config with harnesses to load: %v", err)
	}
	if !reflect.DeepEqual(globalCfg.Harnesses, []string{"factory"}) {
		t.Fatalf("expected global harnesses [factory], got %v", globalCfg.Harnesses)
	}

	localPath := filepath.Join(t.TempDir(), "config.local.yaml")
	mustWriteFile(t, localPath, []byte(`
registries:
  - url: /local/skills
    ref: main
`))

	_, err = LoadConfigFromPath(localPath)
	if err != nil {
		t.Fatalf("expected local config to load: %v", err)
	}
}

func TestLoadLocalConfigFromPathReturnsNilWhenMissing(t *testing.T) {
	cfg, err := LoadLocalConfigFromPath(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil config, got %#v", cfg)
	}
}

func TestLoadLocalConfigFromPathLoadsSkillsAndExclude(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.local.yaml")
	mustWriteFile(t, path, []byte(`
skills:
  include:
    - local-skill
  exclude:
    - opt-out-skill
registries:
  - url: /local/skills
    ref: main
`))

	cfg, err := LoadLocalConfigFromPath(path)
	if err != nil {
		t.Fatalf("expected local config to load: %v", err)
	}

	want := &LocalConfig{
		Registries: []RegistryEntry{{URL: "/local/skills", Ref: "main"}},
		Skills:     KindSelector{Include: []string{"local-skill"}, Exclude: []string{"opt-out-skill"}},
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("expected %#v, got %#v", want, cfg)
	}
}

func TestLoadLocalConfigFromPathReturnsParseError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.local.yaml")
	mustWriteFile(t, path, []byte("skills: [oops"))

	_, err := LoadLocalConfigFromPath(path)
	if err == nil {
		t.Fatalf("expected parse error")
	}
	if !strings.Contains(err.Error(), "parse config") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func TestLoadLocalConfigFromPathRejectsUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.local.yaml")
	mustWriteFile(t, path, []byte(`
skills:
  exclude: [opt-out]
harnesses: [factory]
`))

	_, err := LoadLocalConfigFromPath(path)
	if err == nil {
		t.Fatalf("expected strict parse error for unknown key 'harnesses'")
	}
	if !strings.Contains(err.Error(), "harnesses") {
		t.Fatalf("expected error to mention unknown key 'harnesses', got %v", err)
	}
}

// TestConfigAPIVersionPolicy is a table-driven matrix covering how the loaders
// gate apiVersion across local vs. non-local config paths:
//   - local config (LoadLocalConfigFromPath) tolerates an optional apiVersion,
//     but a mismatched one is still rejected;
//   - a local path loaded through the full Config loader (LoadConfigFromPath,
//     the editor / remove --from path) does NOT require apiVersion, while a
//     non-local path still does.
func TestConfigAPIVersionPolicy(t *testing.T) {
	cases := []struct {
		name        string
		fileName    string
		content     string
		useLocal    bool   // true → LoadLocalConfigFromPath; false → LoadConfigFromPath
		wantErr     bool   // expect a load error
		errSubstr   string // when wantErr, error must contain this substring
		verifyLocal func(t *testing.T, cfg *LocalConfig)
		verifyCfg   func(t *testing.T, cfg *Config)
	}{
		{
			// A local config may carry an optional apiVersion line (added by
			// hand or copied from a sibling) without tripping the strict
			// KnownFields decoder ("field apiVersion not found in ...").
			name:     "local tolerates apiVersion",
			fileName: "config.local.yaml",
			content: `
apiVersion: agent-marketplace/v2
skills:
  include: [local-skill]
registries:
  - url: /local/skills
    ref: main
`,
			useLocal: true,
			verifyLocal: func(t *testing.T, cfg *LocalConfig) {
				if cfg.APIVersion != APIVersionV2 {
					t.Fatalf("expected apiVersion %q, got %q", APIVersionV2, cfg.APIVersion)
				}
				if !reflect.DeepEqual(cfg.Skills, KindSelector{Include: []string{"local-skill"}}) {
					t.Fatalf("unexpected skills selector: %#v", cfg.Skills)
				}
			},
		},
		{
			// A mismatched apiVersion is still rejected so typos / stale
			// versions surface.
			name:     "local rejects wrong apiVersion",
			fileName: "config.local.yaml",
			content: `
apiVersion: agent-marketplace/v1
skills:
  include: [local-skill]
`,
			useLocal:  true,
			wantErr:   true,
			errSubstr: "unsupported apiVersion",
		},
		{
			// Loading a local config path through the full Config loader (the
			// editor and remove --from flows) must not demand apiVersion; local
			// configs are generated without it, so requiring it broke
			// `remove --scope local`.
			name:     "local path via Config loader skips apiVersion requirement",
			fileName: "config.local.yaml",
			content: `
registries:
  - url: /local/skills
    ref: main
    skills:
      include: [hello]
`,
			verifyCfg: func(t *testing.T, cfg *Config) {
				if cfg == nil || len(cfg.Registries) != 1 {
					t.Fatalf("expected one registry entry, got %#v", cfg)
				}
			},
		},
		{
			// The leniency must not leak into project/global configs, which
			// must still declare apiVersion.
			name:     "non-local path still requires apiVersion",
			fileName: "config.yaml",
			content: `
registries:
  - url: /repo/skills
    ref: main
`,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tc.fileName)
			mustWriteFile(t, path, []byte(tc.content))

			var (
				localCfg *LocalConfig
				cfg      *Config
				err      error
			)
			if tc.useLocal {
				localCfg, err = LoadLocalConfigFromPath(path)
			} else {
				cfg, err = LoadConfigFromPath(path)
			}

			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected load error, got nil")
				}
				if tc.errSubstr != "" && !strings.Contains(err.Error(), tc.errSubstr) {
					t.Fatalf("expected error to contain %q, got %v", tc.errSubstr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected config to load, got error: %v", err)
			}
			if tc.verifyLocal != nil {
				tc.verifyLocal(t, localCfg)
			}
			if tc.verifyCfg != nil {
				tc.verifyCfg(t, cfg)
			}
		})
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("expected mkdir %q to succeed: %v", path, err)
	}
}

func mustWriteFile(t *testing.T, path string, contents []byte) {
	t.Helper()

	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatalf("expected write file %q to succeed: %v", path, err)
	}
}

func withWorkingDir(t *testing.T, dir string) {
	t.Helper()

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("expected getwd to succeed: %v", err)
	}

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("expected chdir to %q to succeed: %v", dir, err)
	}

	t.Cleanup(func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Fatalf("expected restore cwd to %q to succeed: %v", originalDir, err)
		}
	})
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected git %v to succeed: %v (%s)", args, err, output)
	}
}

func canonicalPath(t *testing.T, path string) string {
	t.Helper()

	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return filepath.Clean(path)
	}

	return filepath.Clean(resolved)
}

func TestLoadGlobalConfigLoadsV3Minimal(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	configPath := filepath.Join(homeDir, ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v3
harnesses:
  - factory
  - claude
`))

	cfg, err := LoadGlobalConfig()
	if err != nil {
		t.Fatalf("expected v3 global config to load: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.APIVersion != APIVersionV3 {
		t.Fatalf("expected apiVersion %q, got %q", APIVersionV3, cfg.APIVersion)
	}
	if !reflect.DeepEqual(cfg.Harnesses, []string{"factory", "claude"}) {
		t.Fatalf("expected harnesses [factory claude], got %v", cfg.Harnesses)
	}
}

func TestLoadConfigFromPathLoadsV3Global(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	configPath := filepath.Join(homeDir, ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v3
harnesses:
  - factory
`))

	cfg, err := LoadConfigFromPath(configPath)
	if err != nil {
		t.Fatalf("expected v3 config to load: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.APIVersion != APIVersionV3 {
		t.Fatalf("expected apiVersion %q, got %q", APIVersionV3, cfg.APIVersion)
	}
}

func TestLoadConfigAcceptsV3(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	mustWriteFile(t, path, []byte(`apiVersion: agent-marketplace/v3
harnesses:
  - factory
registries:
  - url: git@example.com:repo.git
    ref: main
`))

	cfg, err := loadConfig(path, true)
	if err != nil {
		t.Fatalf("expected v3 config to load through loadConfig: %v", err)
	}
	if cfg.APIVersion != APIVersionV3 {
		t.Fatalf("expected apiVersion %q, got %q", APIVersionV3, cfg.APIVersion)
	}
}

func TestLoadConfigFromPathAllowsV3RepoHarnesses(t *testing.T) {
	// A config.yaml that is not the global path is treated as a repo config.
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	mustWriteFile(t, configPath, []byte(`apiVersion: agent-marketplace/v3
harnesses:
  - factory
`))

	cfg, err := LoadConfigFromPath(configPath)
	if err != nil {
		t.Fatalf("expected v3 repo config with harnesses to load: %v", err)
	}
	if !reflect.DeepEqual(cfg.Harnesses, []string{"factory"}) {
		t.Fatalf("expected v3 repo harnesses [factory], got %#v", cfg.Harnesses)
	}
}
