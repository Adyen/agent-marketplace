package config

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"runtime"
	"testing"
)

const defaultOrganizationConfigPath = "/etc/agent-marketplace/config.yaml"

// testOrganizationConfigPath is a test-only override for the externally
// managed organization config path. Production code always uses
// defaultOrganizationConfigPath.
var testOrganizationConfigPath string

// SetTestV3OrganizationConfigPath overrides the organization config path for
// the duration of a test. The name is retained for test compatibility. It
// panics if called outside a test binary.
// Pass nil as the testing.TB when calling from TestMain.
func SetTestV3OrganizationConfigPath(t testing.TB, path string) {
	if !testing.Testing() {
		panic("SetTestV3OrganizationConfigPath: not in tests")
	}
	old := testOrganizationConfigPath
	testOrganizationConfigPath = path
	if t != nil {
		t.Cleanup(func() { testOrganizationConfigPath = old })
	}
}

// OrganizationConfigPath returns the fixed filesystem path of the externally
// managed organization configuration.
func OrganizationConfigPath() string {
	return organizationConfigPath("")
}

// organizationConfigPath returns the fixed organization config path, honoring
// the explicit override and the test-only package-level override.
func organizationConfigPath(override string) string {
	if override != "" {
		return override
	}
	if testOrganizationConfigPath != "" {
		return testOrganizationConfigPath
	}
	return defaultOrganizationConfigPath
}

// LayerResult carries one Configuration Layer's provenance, parsed config, and
// any validation errors encountered while loading it.
type LayerResult struct {
	Source Source
	Path   string
	Config *Config
	Local  *LocalConfig
	Errors []error
}

// EffectiveConfig is the result of loading and merging all applicable
// Configuration Layers. It exposes provenance, validation failures, the merged
// effective config, and the resolved registry allowlist. No registry discovery
// or network access occurs during LoadEffective.
type EffectiveConfig struct {
	Organization LayerResult
	User         LayerResult
	Repo         LayerResult
	LocalLayer   LayerResult
	Local        *LocalConfig
	Merged       *Config
	// Allowed is the resolved registry allowlist. It is nil when no layer
	// declares an allowlist (permitting all remote origins), empty when a
	// layer explicitly sets an empty allowlist (denying all remote origins),
	// and non-empty when an allowlist is configured.
	Allowed             []string
	TargetHarnesses     []string
	TargetHarnessSource Source
	Errors              []error
	Warnings            []error
	Managed             bool
	// Diagnostics is the filesystem and trust state of the externally managed
	// organization config at the fixed path.
	Diagnostics ManagedInstallationDiagnostics
}

// LoadEffectiveOptions overrides the default layer paths for tests and
// non-standard layouts. Empty values use the standard resolution rules.
type LoadEffectiveOptions struct {
	RepoRoot                     string
	LocalConfigDir               string
	OrganizationPath             string
	GlobalPath                   string
	RepoPath                     string
	LocalPath                    string
	AllowMissingOrganizationLock bool
	FileOverlays                 map[string][]byte
}

func (ec EffectiveConfig) LayerResults() []LayerResult {
	return []LayerResult{ec.Organization, ec.User, ec.Repo, ec.LocalLayer}
}

func (ec EffectiveConfig) PresentLayers() []LayerResult {
	all := ec.LayerResults()
	present := make([]LayerResult, 0, len(all))
	for _, layer := range all {
		if layer.Config != nil || layer.Local != nil {
			present = append(present, layer)
		}
	}
	return present
}

// LoadEffective loads every applicable Configuration Layer, validates each
// according to its authority and schema version, merges the layers, and
// validates the resulting registry URLs against the resolved allowlist.
//
// The returned EffectiveConfig always contains every layer that could be
// loaded; callers must check Errors before proceeding to registry/network
// operations.
func LoadEffective(ctx context.Context, opts LoadEffectiveOptions) EffectiveConfig {
	if err := ctx.Err(); err != nil {
		return EffectiveConfig{Errors: []error{err}}
	}

	slog.Info("loading effective config")

	repoRoot := opts.RepoRoot
	if repoRoot == "" {
		repoRoot = FindRepoRoot()
	}
	localDir := opts.LocalConfigDir
	if localDir == "" {
		localDir = FindLocalConfigDir()
	}

	ec := EffectiveConfig{}

	orgPath := organizationConfigPath(opts.OrganizationPath)
	ec.Organization, ec.Diagnostics = loadEffectiveOrganization(orgPath, opts.AllowMissingOrganizationLock)
	ec.Managed = ec.Diagnostics.Managed()
	ec.Warnings = append(ec.Warnings, ec.Diagnostics.Warnings...)
	LogManagedDiagnostics(ec.Diagnostics)

	ec.User = loadEffectiveUser(opts.GlobalPath, opts.FileOverlays)
	ec.Repo = loadEffectiveRepo(repoRoot, opts.RepoPath, opts.FileOverlays)
	ec.LocalLayer = loadEffectiveLocal(localDir, opts.LocalPath, opts.FileOverlays)
	ec.Local = ec.LocalLayer.Local

	ec.Errors = append(ec.Errors, ec.Organization.Errors...)
	ec.Errors = append(ec.Errors, ec.User.Errors...)
	ec.Errors = append(ec.Errors, ec.Repo.Errors...)
	ec.Errors = append(ec.Errors, ec.LocalLayer.Errors...)

	for _, err := range ec.Errors {
		slog.Error("effective config layer error", "error", err)
	}

	// Resolve the target harness set. Project-level harnesses take precedence
	// for project operations; user-personal harnesses apply when the project
	// does not specify its own, and organization harnesses remain the fallback.
	if ec.Repo.Config != nil && len(ec.Repo.Config.Harnesses) > 0 {
		ec.TargetHarnesses = append([]string(nil), ec.Repo.Config.Harnesses...)
		ec.TargetHarnessSource = RepoCommitted
	} else if ec.User.Config != nil && len(ec.User.Config.Harnesses) > 0 {
		ec.TargetHarnesses = append([]string(nil), ec.User.Config.Harnesses...)
		ec.TargetHarnessSource = UserPersonal
	} else if ec.Organization.Config != nil && len(ec.Organization.Config.Harnesses) > 0 {
		ec.TargetHarnesses = append([]string(nil), ec.Organization.Config.Harnesses...)
		ec.TargetHarnessSource = ec.Organization.Source
	}

	slog.Info("resolved target harness set",
		"harnesses", ec.TargetHarnesses,
		"source", ec.TargetHarnessSource,
	)

	layers := make([]Layer, 0, 4)
	if ec.Organization.Config != nil {
		layers = append(layers, Layer{Source: Organization, Config: ec.Organization.Config})
	}
	if ec.User.Config != nil {
		layers = append(layers, Layer{Source: UserPersonal, Config: ec.User.Config})
	}
	if ec.Repo.Config != nil {
		layers = append(layers, Layer{Source: RepoCommitted, Config: ec.Repo.Config})
	}
	if l := LocalConfigAsLayer(ec.Local); l != nil {
		layers = append(layers, *l)
	}

	merged := MergeLayers(layers)
	ec.Merged = merged

	// Resolve the allowlist from the merged file layers. A nil pointer means
	// no layer declared an allowlist, which permits all remote origins. A
	// non-nil empty slice means an explicit empty allowlist, which denies all
	// remote origins.
	var allowed []string
	if merged.AllowedRegistries != nil {
		allowed = *merged.AllowedRegistries
	}
	ec.Allowed = allowed

	if err := ValidateRegistryURLs(merged.Registries, allowed); err != nil {
		slog.Error("registry allowlist rejected one or more URLs", "error", err)
		ec.Errors = append(ec.Errors, err)
	}

	if len(ec.Errors) > 0 {
		slog.Error("effective config loaded with errors",
			"error_count", len(ec.Errors),
			"warning_count", len(ec.Warnings),
		)
	} else {
		slog.Info("effective config loaded successfully",
			"registry_count", len(merged.Registries),
			"warning_count", len(ec.Warnings),
		)
	}

	return ec
}

func globalConfigPath() (string, error) {
	p, err := GlobalConfigPath()
	if err != nil {
		return "", fmt.Errorf("resolve user config path: %w", err)
	}
	return p, nil
}

// loadEffectiveOrganization reads the externally managed organization config
// at path. It accepts the canonical v2 schema and the v3 compatibility alias,
// loading both with strict per-layer rules. Windows is always treated as
// unmanaged. A missing file is valid (unmanaged). Any other read error is a
// hard error.
func loadEffectiveOrganization(path string, allowMissingLock bool) (LayerResult, ManagedInstallationDiagnostics) {
	if runtime.GOOS == "windows" {
		lr := LayerResult{Source: SourceUnset, Path: path}
		return lr, inspectManagedConfig(path, fs.ErrNotExist, nil, SourceUnset, nil)
	}

	if _, err := os.Lstat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			lr := LayerResult{Source: SourceUnset, Path: path}
			return lr, inspectManagedConfig(path, err, nil, SourceUnset, nil)
		}
		lr := LayerResult{Source: Organization, Path: path, Errors: []error{fmt.Errorf("inspect organization config %q: %w", path, err)}}
		return lr, inspectManagedConfig(path, err, nil, Organization, lr.Errors)
	}

	if trustErr := ValidateOrganizationConfigAuthority(path); trustErr != nil {
		lr := LayerResult{Source: Organization, Path: path, Errors: []error{trustErr}}
		d := inspectManagedConfig(path, nil, nil, Organization, lr.Errors)
		d.Validation = "untrusted"
		d.Warnings = append(d.Warnings, trustErr)
		return lr, d
	}

	data, readErr := os.ReadFile(path)
	if readErr != nil {
		lr := LayerResult{Source: Organization, Path: path, Errors: []error{fmt.Errorf("read organization config %q: %w", path, readErr)}}
		return lr, inspectManagedConfig(path, readErr, nil, Organization, lr.Errors)
	}
	lr := loadOrganizationConfig(path, data)
	d := inspectManagedConfig(path, nil, lr.Config, lr.Source, lr.Errors)
	if len(lr.Errors) == 0 && lr.Config != nil && organizationHasRemoteRequirements(lr.Config) {
		if trustErr := ValidateOrganizationTrustUnit(path, true, allowMissingLock); trustErr != nil {
			lr.Errors = append(lr.Errors, trustErr)
			lr.Config = nil
			d.State = ManagedInstallationUnmanaged
			d.Validation = "blocked"
			d.Warnings = append(d.Warnings, trustErr)
		}
	}
	return lr, d
}

// LoadOrganizationConfig reads the externally managed organization config at
// the default path. It returns nil when the file is missing. It returns an
// error when the file is present but cannot be read or declares an unsupported
// apiVersion.
func LoadOrganizationConfig() (*Config, error) {
	path := organizationConfigPath("")
	lr, _ := loadEffectiveOrganization(path, false)
	if len(lr.Errors) > 0 {
		return nil, errors.Join(lr.Errors...)
	}
	return lr.Config, nil
}

func organizationHasRemoteRequirements(cfg *Config) bool {
	for _, entry := range cfg.Registries {
		if !entry.IsLocal() {
			return true
		}
	}
	return len(cfg.Marketplaces) > 0
}

func loadOrganizationConfig(path string, data []byte) LayerResult {
	cfg, errs := loadV3Config(data, organizationLayer, false)
	if cfg != nil {
		if logErrs := validateLogging(cfg.Logging); len(logErrs) > 0 {
			errs = append(errs, logErrs...)
		}
	}
	return LayerResult{Source: Organization, Path: path, Config: cfg, Errors: errs}
}

func loadEffectiveUser(override string, overlays map[string][]byte) LayerResult {
	path := override
	if path == "" {
		var err error
		path, err = globalConfigPath()
		if err != nil {
			return LayerResult{Source: UserPersonal, Errors: []error{err}}
		}
	}

	data, readErr := readEffectiveLayer(path, overlays)
	if readErr != nil {
		if errors.Is(readErr, os.ErrNotExist) {
			return LayerResult{Source: UserPersonal, Path: path}
		}
		return LayerResult{Source: UserPersonal, Path: path, Errors: []error{err}}
	}
	return LayerResult{Source: UserPersonal, Path: path, Config: cfg}
}

func loadEffectiveRepo(repoRoot, override string, overlays map[string][]byte) LayerResult {
	path := override
	if path == "" {
		path = ProjectConfigPath(repoRoot)
	}
	if IsHomeDir(repoRoot) {
		return LayerResult{Source: RepoCommitted, Path: path}
	}

	data, readErr := readEffectiveLayer(path, overlays)
	if readErr != nil {
		if errors.Is(readErr, os.ErrNotExist) {
			return LayerResult{Source: RepoCommitted, Path: path}
		}
		return LayerResult{Source: RepoCommitted, Path: path, Errors: []error{err}}
	}
	return LayerResult{Source: RepoCommitted, Path: path, Config: cfg}
}

func loadEffectiveLocal(localDir, override string, overlays map[string][]byte) LayerResult {
	path := override
	if path == "" {
		path = LocalConfigPath(localDir)
	}
	if IsHomeDir(localDir) {
		return LayerResult{Source: RepoPersonal, Path: path}
	}

	data, readErr := readEffectiveLayer(path, overlays)
	if readErr != nil {
		if errors.Is(readErr, os.ErrNotExist) {
			return LayerResult{Source: RepoPersonal, Path: path}
		}
		return LayerResult{Source: RepoPersonal, Path: path, Errors: []error{fmt.Errorf("read local config %q: %w", path, readErr)}}
	}

	cfg, errs := loadV3LocalConfig(data)
	return LayerResult{Source: RepoPersonal, Path: path, Local: cfg, Errors: errs}
}

func readEffectiveLayer(path string, overlays map[string][]byte) ([]byte, error) {
	if data, ok := overlays[path]; ok {
		return append([]byte(nil), data...), nil
	}
	current := filepath.Clean(path)
	missing := []string{}
	for {
		canonical, err := filepath.EvalSymlinks(current)
		if err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				canonical = filepath.Join(canonical, missing[i])
			}
			if data, ok := overlays[canonical]; ok {
				return append([]byte(nil), data...), nil
			}
			break
		}
		parent := filepath.Dir(current)
		if !errors.Is(err, os.ErrNotExist) || parent == current {
			break
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
	return os.ReadFile(path)
}
