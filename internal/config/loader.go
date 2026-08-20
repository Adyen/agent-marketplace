package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// fileExists reports whether the given path exists (file or directory).
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// gitRootProbe forks `git rev-parse --show-toplevel` in dir and returns the
// cleaned repository root. The second result is true when the answer is a
// genuine worktree boundary (including "not a repository"); it is false when
// the probe itself failed (git missing, killed, or timed out). Failed probes
// must not be memoized as "not a repo".
//
// It is a package-level variable so tests can substitute a deterministic,
// countable stand-in instead of depending on a real git binary.
var gitRootProbe = probeGitRoot

func probeGitRoot(dir string) (string, bool) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return "", true
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	// Run in the directory we are keying the cache on rather than relying on
	// the process working directory, so the memo key is the actual input.
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if ctx.Err() == nil && errors.As(err, &exitErr) {
			// git ran and reported "not a repository": a real, cacheable answer.
			return "", true
		}
		slog.Warn("git root probe failed", "dir", dir, "error", err)
		return "", false
	}
	r := strings.TrimSpace(string(out))
	if r == "" {
		return "", true
	}
	return filepath.Clean(r), true
}

// gitRootCache memoizes gitRootProbe per working directory.
//
// Only the git boundary is memoized, never the full FindRepoRoot result. The
// `.agents/config.yaml` boundary is mutated by our own commands mid-process
// (install --scope repo seeds a skeleton config in a monorepo subdirectory and
// then expects FindRepoRoot to return that subdirectory), so the upward walk
// must keep observing live filesystem state. The git boundary of a directory,
// by contrast, is a stable property of the checkout for the lifetime of a CLI
// invocation — and it is the only part that costs a process fork (~18 ms).
//
// The key is the working directory, so a test that chdirs into its own
// t.TempDir() can never observe another test's memo. A "" (not in a git repo)
// answer is cached like any other genuine result. Failed probes are not cached
// so a transient git stall can self-correct on the next call.
//
// A per-directory mutex is kept so concurrent calls for the same directory only
// probe once while calls for different directories do not block each other.
var (
	gitRootMu    sync.Mutex
	gitRootCache = map[string]string{}
	gitRootLocks = map[string]*sync.Mutex{}
)

// gitRootFor returns the git worktree root containing dir, resolving it at
// most once per directory per process for successful probes.
func gitRootFor(dir string) string {
	// Fast path: read the cache.
	gitRootMu.Lock()
	if root, ok := gitRootCache[dir]; ok {
		gitRootMu.Unlock()
		return root
	}

	// Slow path: obtain the per-directory lock so only one goroutine probes
	// for this directory. Other goroutines for the same dir block here; callers
	// for other dirs can proceed in parallel. Release the cache mutex before
	// waiting so the probing goroutine can publish its result.
	mu, ok := gitRootLocks[dir]
	if !ok {
		mu = &sync.Mutex{}
		gitRootLocks[dir] = mu
	}
	gitRootMu.Unlock()

	mu.Lock()
	defer mu.Unlock()

	// Double-check the cache now that we hold the per-directory lock.
	gitRootMu.Lock()
	if root, ok := gitRootCache[dir]; ok {
		gitRootMu.Unlock()
		return root
	}
	gitRootMu.Unlock()

	root, cacheable := gitRootProbe(dir)
	if cacheable {
		gitRootMu.Lock()
		gitRootCache[dir] = root
		gitRootMu.Unlock()
	}

	slog.Debug("git root resolved",
		"dir", dir,
		"git_root", root,
		"in_git_repo", root != "",
		"cached", cacheable,
	)
	return root
}

// ResetRepoRootCache discards the memoized git-root lookups. Tests that change
// the git boundary of a directory in place must call it; production code never
// needs to, because the git boundary of a working directory does not change
// during a single CLI invocation.
func ResetRepoRootCache() {
	gitRootMu.Lock()
	defer gitRootMu.Unlock()
	clear(gitRootCache)
	slog.Debug("repo root cache reset")
}

func FindRepoRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}

	// Determine the git repo boundary first so we never return a directory
	// that belongs to a different (parent) git repository. This prevents
	// situations like: skill-eval/ nested inside agent-marketplace/ where
	// the walk finds agent-marketplace/.agents/config.yaml and incorrectly
	// treats it as the project root for skill-eval.
	//
	// Memoized per working directory: this is the only forking step, and it
	// used to run several times per command (3x for doctor, 6x for status).
	gitRoot := gitRootFor(filepath.Clean(cwd))

	for dir := filepath.Clean(cwd); ; dir = filepath.Dir(dir) {
		// Skip the home directory: ~/.agents/config.yaml is the user-personal
		// config layer, not a project config. Without this guard FindRepoRoot
		// would return ~ when invoked from the home directory, causing
		// LoadRepoConfig to load the same file as LoadGlobalConfig.
		if !IsHomeDir(dir) {
			if fileExists(filepath.Join(dir, ".agents", "config.yaml")) {
				// Only accept this directory if it is within the current git
				// repo's boundary (or if git is not available). This prevents
				// a nested repo from adopting a parent repo's config.
				if gitRoot == "" || isWithinRoot(dir, gitRoot) {
					return dir
				}
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}

	if gitRoot != "" {
		return gitRoot
	}

	return cwd
}

// FindLocalConfigDir walks upward from cwd looking for the nearest
// .agents/config.local.yaml, stopping at (but not past) the repo root
// returned by FindRepoRoot. This enables a monorepo subdirectory to have
// its own local (gitignored) config that layers on top of the parent's
// committed config.yaml, without the subdirectory becoming the repo root.
//
// If no config.local.yaml is found between cwd and the repo root, the
// repo root itself is returned (preserving the original behavior where
// the local config lives at the repo root).
func FindLocalConfigDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	repoRoot := FindRepoRoot()

	for dir := filepath.Clean(cwd); ; dir = filepath.Dir(dir) {
		if !IsHomeDir(dir) {
			if fileExists(filepath.Join(dir, ".agents", "config.local.yaml")) {
				return dir
			}
		}
		// Stop at the repo root — don't walk past it.
		if filepath.Clean(dir) == filepath.Clean(repoRoot) {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}

	return repoRoot
}

func isWithinRoot(path, root string) bool {
	p := filepath.Clean(path)
	r := filepath.Clean(root)
	if rp, err := filepath.EvalSymlinks(p); err == nil {
		p = filepath.Clean(rp)
	}
	if rr, err := filepath.EvalSymlinks(r); err == nil {
		r = filepath.Clean(rr)
	}
	rel, err := filepath.Rel(r, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}

// DefaultHarnesses returns the default target harness list used when
// creating skeleton configs or migrating v1 configs.
func DefaultHarnesses() []string {
	return []string{"factory", "windsurf"}
}

// loadV3OrV2Config reads a Config from path, dispatching to the strict v3
// loader when the file declares apiVersion agent-marketplace/v3. The layer
// kind controls which top-level fields are allowed in v3 mode; for v2 files
// the standard validation path is used.
func loadV3OrV2Config(path string, kind layerKind, requireHarnesses bool) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}

	if PeekAPIVersion(data) == APIVersionV3 {
		cfg, errs := loadV3Config(data, kind, requireHarnesses)
		if len(errs) > 0 {
			return nil, fmt.Errorf("validate config %q: %w", path, errors.Join(errs...))
		}
		return cfg, nil
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}
	normalizeConfigHarnessTargets(&cfg)
	if err := cfg.validate(requireHarnesses, true, kind.name()); err != nil {
		if IsMigrationRequired(err) {
			slog.Warn("config needs migration", "path", path, "error", err)
		}
		return nil, fmt.Errorf("validate config %q: %w", path, err)
	}
	return &cfg, nil
}

func LoadGlobalConfig() (*Config, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}

	configPath := filepath.Join(homeDir, ".agents", "config.yaml")
	cfg, err := loadV3OrV2Config(configPath, userLayer, false)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			slog.Debug("config skipped", "layer", "user", "path", configPath, "reason", "not_found")
			return nil, nil
		}
		return nil, err
	}

	if logErrs := validateLogging(cfg.Logging); len(logErrs) > 0 {
		return nil, fmt.Errorf("validate logging config %q: %w", configPath, errors.Join(logErrs...))
	}

	slog.Debug("config loaded", "layer", "user", "path", configPath, "registries", len(cfg.Registries))
	return cfg, nil
}

// LoadUserLoggingConfig loads only the user-level logging block from
// ~/.agents/config.yaml. Repo and local configs are intentionally ignored for
// logging.
func LoadUserLoggingConfig() (*LoggingConfig, error) {
	configPath, err := GlobalConfigPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read user logging config %q: %w", configPath, err)
	}
	var cfg struct {
		Logging *LoggingConfig `yaml:"logging,omitempty"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse user logging config %q: %w", configPath, err)
	}
	if errs := validateLogging(cfg.Logging); len(errs) > 0 {
		return nil, fmt.Errorf("validate user logging config %q: %w", configPath, errors.Join(errs...))
	}
	return cfg.Logging, nil
}

// LoadConfigFromPath loads a Config from an explicit file path.
// Unlike LoadRepoConfig, it does not use FindRepoRoot.
// Harnesses is not required; project-level configs may omit it. When the path
// is a local config (config.local.yaml), apiVersion is also not required —
// these files use the LocalConfig schema and are generated without it. Editor
// helpers read every scope's config through this function, so the leniency
// here keeps remove/exclude operations working against local configs.
func LoadConfigFromPath(path string) (*Config, error) {
	if isLocalConfigPath(path) {
		cfg, err := loadConfigGated(path, false, false, localLayer.name())
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				slog.Debug("config skipped", "path", path, "reason", "not_found")
				return nil, nil
			}
			return nil, err
		}

		slog.Debug("config loaded", "path", path, "registries", len(cfg.Registries))
		return cfg, nil
	}

	var kind layerKind
	if isGlobalConfigPath(path) {
		kind = userLayer
	} else {
		kind = repoLayer
	}

	cfg, err := loadV3OrV2Config(path, kind, false)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			slog.Debug("config skipped", "path", path, "reason", "not_found")
			return nil, nil
		}
		return nil, err
	}

	if cfg != nil && kind == repoLayer && len(cfg.Harnesses) > 0 {
		return nil, fmt.Errorf("repository config at %q cannot declare top-level harnesses; the target harness set is machine-wide", path)
	}

	slog.Debug("config loaded", "path", path, "registries", len(cfg.Registries))
	return cfg, nil
}

// LoadLocalConfigFromPath loads a LocalConfig from an explicit file path.
// Unlike LoadLocalConfig, it does not use FindRepoRoot.
func LoadLocalConfigFromPath(path string) (*LocalConfig, error) {
	cfg, err := loadLocalConfig(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			slog.Debug("local config skipped", "path", path, "reason", "not_found")
			return nil, nil
		}
		return nil, err
	}
	slog.Debug("local config loaded", "path", path, "registries", len(cfg.Registries))
	return cfg, nil
}

func LoadRepoConfig() (*Config, error) {
	repoRoot := FindRepoRoot()

	// When invoked from the home directory, FindRepoRoot may return ~ (either
	// because ~/.agents/config.yaml exists, or as the cwd fallback). That path
	// is already covered by LoadGlobalConfig; loading it again as RepoCommitted
	// would duplicate every registry entry with the wrong scope.
	if IsHomeDir(repoRoot) {
		slog.Debug("config skipped", "layer", "repo", "repo_root", repoRoot, "reason", "home_dir")
		return nil, nil
	}

	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	cfg, err := loadV3OrV2Config(configPath, repoLayer, false)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			slog.Debug("config skipped", "layer", "repo", "path", configPath, "reason", "not_found")
			return nil, nil
		}
		return nil, err
	}
	if cfg != nil && len(cfg.Harnesses) > 0 {
		return nil, fmt.Errorf("repository config at %q cannot declare top-level harnesses; the target harness set is machine-wide", configPath)
	}
	slog.Debug("config loaded", "layer", "repo", "path", configPath, "registries", len(cfg.Registries))
	return cfg, nil
}

func LoadLocalConfig() (*LocalConfig, error) {
	localDir := FindLocalConfigDir()

	// Same guard as LoadRepoConfig: don't treat ~ as a project root.
	if IsHomeDir(localDir) {
		slog.Debug("local config skipped", "local_dir", localDir, "reason", "home_dir")
		return nil, nil
	}

	configPath := filepath.Join(localDir, ".agents", "config.local.yaml")
	cfg, err := loadLocalConfig(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			slog.Debug("local config skipped", "path", configPath, "reason", "not_found")
			return nil, nil
		}
		return nil, err
	}

	slog.Debug("local config loaded", "path", configPath, "registries", len(cfg.Registries))
	return cfg, nil
}

// IsHomeDir reports whether path refers to the user's home directory.
// It uses os.SameFile (inode comparison) to handle symlinks correctly,
// e.g. macOS /tmp → /private/tmp.
func IsHomeDir(path string) bool {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	pStat, err := os.Stat(path)
	if err != nil {
		return false
	}
	hStat, err := os.Stat(homeDir)
	if err != nil {
		return false
	}
	return os.SameFile(pStat, hStat)
}

func loadConfig(path string, requireHarnesses bool) (*Config, error) {
	return loadConfigGated(path, requireHarnesses, true, "")
}

// loadConfigGated reads and validates a Config, with independent control over
// whether harnesses and apiVersion are required. Local config files
// (config.local.yaml) are loaded with requireAPIVersion=false because they use
// the LocalConfig schema and are generated without an apiVersion field.
func loadConfigGated(path string, requireHarnesses, requireAPIVersion bool, layer string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}
	normalizeConfigHarnessTargets(&cfg)

	if err := cfg.validate(requireHarnesses, requireAPIVersion, layer); err != nil {
		if IsMigrationRequired(err) {
			slog.Warn("config needs migration", "path", path, "error", err)
		}
		return nil, fmt.Errorf("validate config %q: %w", path, err)
	}

	return &cfg, nil
}

func loadLocalConfig(path string) (*LocalConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}

	var cfg LocalConfig
	if len(bytes.TrimSpace(data)) == 0 {
		return &cfg, nil
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}
	normalizeLocalConfigHarnessTargets(&cfg)
	if err := cfg.ValidateBasic(); err != nil {
		return nil, fmt.Errorf("validate config %q: %w", path, err)
	}

	return &cfg, nil
}
