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

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
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

	// for other dirs can proceed in parallel. Release the cache lock before
	// acquiring the per-dir lock to avoid a lock-order inversion: the probing
	// goroutine later needs the cache lock to publish the result, while waiters
	// would otherwise hold the cache lock waiting for the per-dir lock.
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

type configIncludeLoader struct {
	kind               layerKind
	loaded             map[string]struct{}
	visiting           map[string]struct{}
	files              []*Config
	rootPath           string
	rootData           []byte
	nextFilePrecedence int
}

func newConfigIncludeLoader(kind layerKind) *configIncludeLoader {
	return &configIncludeLoader{kind: kind, loaded: map[string]struct{}{}, visiting: map[string]struct{}{}}
}

func canonicalConfigPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve config path %q: %w", path, err)
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve config path %q: %w", absolute, err)
	}
	return filepath.Clean(canonical), nil
}

func sourceForLayer(kind layerKind) Source {
	switch kind {
	case organizationLayer:
		return Organization
	case userLayer:
		return UserPersonal
	case repoLayer:
		return RepoCommitted
	case localLayer:
		return RepoPersonal
	default:
		return SourceUnset
	}
}

func (l *configIncludeLoader) collect(path string, root bool) (*Config, error) {
	canonical := l.rootPath
	var err error
	if !root || canonical == "" {
		canonical, err = canonicalConfigPath(path)
		if err != nil {
			return nil, err
		}
	}
	if _, ok := l.visiting[canonical]; ok {
		return nil, fmt.Errorf("config include cycle at %q", canonical)
	}
	if _, ok := l.loaded[canonical]; ok {
		slog.Debug("config include skipped", "path", canonical, "reason", "duplicate")
		return nil, nil
	}

	l.visiting[canonical] = struct{}{}
	defer delete(l.visiting, canonical)
	data := l.rootData
	if !root || data == nil {
		data, err = os.ReadFile(canonical)
		if err != nil {
			return nil, fmt.Errorf("read config %q: %w", canonical, err)
		}
	}
	cfg, errs := loadV3ConfigWithAPIVersion(data, l.kind, false, root && l.kind != localLayer)
	if len(errs) > 0 {
		return nil, fmt.Errorf("validate config %q: %w", canonical, errors.Join(errs...))
	}
	if logErrs := validateLogging(cfg.Logging); len(logErrs) > 0 {
		return nil, fmt.Errorf("validate logging config %q: %w", canonical, errors.Join(logErrs...))
	}
	for _, include := range cfg.Include {
		if strings.TrimSpace(include) == "" {
			return nil, fmt.Errorf("validate config %q: include path must not be empty", canonical)
		}
		includePath := include
		if !filepath.IsAbs(includePath) {
			includePath = filepath.Join(filepath.Dir(canonical), includePath)
		}
		slog.Debug("loading config include", "owner", canonical, "path", includePath)
		if _, err := l.collect(includePath, false); err != nil {
			slog.Error("config include failed", "owner", canonical, "path", includePath, "error", err)
			return nil, fmt.Errorf("load include %q referenced by %q: %w", includePath, canonical, err)
		}
	}
	if !root || len(cfg.Include) > 0 {
		l.nextFilePrecedence++
		for i := range cfg.Registries {
			cfg.Registries[i].FilePrecedence = l.nextFilePrecedence
		}
	}
	l.files = append(l.files, cfg)
	l.loaded[canonical] = struct{}{}
	return cfg, nil
}

func mergeIncludeSelector(dst *KindSelector, src KindSelector) {
	dst.Exclude = removeSelectorNames(dst.Exclude, src.Include)
	dst.Include = removeSelectorNames(dst.Include, src.Include)
	dst.Include = appendSelectorNames(dst.Include, src.Include)
	dst.Include = removeSelectorNames(dst.Include, src.Exclude)
	dst.Exclude = removeSelectorNames(dst.Exclude, src.Exclude)
	dst.Exclude = appendSelectorNames(dst.Exclude, src.Exclude)
	dst.ExcludeAll = dst.ExcludeAll || src.ExcludeAll
}

func removeSelectorNames(values, removed []string) []string {
	if len(values) == 0 || len(removed) == 0 {
		return values
	}
	set := make(map[string]struct{}, len(removed))
	for _, value := range removed {
		set[strings.TrimSpace(value)] = struct{}{}
	}
	filtered := values[:0]
	for _, value := range values {
		if _, drop := set[strings.TrimSpace(value)]; !drop {
			filtered = append(filtered, value)
		}
	}
	return filtered
}

func appendSelectorNames(dst, src []string) []string {
	if len(src) == 0 {
		return dst
	}
	seen := make(map[string]struct{}, len(dst)+len(src))
	for _, value := range dst {
		seen[strings.TrimSpace(value)] = struct{}{}
	}
	for _, value := range src {
		key := strings.TrimSpace(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		dst = append(dst, value)
	}
	return dst
}

func mergeLocalIncludeLayers(layers []Layer) *Config {
	merged := &Config{}
	for _, layer := range layers {
		cfg := layer.Config
		if cfg == nil {
			continue
		}
		merged.Registries = append(merged.Registries, cfg.Registries...)
		for _, kind := range AllKinds {
			mergeIncludeSelector(merged.SelectorForKindPtr(kind), cfg.SelectorForKind(kind))
		}
		if cfg.APIVersion != "" {
			merged.APIVersion = cfg.APIVersion
		}
		if cfg.Materialization != nil {
			merged.Materialization = &MaterializationConfig{Mode: cfg.Materialization.Mode}
		}
	}
	return merged
}

func loadConfigTree(path string, kind layerKind) (*Config, error) {
	return loadCollectedConfigTree(path, newConfigIncludeLoader(kind))
}

func loadConfigTreeFromRootData(canonicalPath string, data []byte, kind layerKind) (*Config, error) {
	loader := newConfigIncludeLoader(kind)
	loader.rootPath = canonicalPath
	loader.rootData = data
	return loadCollectedConfigTree(canonicalPath, loader)
}

func configWithCascadingDefaults(files []*Config, index int) *Config {
	copyCfg := *files[index]
	for i := index + 1; i < len(files); i++ {
		later := files[i]
		if later.Tags != nil {
			copyCfg.Tags = later.Tags
		}
		for _, kind := range AllKinds {
			if kind == ir.KindCollection {
				continue
			}
			dst := copyCfg.SelectorForKindPtr(kind)
			src := later.SelectorForKind(kind)
			if src.Include != nil {
				dst.Include = src.Include
			}
			if src.Exclude != nil {
				dst.Exclude = src.Exclude
			}
			dst.ExcludeAll = dst.ExcludeAll || src.ExcludeAll
		}
	}
	return &copyCfg
}

func loadCollectedConfigTree(path string, loader *configIncludeLoader) (*Config, error) {
	kind := loader.kind
	root, err := loader.collect(path, true)
	if err != nil {
		return nil, err
	}
	if len(loader.files) == 1 && len(root.Include) == 0 {
		return root, nil
	}
	layers := make([]Layer, 0, len(loader.files))
	for i, cfg := range loader.files {
		if kind != localLayer {
			cfg = configWithCascadingDefaults(loader.files, i)
		}
		layers = append(layers, Layer{Source: sourceForLayer(kind), Config: cfg})
	}
	var merged *Config
	if kind == localLayer {
		merged = mergeLocalIncludeLayers(layers)
	} else {
		merged = MergeLayers(layers)
		merged.Collections = KindSelector{}
		for _, cfg := range loader.files {
			mergeIncludeSelector(&merged.Collections, cfg.Collections)
			if cfg.Telemetry != nil {
				telemetry := *cfg.Telemetry
				merged.Telemetry = &telemetry
			}
		}
	}
	merged.Include = append(IncludePaths(nil), root.Include...)
	return merged, nil
}

func rootConfigNotExist(path string, err error) bool {
	if !errors.Is(err, os.ErrNotExist) {
		return false
	}
	_, statErr := os.Stat(path)
	return errors.Is(statErr, os.ErrNotExist)
}

func configAsLocal(cfg *Config) *LocalConfig {
	if cfg == nil {
		return nil
	}
	return &LocalConfig{
		Include:         cfg.Include,
		APIVersion:      cfg.APIVersion,
		Registries:      cfg.Registries,
		Skills:          cfg.Skills,
		Commands:        cfg.Commands,
		Subagents:       cfg.Subagents,
		Hooks:           cfg.Hooks,
		MCP:             cfg.MCP,
		Collections:     cfg.Collections,
		Materialization: cfg.Materialization,
	}
}

// loadV3OrV2Config reads a Config from path using the strict layer-aware
// loader. v2 is the canonical schema and v3 is accepted as a compatibility
// alias; neither version is rewritten while loading.
func loadV3OrV2Config(path string, kind layerKind, requireHarnesses bool) (*Config, error) {
	cfg, err := loadConfigTree(path, kind)
	if err != nil {
		return nil, err
	}
	if requireHarnesses && len(cfg.Harnesses) == 0 {
		return nil, fmt.Errorf("validate config %q: harnesses is required", path)
	}
	return cfg, nil
}

func LoadGlobalConfig() (*Config, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}

	configPath := filepath.Join(homeDir, ".agents", "config.yaml")
	cfg, err := loadV3OrV2Config(configPath, userLayer, false)
	if err != nil {
		if rootConfigNotExist(configPath, err) {
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
	cfg, err := loadConfigTree(configPath, userLayer)
	if err != nil {
		if rootConfigNotExist(configPath, err) {
			return nil, nil
		}
		return nil, fmt.Errorf("load user logging config %q: %w", configPath, err)
	}
	return cfg.Logging, nil
}

func LoadRootConfigFromPath(path string) (*Config, error) {
	kind := repoLayer
	requireAPIVersion := true
	if isLocalConfigPath(path) {
		kind = localLayer
		requireAPIVersion = false
	} else if isGlobalConfigPath(path) {
		kind = userLayer
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}
	cfg, errs := loadV3ConfigWithAPIVersion(data, kind, false, requireAPIVersion)
	if len(errs) > 0 {
		return nil, fmt.Errorf("validate config %q: %w", path, errors.Join(errs...))
	}
	if logErrs := validateLogging(cfg.Logging); len(logErrs) > 0 {
		return nil, fmt.Errorf("validate logging config %q: %w", path, errors.Join(logErrs...))
	}
	return cfg, nil
}

// LoadConfigFromPath loads an explicit config and its includes.
// Harnesses is optional. Local configs may also omit apiVersion.
func LoadConfigFromPath(path string) (*Config, error) {
	if isLocalConfigPath(path) {
		cfg, err := loadConfigTree(path, localLayer)
		if err != nil {
			if rootConfigNotExist(path, err) {
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
		if rootConfigNotExist(path, err) {
			slog.Debug("config skipped", "path", path, "reason", "not_found")
			return nil, nil
		}
		return nil, err
	}

	slog.Debug("config loaded", "path", path, "registries", len(cfg.Registries))
	return cfg, nil
}

// LoadLocalConfigFromPath loads a LocalConfig from an explicit file path.
// Unlike LoadLocalConfig, it does not use FindRepoRoot.
func LoadLocalConfigFromPath(path string) (*LocalConfig, error) {
	loaded, err := loadConfigTree(path, localLayer)
	cfg := configAsLocal(loaded)
	if err != nil {
		if rootConfigNotExist(path, err) {
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
		if rootConfigNotExist(configPath, err) {
			slog.Debug("config skipped", "layer", "repo", "path", configPath, "reason", "not_found")
			return nil, nil
		}
		return nil, err
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
	loaded, err := loadConfigTree(configPath, localLayer)
	cfg := configAsLocal(loaded)
	if err != nil {
		if rootConfigNotExist(configPath, err) {
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
	layer := "repository committed"
	if requireHarnesses {
		layer = "user personal"
	}
	return loadConfigGated(path, requireHarnesses, true, layer)
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
