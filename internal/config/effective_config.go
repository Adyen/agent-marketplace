package config

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

const defaultOrganizationConfigPath = "/etc/agent-marketplace/config.yaml"

// testOrganizationConfigPath is a test-only override for the externally
// managed organization config path. Production code always uses
// defaultOrganizationConfigPath.
var testOrganizationConfigPath string

// SetTestOrganizationConfigPath overrides the organization config path
// for the duration of a test. It panics if called outside a test binary.
// Pass nil as the testing.TB when calling from TestMain.
func SetTestOrganizationConfigPath(t testing.TB, path string) {
	if !testing.Testing() {
		panic("SetTestOrganizationConfigPath: not in tests")
	}
	old := testOrganizationConfigPath
	testOrganizationConfigPath = path
	if t != nil {
		t.Cleanup(func() { testOrganizationConfigPath = old })
	}
}

// SetTestV3OrganizationConfigPath retains compatibility with existing tests.
func SetTestV3OrganizationConfigPath(t testing.TB, path string) {
	SetTestOrganizationConfigPath(t, path)
}

// OrganizationConfigPath returns the fixed filesystem path of the externally
// managed organization configuration.
func OrganizationConfigPath() string {
	return organizationConfigPath("")
}

// unsetTestOrganizationConfigPath stands in for the organization layer when a
// test binary never chose a path. It names a file no test creates, so the
// layer reads as absent instead of as whatever the machine happens to have.
var unsetTestOrganizationConfigPath = filepath.Join(os.TempDir(), "agent-marketplace-tests-must-not-read-the-real-organization-config.yaml")

// organizationConfigPath returns the fixed organization config path, honoring
// the explicit override and the test-only package-level override.
//
// Under `go test` an unset override resolves to a path that does not exist
// rather than to the real file. The organization layer lives at an absolute
// path outside HOME, so isolating HOME does not isolate it, and a test that
// reaches it stops testing the code and starts testing the developer's
// machine: it inherits real registries and fetches them over the network.
// Defaulting to the real file made that the reward for forgetting to opt out,
// which is the wrong way round, and every package pays for one omission.
// Tests that mean to exercise an organization config still name one through
// SetTestOrganizationConfigPath or LoadEffectiveOptions.OrganizationPath.
func organizationConfigPath(override string) string {
	if override != "" {
		return override
	}
	if testOrganizationConfigPath != "" {
		return testOrganizationConfigPath
	}
	if testing.Testing() {
		return unsetTestOrganizationConfigPath
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
	Reads  []ConfigRead
}

type ConfigRead struct {
	Path            string
	RequestedPath   string
	Digest          string
	Overlay         bool
	RuntimeLock     bool
	AcquisitionLock bool
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
	AllowedMarketplaces []string
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
	RepoRoot         string
	LocalConfigDir   string
	OrganizationPath string
	GlobalPath       string
	RepoPath         string
	LocalPath        string
	FileOverlays     map[string][]byte
	// OrganizationRuntimeLock supplies a verified, process-local lock without
	// making organization configuration itself overlayable.
	OrganizationRuntimeLock []byte
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
	ec.Organization, ec.Diagnostics = loadEffectiveOrganizationWithRuntimeLock(orgPath, opts.OrganizationRuntimeLock)
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

	var allowedMarketplaces []string
	if merged.AllowedMarketplaces != nil {
		allowedMarketplaces = *merged.AllowedMarketplaces
	}
	ec.AllowedMarketplaces = allowedMarketplaces

	if err := ValidateMarketplaceURLs(merged.Marketplaces, allowedMarketplaces); err != nil {
		slog.Error("marketplace allowlist rejected one or more URLs", "error", err)
		ec.Errors = append(ec.Errors, err)
	}
	if errs := ValidateMarketplaceEntries(merged.Marketplaces); len(errs) > 0 {
		err := errors.Join(errs...)
		slog.Error("effective Marketplace entries rejected", "error", err)
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
func loadEffectiveOrganization(path string) (LayerResult, ManagedInstallationDiagnostics) {
	return loadEffectiveOrganizationWithRuntimeLock(path, nil)
}

func loadEffectiveOrganizationWithRuntimeLock(path string, runtimeLock []byte) (LayerResult, ManagedInstallationDiagnostics) {
	if runtime.GOOS == "windows" {
		lr := LayerResult{Source: SourceUnset, Path: path}
		return lr, inspectManagedConfig(path, fs.ErrNotExist, nil, SourceUnset, nil)
	}

	canonicalPath, canonicalErr := canonicalConfigPath(path)
	if canonicalErr != nil {
		if errors.Is(canonicalErr, os.ErrNotExist) {
			lr := LayerResult{Source: SourceUnset, Path: path}
			return lr, inspectManagedConfig(path, os.ErrNotExist, nil, SourceUnset, nil)
		}
		lr := LayerResult{Source: Organization, Path: path, Errors: []error{canonicalErr}}
		return lr, inspectManagedConfig(path, canonicalErr, nil, Organization, lr.Errors)
	}
	if trustErr := ValidateOrganizationConfigAuthority(path); trustErr != nil {
		lr := LayerResult{Source: Organization, Path: path, Errors: []error{trustErr}}
		d := inspectManagedConfig(path, nil, nil, Organization, lr.Errors)
		d.Validation = "untrusted"
		d.Warnings = append(d.Warnings, trustErr)
		return lr, d
	}

	data, readErr := readConfigFile(canonicalPath)
	if readErr != nil {
		lr := LayerResult{Source: Organization, Path: path, Errors: []error{fmt.Errorf("read organization config %q: %w", path, readErr)}}
		return lr, inspectManagedConfig(path, readErr, nil, Organization, lr.Errors)
	}
	var lockOverlays map[string][]byte
	if len(runtimeLock) > 0 {
		paths, pathErr := configLockPaths(path, canonicalPath)
		if pathErr != nil {
			lr := LayerResult{Source: Organization, Path: path, Errors: []error{pathErr}}
			return lr, inspectManagedConfig(path, pathErr, nil, Organization, lr.Errors)
		}
		var overlayErr error
		lockOverlays, overlayErr = organizationRuntimeLockOverlay(paths.CanonicalLock, runtimeLock)
		if overlayErr != nil {
			lr := LayerResult{Source: Organization, Path: path, Errors: []error{overlayErr}}
			return lr, inspectManagedConfig(path, overlayErr, nil, Organization, lr.Errors)
		}
	}
	lr := loadOrganizationConfig(path, canonicalPath, data, lockOverlays)
	lr.Path = path
	if len(lr.Reads) > 0 {
		lr.Reads[0].RequestedPath, _ = filepath.Abs(path)
	}
	d := inspectManagedConfig(path, nil, lr.Config, lr.Source, lr.Errors)
	if len(lr.Errors) == 0 && lr.Config != nil && organizationHasRemoteRequirements(lr.Config) {
		if trustErr := ValidateOrganizationTrustUnit(path, true, true); trustErr != nil {
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
	lr, _ := loadEffectiveOrganization(organizationConfigPath(""))
	return lr.Config, errors.Join(lr.Errors...)
}

func LoadOrganizationPolicyConfig() (*Config, error) {
	path := organizationConfigPath("")
	lr, _ := loadEffectiveOrganization(path)
	if len(lr.Errors) == 0 {
		return lr.Config, nil
	}
	if cfg := organizationPolicyWithoutLock(path, lr.Errors); cfg != nil {
		return cfg, nil
	}
	return nil, errors.Join(lr.Errors...)
}

func organizationPolicyWithoutLock(path string, errs []error) *Config {
	var validation *V4ValidationError
	if !errors.As(errors.Join(errs...), &validation) || validation.Code != "v4.marketplace-lock-required" {
		return nil
	}
	if ValidateOrganizationTrustUnit(path, true, true) != nil {
		return nil
	}
	if _, err := os.Lstat(OrganizationLockPath(path)); !errors.Is(err, os.ErrNotExist) {
		return nil
	}
	v4, err := LoadV4ConfigIfPresent(path, Organization)
	if err != nil || v4 == nil {
		return nil
	}
	cfg, err := v4.policyConfig(organizationLayer)
	if err != nil {
		return nil
	}
	return cfg
}

func organizationHasRemoteRequirements(cfg *Config) bool {
	if len(cfg.Marketplaces) > 0 {
		return true
	}
	for _, entry := range cfg.Registries {
		if !entry.IsLocal() {
			return true
		}
	}
	return false
}

func loadOrganizationConfig(path, canonicalPath string, data []byte, runtimeLocks map[string][]byte) LayerResult {
	cfg, reads, err := loadConfigTreeWithReads(path, canonicalPath, data, organizationLayer, runtimeLocks)
	for i := range reads {
		if !reads[i].Overlay {
			continue
		}
		if _, ok := runtimeLocks[reads[i].Path]; ok {
			reads[i].RuntimeLock = true
			continue
		}
		if _, ok := runtimeLocks[reads[i].RequestedPath]; ok {
			reads[i].RuntimeLock = true
		}
	}
	lr := LayerResult{Source: Organization, Path: path, Config: cfg, Reads: reads}
	if err != nil {
		lr.Errors = []error{err}
	}
	return lr
}

func organizationRuntimeLockOverlay(lockPath string, runtimeLock []byte) (map[string][]byte, error) {
	if len(runtimeLock) == 0 {
		return nil, nil
	}
	if int64(len(runtimeLock)) > lockfile.MaxFileSize {
		return nil, fmt.Errorf("%w: runtime lock %q exceeds %d bytes", lockfile.ErrFileTooLarge, lockPath, lockfile.MaxFileSize)
	}
	return map[string][]byte{lockPath: append([]byte(nil), runtimeLock...)}, nil
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

	canonicalPath, data, readErr := readEffectiveLayer(path, overlays)
	if readErr != nil {
		if errors.Is(readErr, os.ErrNotExist) {
			return LayerResult{Source: UserPersonal, Path: path}
		}
		return LayerResult{Source: UserPersonal, Path: path, Errors: []error{readErr}}
	}
	cfg, reads, err := loadConfigTreeWithReads(path, canonicalPath, data, userLayer, overlays)
	lr := LayerResult{Source: UserPersonal, Path: path, Config: cfg, Reads: reads}
	if err != nil {
		lr.Errors = []error{err}
	}
	return lr
}

func loadEffectiveRepo(repoRoot, override string, overlays map[string][]byte) LayerResult {
	path := override
	if path == "" {
		path = ProjectConfigPath(repoRoot)
	}
	if IsHomeDir(repoRoot) {
		return LayerResult{Source: RepoCommitted, Path: path}
	}

	canonicalPath, data, readErr := readEffectiveLayer(path, overlays)
	if readErr != nil {
		if errors.Is(readErr, os.ErrNotExist) {
			return LayerResult{Source: RepoCommitted, Path: path}
		}
		return LayerResult{Source: RepoCommitted, Path: path, Errors: []error{readErr}}
	}
	cfg, reads, err := loadConfigTreeWithReads(path, canonicalPath, data, repoLayer, overlays)
	lr := LayerResult{Source: RepoCommitted, Path: path, Config: cfg, Reads: reads}
	if err != nil {
		lr.Errors = []error{err}
	}
	return lr
}

func loadEffectiveLocal(localDir, override string, overlays map[string][]byte) LayerResult {
	path := override
	if path == "" {
		path = LocalConfigPath(localDir)
	}
	if IsHomeDir(localDir) {
		return LayerResult{Source: RepoPersonal, Path: path}
	}

	canonicalPath, data, readErr := readEffectiveLayer(path, overlays)
	if readErr != nil {
		if errors.Is(readErr, os.ErrNotExist) {
			return LayerResult{Source: RepoPersonal, Path: path}
		}
		return LayerResult{Source: RepoPersonal, Path: path, Errors: []error{fmt.Errorf("read local config %q: %w", path, readErr)}}
	}
	cfg, reads, err := loadConfigTreeWithReads(path, canonicalPath, data, localLayer, overlays)
	lr := LayerResult{Source: RepoPersonal, Path: path, Local: configAsLocal(cfg), Reads: reads}
	if err != nil {
		lr.Errors = []error{err}
	}
	return lr
}

func readEffectiveLayer(path string, overlays map[string][]byte) (string, []byte, error) {
	canonical, err := canonicalConfigPathTolerateMissing(path)
	if err != nil {
		return "", nil, err
	}
	if data, ok := overlays[path]; ok {
		bounded, err := boundedConfigData(data)
		return canonical, bounded, err
	}
	if data, ok := overlays[canonical]; ok {
		bounded, err := boundedConfigData(data)
		return canonical, bounded, err
	}
	data, err := readConfigFile(path)
	return canonical, data, err
}
