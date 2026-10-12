package config

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
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

func nearestGitMarker(dir string) (string, error) {
	for current := filepath.Clean(dir); ; current = filepath.Dir(current) {
		if _, err := os.Lstat(filepath.Join(current, ".git")); err == nil {
			return current, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		if filepath.Dir(current) == current {
			return "", nil
		}
	}
}

func probeGitRoot(dir string) (string, bool) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return "", true
	}
	markerRoot, markerErr := nearestGitMarker(dir)
	if markerErr != nil {
		slog.Warn("git root probe failed", "dir", dir, "reason", "git_marker", "error", markerErr)
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	// Run in the directory we are keying the cache on rather than relying on
	// the process working directory, so the memo key is the actual input.
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if ctx.Err() == nil && errors.As(err, &exitErr) && exitErr.ExitCode() == 128 && markerRoot == "" {
			message := strings.TrimSpace(string(exitErr.Stderr))
			noRepository := message == "fatal: not a git repository (or any of the parent directories): .git" || strings.HasPrefix(message, "fatal: not a git repository (or any parent up to mount point ") && strings.HasSuffix(message, "\nStopping at filesystem boundary (GIT_DISCOVERY_ACROSS_FILESYSTEM not set).")
			if noRepository {
				// git ran and reported "not a repository": a real, cacheable answer.
				return "", true
			}
		}
		slog.Warn("git root probe failed", "dir", dir, "error", err)
		return "", false
	}
	r := strings.TrimSpace(string(out))
	if !filepath.IsAbs(r) || !isWithinRoot(dir, r) || markerRoot != "" && !isWithinRoot(r, markerRoot) {
		slog.Warn("git root probe failed", "dir", dir, "reason", "invalid_boundary")
		return "", false
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
// sync.Map is used so the fast path never blocks a slow-path probe and so the
// per-directory lock can be acquired without holding the cache lock, avoiding
// any lock-ordering inversion.
var (
	gitRootCache sync.Map
	gitRootLocks sync.Map
)

// gitRootFor returns the git worktree root containing dir, resolving it at
// most once per directory per process for successful probes.
func gitRootFor(dir string) string {
	// Fast path: read the cache.
	if root, ok := gitRootCache.Load(dir); ok {
		return root.(string)
	}

	// Slow path: obtain the per-directory lock so only one goroutine probes
	// for this directory. Other goroutines for the same dir block here; callers
	// for other dirs can proceed in parallel.
	actual, _ := gitRootLocks.LoadOrStore(dir, &sync.Mutex{})
	mu := actual.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	// Double-check the cache now that we hold the per-directory lock.
	if root, ok := gitRootCache.Load(dir); ok {
		return root.(string)
	}

	root, cacheable := gitRootProbe(dir)
	if cacheable {
		gitRootCache.Store(dir, root)
	}

	slog.Debug("git root resolved",
		"dir", dir,
		"git_root", root,
		"in_git_repo", root != "",
		"cached", cacheable,
	)
	if !cacheable {
		slog.Warn("git boundary unavailable; limiting repository discovery to current directory", "dir", dir)
		return dir
	}
	return root
}

// ResetRepoRootCache discards the memoized git-root lookups. Tests that change
// the git boundary of a directory in place must call it; production code never
// needs to, because the git boundary of a working directory does not change
// during a single CLI invocation.
func ResetRepoRootCache() {
	gitRootCache.Clear()
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
				// repo's boundary (or if the probe confirmed no Git repository). This prevents
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
	organizationPath   string
	rootData           []byte
	nextFilePrecedence int
	reads              []ConfigRead
	readByPath         map[string]ConfigRead
	overlays           map[string][]byte
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

// canonicalConfigPathTolerateMissing canonicalizes path even when the file,
// or some of its directories, do not exist yet. It resolves the deepest
// existing ancestor and appends the missing components, which yields the same
// name canonicalConfigPath produces once the file exists. That keeps staged
// edits, provenance, and lock-path comparisons stable across the creating edit.
// A dangling symlink on the path has no such stable name, so it is refused
// with an error that still reports the file as not existing.
func canonicalConfigPathTolerateMissing(path string) (string, error) {
	current, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve config path %q: %w", path, err)
	}
	var missing []string
	for {
		canonical, err := filepath.EvalSymlinks(current)
		if err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				canonical = filepath.Join(canonical, missing[i])
			}
			return filepath.Clean(canonical), nil
		}
		parent := filepath.Dir(current)
		if !errors.Is(err, fs.ErrNotExist) || parent == current {
			return "", fmt.Errorf("resolve config path %q: %w", path, err)
		}
		if _, lstatErr := os.Lstat(current); lstatErr == nil {
			return "", fmt.Errorf("resolve config path %q: %s is a symlink to a missing target: %w", path, current, err)
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

// ConfigLockPaths identifies one config and its layer-owned lock through both
// the requested and canonical filesystem names.
type ConfigLockPaths struct {
	RequestedConfig string
	CanonicalConfig string
	RequestedLock   string
	CanonicalLock   string
}

// ResolveConfigLockPathsTolerateMissing is ResolveConfigLockPaths for a config
// that may not exist yet, such as a layer root an edit is about to create.
func ResolveConfigLockPathsTolerateMissing(path string) (ConfigLockPaths, error) {
	if absolute, err := filepath.Abs(path); err == nil && dependencytxn.ClassifyDestination(absolute) != dependencytxn.AuthorityLegacy {
		return ResolveConfigLockPaths(absolute)
	}
	canonical, err := canonicalConfigPathTolerateMissing(path)
	if err != nil {
		return ConfigLockPaths{}, err
	}
	return configLockPaths(path, canonical)
}

// ResolveConfigLockPaths resolves path before deriving its layer-owned lock.
func ResolveConfigLockPaths(path string) (ConfigLockPaths, error) {
	requested, err := filepath.Abs(path)
	if err != nil {
		return ConfigLockPaths{}, fmt.Errorf("resolve config path %q: %w", path, err)
	}
	var canonical string
	if dependencytxn.ClassifyDestination(requested) != dependencytxn.AuthorityLegacy {
		// The lock belongs to the logical layer, not an approved config
		// link's target (which can be outside the .agents directory).
		canonical, err = dependencytxn.CanonicalConfigDestination(filepath.Clean(requested))
	} else {
		canonical, err = canonicalConfigPath(path)
	}
	if err != nil {
		return ConfigLockPaths{}, err
	}
	return configLockPaths(path, canonical)
}

func configLockPaths(path, canonical string) (ConfigLockPaths, error) {
	requested, err := filepath.Abs(path)
	if err != nil {
		return ConfigLockPaths{}, fmt.Errorf("resolve config path %q: %w", path, err)
	}
	requested = filepath.Clean(requested)
	canonical = filepath.Clean(canonical)
	requestedLock, err := lockfile.PathForConfig(requested)
	if err != nil {
		return ConfigLockPaths{}, err
	}
	canonicalLock, err := lockfile.PathForConfig(canonical)
	if err != nil {
		return ConfigLockPaths{}, err
	}
	return ConfigLockPaths{
		RequestedConfig: requested,
		CanonicalConfig: canonical,
		RequestedLock:   requestedLock,
		CanonicalLock:   canonicalLock,
	}, nil
}

func readDeployedLock(path string, scope lockfile.Scope, overlays map[string][]byte) (*lockfile.Document, *ConfigRead, error) {
	paths, err := ResolveConfigLockPathsTolerateMissing(path)
	if err != nil {
		return nil, nil, err
	}
	ev, decode, err := dependencytxn.ReadLayerLock(paths.CanonicalLock, paths.RequestedLock, scope, overlays)
	if err != nil {
		var coded *lockfile.Error
		if decode && errors.As(err, &coded) && coded.Code == lockfile.CodeScopeMismatch {
			return nil, nil, v4err(v4MarketplaceLockMismatchCode, "", 0, "%s", coded.Err)
		}
		op := "read"
		if decode {
			op = "decode"
		}
		return nil, nil, fmt.Errorf("%s lock %q for config %q: %w", op, paths.CanonicalLock, path, err)
	}
	if ev.Digest == "" {
		return nil, nil, nil
	}
	return &ev.Document, &ConfigRead{
		Path:            paths.CanonicalLock,
		RequestedPath:   paths.RequestedLock,
		Digest:          ev.Digest,
		Overlay:         ev.Overlay,
		AcquisitionLock: true,
	}, nil
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
	if l.kind == organizationLayer {
		if !filepath.IsAbs(path) {
			wd, err := os.Getwd()
			if err != nil {
				return nil, fmt.Errorf("resolve organization config path %q: %w", path, err)
			}
			path = wd + string(filepath.Separator) + path
		}
		if root {
			l.organizationPath = path
		} else if err := validateOrganizationInclude(path, l.organizationPath); err != nil {
			return nil, err
		}
	}
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
		if l.readByPath != nil {
			read := l.readByPath[canonical]
			read.RequestedPath, err = filepath.Abs(path)
			if err != nil {
				return nil, err
			}
			l.reads = append(l.reads, read)
		}
		slog.Debug("config include skipped", "path", canonical, "reason", "duplicate")
		return nil, nil
	}

	l.visiting[canonical] = struct{}{}
	defer delete(l.visiting, canonical)
	data := l.rootData
	overlay, staged := l.overlays[canonical]
	if !staged {
		overlay, staged = l.overlays[path]
	}
	if staged {
		data, err = boundedConfigData(overlay)
	} else if !root || data == nil {
		data, err = readConfigFile(canonical)
	}
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", canonical, err)
	}
	if l.readByPath != nil {
		requested, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		read := ConfigRead{Path: canonical, RequestedPath: requested, Digest: fmt.Sprintf("%x", sha256.Sum256(data)), Overlay: staged}
		l.reads = append(l.reads, read)
		l.readByPath[canonical] = read
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
			if l.kind == organizationLayer {
				includePath = lexicalConfigParent(path) + string(filepath.Separator) + includePath
			} else {
				includePath = filepath.Join(filepath.Dir(canonical), includePath)
			}
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
			merged.Materialization = mergeMaterializationConfig(merged.Materialization, cfg.Materialization)
		}
	}
	return merged
}

func loadIntentConfig(path string, kind layerKind, overlays map[string][]byte) (*Config, error) {
	data, err := readConfigFile(path)
	if err != nil {
		return nil, err
	}
	li, err := readLayerIntent(path, "", data, kind, overlays)
	if err != nil {
		return nil, err
	}
	doc, err := li.deployedLock(overlays)
	if err != nil {
		return nil, err
	}
	return li.bind(doc)
}

func loadConfigTreeFromRootData(canonicalPath string, data []byte, kind layerKind) (*Config, error) {
	cfg, _, err := loadConfigTreeFromRootDataWithFiles(canonicalPath, data, kind)
	return cfg, err
}

func loadConfigTreeFromRootDataWithFiles(canonicalPath string, data []byte, kind layerKind) (*Config, []*Config, error) {
	loader := newConfigIncludeLoader(kind)
	loader.rootPath = canonicalPath
	loader.rootData = data
	cfg, err := loadCollectedConfigTree(canonicalPath, loader)
	if err != nil {
		return nil, nil, err
	}
	return cfg, append([]*Config(nil), loader.files...), nil
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
		Marketplaces:    cfg.Marketplaces,
		Skills:          cfg.Skills,
		Commands:        cfg.Commands,
		Subagents:       cfg.Subagents,
		Hooks:           cfg.Hooks,
		MCP:             cfg.MCP,
		Collections:     cfg.Collections,
		Materialization: cfg.Materialization,
		Requires:        cfg.Requires,
	}
}

func LoadGlobalConfig() (*Config, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}

	configPath := filepath.Join(homeDir, ".agents", "config.yaml")
	cfg, err := loadIntentConfig(configPath, userLayer, nil)
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
	settings, err := loadSettings(configPath, userLayer)
	if err != nil {
		return nil, fmt.Errorf("load user logging config %q: %w", configPath, err)
	}
	return settings.Logging, nil
}

func LoadRootConfigFromPath(path string) (*Config, error) {
	kind := repoLayer
	if isLocalConfigPath(path) {
		kind = localLayer
	} else if isGlobalConfigPath(path) {
		kind = userLayer
	}
	data, err := readConfigFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}
	li, err := readRootLayerIntent(path, data, kind)
	if err != nil {
		return nil, err
	}
	doc, err := li.deployedLock(nil)
	if err != nil {
		return nil, err
	}
	return li.bind(doc)
}

// LoadConfigFromPath loads an explicit config and its includes.
// Harnesses is optional. Local configs may also omit apiVersion.
func LoadConfigFromPath(path string) (*Config, error) {
	if isLocalConfigPath(path) {
		cfg, err := loadIntentConfig(path, localLayer, nil)
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

	cfg, err := loadIntentConfig(path, kind, nil)
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
	loaded, err := loadIntentConfig(path, localLayer, nil)
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
	cfg, err := loadIntentConfig(configPath, repoLayer, nil)
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
	loaded, err := loadIntentConfig(configPath, localLayer, nil)
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
