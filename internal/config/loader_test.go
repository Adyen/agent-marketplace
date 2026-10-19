package config

import (
	"errors"
	"io/fs"
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
	mustWriteFile(t, filepath.Join(repoRoot, ".agents", "config.yaml"), []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {}\n"))

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
		[]byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {}\n"))
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
		[]byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {}\n"))
	runGit(t, monoRepo, "init")

	// service-a has its own config within the same git repo.
	serviceA := filepath.Join(monoRepo, "services", "service-a")
	mustMkdirAll(t, filepath.Join(serviceA, ".agents"))
	mustWriteFile(t, filepath.Join(serviceA, ".agents", "config.yaml"),
		[]byte("apiVersion: agent-marketplace/v4\nsources: {}\n"))

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
apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources:
  security: {git: git@example.com:security.git, ref: main}
resources:
  security:
    required: true
    include: [skill/no-curl-bash]
    exclude: [skill/legacy-security]
    tags: [security]
`))

	cfg, err := LoadGlobalConfig()
	if err != nil {
		t.Fatalf("expected global config to load: %v", err)
	}

	if cfg.APIVersion != APIVersionV4 || !reflect.DeepEqual(cfg.Harnesses, []string{"factory"}) || len(cfg.Registries) != 1 {
		t.Fatalf("unexpected global config: %#v", cfg)
	}
	entry := cfg.Registries[0]
	if entry.URL != "git@example.com:security.git" || entry.Ref != "main" || entry.V4Kind != V4SourceGit || !entry.Required || !reflect.DeepEqual(entry.Skills, KindSelector{Include: []string{"no-curl-bash"}, Exclude: []string{"legacy-security"}}) || !reflect.DeepEqual(entry.Tags, []string{"security"}) {
		t.Fatalf("global source policy lost: %#v", entry)
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
apiVersion: agent-marketplace/v4
sources:
  infra: {git: git@example.com:infra.git, ref: v1.0.0}
resources:
  infra:
    include: [skill/vm-lifecycle]
    exclude: [skill/firewall-rule-lifecycle]
    tags: [k8s]
`))

	nestedDir := filepath.Join(repoRoot, "cmd", "agent-marketplace")
	mustMkdirAll(t, nestedDir)
	withWorkingDir(t, nestedDir)

	cfg, err := LoadRepoConfig()
	if err != nil {
		t.Fatalf("expected repo config to load: %v", err)
	}

	if cfg.APIVersion != APIVersionV4 || len(cfg.Registries) != 1 {
		t.Fatalf("unexpected repo config: %#v", cfg)
	}
	entry := cfg.Registries[0]
	if entry.URL != "git@example.com:infra.git" || entry.Ref != "v1.0.0" || entry.V4Kind != V4SourceGit || !reflect.DeepEqual(entry.Skills, KindSelector{Include: []string{"vm-lifecycle"}, Exclude: []string{"firewall-rule-lifecycle"}}) || !reflect.DeepEqual(entry.Tags, []string{"k8s"}) {
		t.Fatalf("repo source policy lost: %#v", entry)
	}
}

func TestLoadRepoConfigAllowsTopLevelHarnesses(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`
apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources:
  infra: {git: git@example.com:infra.git, ref: v1.0.0}
resources:
  infra: [skill/review]
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
apiVersion: agent-marketplace/v4
sources:
  infra: {git: git@example.com:infra.git, ref: main}
resources:
  infra: [skill/review]
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
	mustWriteFile(t, configPath, []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {}\n"))

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
	mustWriteFile(t, filepath.Join(homeDir, ".agents", "config.yaml"), []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {}\n"))

	// Create a real project nested below home.
	projectRoot := filepath.Join(homeDir, "projects", "myrepo")
	mustMkdirAll(t, filepath.Join(projectRoot, ".agents"))
	mustWriteFile(t, filepath.Join(projectRoot, ".agents", "config.yaml"), []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {}\n"))

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

func TestLoadLocalConfigLoadsSourceBoundSkillsAndExclusions(t *testing.T) {
	repoRoot := filepath.Join(t.TempDir(), "repo")
	mustMkdirAll(t, filepath.Join(repoRoot, ".agents"))
	mustWriteFile(t, filepath.Join(repoRoot, ".agents", "config.yaml"), []byte(`apiVersion: agent-marketplace/v4
harnesses: [factory]
sources: {}
`))
	configPath := filepath.Join(repoRoot, ".agents", "config.local.yaml")
	mustWriteFile(t, configPath, []byte(`
apiVersion: agent-marketplace/v4
sources:
  local: {git: git@example.com:local.git, ref: main}
resources:
  local:
    include: [skill/locally-enabled-skill]
    exclude: [skill/local-only-skill, skill/another-local-skill]
`))

	nestedDir := filepath.Join(repoRoot, "cmd", "agent-marketplace")
	mustMkdirAll(t, nestedDir)
	withWorkingDir(t, nestedDir)

	cfg, err := LoadLocalConfig()
	if err != nil {
		t.Fatalf("expected local config to load: %v", err)
	}

	if cfg.APIVersion != APIVersionV4 || len(cfg.Registries) != 1 || cfg.Registries[0].URL != "git@example.com:local.git" || cfg.Registries[0].Ref != "main" || !reflect.DeepEqual(cfg.Registries[0].Skills, KindSelector{Include: []string{"locally-enabled-skill"}, Exclude: []string{"local-only-skill", "another-local-skill"}}) {
		t.Fatalf("local source policy lost: %#v", cfg)
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
apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources:
  infra: {git: git@example.com:infra.git, ref: v1.0.0}
resources:
  infra: {required: true, include: [skill/vm-lifecycle]}
`))

	cfg, err := LoadConfigFromPath(path)
	if err != nil {
		t.Fatalf("expected config to load: %v", err)
	}

	if cfg.APIVersion != APIVersionV4 || !reflect.DeepEqual(cfg.Harnesses, []string{"factory"}) || len(cfg.Registries) != 1 {
		t.Fatalf("unexpected config: %#v", cfg)
	}
	entry := cfg.Registries[0]
	if entry.URL != "git@example.com:infra.git" || entry.Ref != "v1.0.0" || !entry.Required || !reflect.DeepEqual(entry.Skills.Include, []string{"vm-lifecycle"}) {
		t.Fatalf("source policy lost: %#v", entry)
	}
}

func TestLoadConfigFromPathReturnsValidationError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	mustWriteFile(t, path, []byte("apiVersion: agent-marketplace/v4\nsources: {shared: {git: '', ref: main}}\n"))
	_, err := LoadConfigFromPath(path)
	assertLoaderV4Error(t, err, "sources.shared", "v4.source-value", 2)
}

func TestLoadConfigFromPathReturnsParseError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	mustWriteFile(t, path, []byte("not: [valid yaml"))

	_, err := LoadConfigFromPath(path)
	if err == nil {
		t.Fatalf("expected parse error")
	}
	assertLoaderV4Error(t, err, canonicalPath(t, path), "v4.parse", 0)
}

func TestLoadConfigFromPathAllowsRepoHarnesses(t *testing.T) {
	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	mustWriteFile(t, configPath, []byte(`
apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources:
  infra: {git: git@example.com:infra.git, ref: main}
resources:
  infra: [skill/review]
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
apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources: {}
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
apiVersion: agent-marketplace/v4
sources:
  local: {directory: /local/skills}
resources:
  local: [skill/review]
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
apiVersion: agent-marketplace/v4
sources:
  local: {directory: /local/skills}
resources:
  local: {include: [skill/local-skill], exclude: [skill/opt-out-skill]}
`))

	cfg, err := LoadLocalConfigFromPath(path)
	if err != nil {
		t.Fatalf("expected local config to load: %v", err)
	}

	if cfg.APIVersion != APIVersionV4 || len(cfg.Registries) != 1 || cfg.Registries[0].URL != "/local/skills" || !cfg.Registries[0].IsLocal() || !reflect.DeepEqual(cfg.Registries[0].Skills, KindSelector{Include: []string{"local-skill"}, Exclude: []string{"opt-out-skill"}}) {
		t.Fatalf("local selection lost: %#v", cfg)
	}
}

func TestLoadLocalConfigFromPathReturnsParseError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.local.yaml")
	mustWriteFile(t, path, []byte("skills: [oops"))

	_, err := LoadLocalConfigFromPath(path)
	if err == nil {
		t.Fatalf("expected parse error")
	}
	assertLoaderV4Error(t, err, canonicalPath(t, path), "v4.parse", 0)
}

func TestLoadLocalConfigFromPathRejectsUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.local.yaml")
	mustWriteFile(t, path, []byte("apiVersion: agent-marketplace/v4\nsources: {local: {directory: /local/skills}}\nresources: {local: {exclude: [skill/opt-out]}}\nharnesses: [factory]\n"))
	_, err := LoadLocalConfigFromPath(path)
	assertLoaderV4Error(t, err, "harnesses", "v4.layer-field", 0)
}

// spec: consumer-config-format/explicit-non-v4-root-versions-are-refused
// spec: consumer-config-format/versionless-roots-are-refused-in-every-consumer-layer
func TestConfigAPIVersionPolicy(t *testing.T) {
	cases := []struct {
		name, version, code string
		line                int
	}{
		{"v4", "apiVersion: agent-marketplace/v4\n", "", 0},
		{"versionless", "", "v4.api-version-required", 1},
		{"v1", "apiVersion: agent-marketplace/v1\n", "v4.api-version", 1},
		{"v2", "apiVersion: agent-marketplace/v2\n", "v4.api-version", 1},
		{"v3", "apiVersion: agent-marketplace/v3\n", "v4.api-version", 1},
		{"unknown", "apiVersion: agent-marketplace/v99\n", "v4.api-version", 1},
		{"empty", "apiVersion: ''\n", "v4.api-version", 1},
		{"null", "apiVersion: null\n", "v4.api-version", 1},
		{"number", "apiVersion: 4\n", "v4.api-version", 1},
		{"list", "apiVersion: [agent-marketplace/v4]\n", "v4.api-version", 1},
		{"case", "apiVersion: Agent-Marketplace/v4\n", "v4.api-version", 1},
		{"padded", "apiVersion: ' agent-marketplace/v4 '\n", "v4.api-version", 1},
	}
	for _, loader := range []struct {
		name, filename string
		load           func(string) (*Config, error)
	}{
		{"config", "config.yaml", LoadConfigFromPath},
		{"local-via-config", "config.local.yaml", LoadConfigFromPath},
		{"local", "config.local.yaml", func(path string) (*Config, error) {
			cfg, err := LoadLocalConfigFromPath(path)
			if cfg == nil {
				return nil, err
			}
			return &Config{APIVersion: cfg.APIVersion, Registries: cfg.Registries}, err
		}},
		{"root-only", "config.yaml", LoadRootConfigFromPath},
	} {
		for _, tc := range cases {
			t.Run(loader.name+"/"+tc.name, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), loader.filename)
				before := tc.version + "sources: {local: {directory: /local/skills}}\nresources: {local: [skill/hello]}\n"
				mustWriteFile(t, path, []byte(before))
				cfg, err := loader.load(path)
				if tc.code != "" {
					assertLoaderV4Error(t, err, path, tc.code, tc.line)
					if cfg != nil {
						t.Fatalf("refused root exposed config: %#v", cfg)
					}
				} else if err != nil || cfg == nil || cfg.APIVersion != APIVersionV4 || len(cfg.Registries) != 1 || cfg.Registries[0].URL != "/local/skills" || !reflect.DeepEqual(cfg.Registries[0].Skills.Include, []string{"hello"}) {
					t.Fatalf("valid control lost selections: cfg=%#v err=%v", cfg, err)
				}
				if got, err := os.ReadFile(path); err != nil || string(got) != before {
					t.Fatalf("loader mutated root: %q %v", got, err)
				}
			})
		}
	}
}

func assertLoaderV4Error(t *testing.T, err error, path, code string, line int) {
	t.Helper()
	var validation *V4ValidationError
	if !errors.As(err, &validation) || validation.Code != code || validation.Path != path || validation.Line != line {
		t.Fatalf("error = %v, want %s at %s line %d", err, code, path, line)
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

func TestPublicLoadersRefuseV3WithV4Controls(t *testing.T) {
	for _, name := range []string{"global", "explicit-global", "repo", "explicit-repo"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			home := filepath.Join(dir, "home")
			repo := filepath.Join(dir, "repo")
			t.Setenv("HOME", home)
			path := filepath.Join(repo, ".agents", "config.yaml")
			if name == "global" || name == "explicit-global" {
				path = filepath.Join(home, ".agents", "config.yaml")
			}
			mustMkdirAll(t, filepath.Dir(path))
			withWorkingDir(t, filepath.Dir(filepath.Dir(path)))
			load := func() (*Config, error) {
				switch name {
				case "global":
					return LoadGlobalConfig()
				case "repo":
					return LoadRepoConfig()
				default:
					return LoadConfigFromPath(path)
				}
			}
			for _, version := range []string{"agent-marketplace/v3", "agent-marketplace/v4"} {
				mustWriteFile(t, path, []byte("apiVersion: "+version+"\nharnesses: [factory, claude]\n"))
				cfg, err := load()
				if version == "agent-marketplace/v3" {
					wantPath := path
					if name == "repo" {
						wantPath = canonicalPath(t, path)
					}
					assertLoaderV4Error(t, err, wantPath, "v4.api-version", 1)
					if cfg != nil {
						t.Fatalf("legacy config exposed: %#v", cfg)
					}
				} else if err != nil || cfg == nil || cfg.APIVersion != APIVersionV4 || !reflect.DeepEqual(cfg.Harnesses, []string{"factory", "claude"}) {
					t.Fatalf("v4 loader control: cfg=%#v err=%v", cfg, err)
				}
			}
		})
	}
}

// A config root that does not exist yet must get one canonical name however
// many of its directories are missing, so staged edits, the loader, and lock
// reconciliation agree on it. The home here is reached through a symlink, as
// it is under macOS /tmp and /var.
// spec: config-mutation/user-scope-install-creates-a-missing-user-root
func TestCanonicalConfigPathTolerateMissingResolvesTheDeepestExistingAncestor(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	resolvedReal, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ path, want string }{
		{filepath.Join(link, "config.yaml"), filepath.Join(resolvedReal, "config.yaml")},
		{filepath.Join(link, ".agents", "config.yaml"), filepath.Join(resolvedReal, ".agents", "config.yaml")},
		{filepath.Join(link, "a", "b", "config.yaml"), filepath.Join(resolvedReal, "a", "b", "config.yaml")},
	} {
		got, err := canonicalConfigPathTolerateMissing(tc.path)
		if err != nil || got != tc.want {
			t.Fatalf("canonicalConfigPathTolerateMissing(%q) = %q, %v; want %q", tc.path, got, err, tc.want)
		}
		if err := os.MkdirAll(filepath.Dir(tc.path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(tc.path, []byte("apiVersion: agent-marketplace/v4\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		existing, err := canonicalConfigPath(tc.path)
		if err != nil || existing != got {
			t.Fatalf("name after creating %q = %q, %v; want the pre-creation name %q", tc.path, existing, err, got)
		}
	}
}

// A dangling symlink on the path, as the file or as one of its directories,
// has no name that stays the same once its target exists. It is refused, and
// the error still reports a missing file so readers treat the layer as absent.
// spec: config-mutation/user-scope-install-through-a-dangling-symlink-is-refused
func TestCanonicalConfigPathTolerateMissingRefusesADanglingSymlink(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "dotfiles", "agents"), filepath.Join(home, ".agents")); err != nil {
		t.Fatal(err)
	}
	leafDir := filepath.Join(root, "leaf")
	if err := os.MkdirAll(leafDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "dotfiles", "config.yaml"), filepath.Join(leafDir, "config.yaml")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(home, ".agents", "config.yaml"),
		filepath.Join(home, ".agents", "nested", "config.yaml"),
		filepath.Join(leafDir, "config.yaml"),
	} {
		got, err := canonicalConfigPathTolerateMissing(path)
		if err == nil || !errors.Is(err, fs.ErrNotExist) || !strings.Contains(err.Error(), "symlink to a missing target") {
			t.Fatalf("canonicalConfigPathTolerateMissing(%q) = %q, %v; want a dangling-symlink error that wraps fs.ErrNotExist", path, got, err)
		}
	}
}
