package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
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
	Source  Source
	Path    string
	Config  *Config
	Local   *LocalConfig
	Errors  []error
	Reads   []ConfigRead
	Sources []SourceDecl
}

type ConfigRead struct {
	Path            string
	RequestedPath   string
	Digest          string
	Overlay         bool
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
	// RelockOrganization skips the deployed organization lock's
	// configuration-match check for the explicit organization lock write;
	// decode, scope, and trust validation still apply.
	RelockOrganization bool
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

func globalConfigPath() (string, error) {
	p, err := GlobalConfigPath()
	if err != nil {
		return "", fmt.Errorf("resolve user config path: %w", err)
	}
	return p, nil
}

// LoadOrganizationConfig reads the externally managed organization config at
// the default path. It returns nil when the file is missing. It returns an
// error when the file is present but cannot be read or declares an unsupported
// apiVersion.
func LoadOrganizationConfig() (*Config, error) {
	li, _ := readOrganizationIntent(organizationConfigPath(""))
	cfg, errs := bindOrganizationLayer(li)
	return cfg, errors.Join(errs...)
}

func bindOrganizationLayer(li LayerIntent) (*Config, []error) {
	if !li.Present() || len(li.errors) > 0 {
		return nil, li.errors
	}
	doc, _, evErrs := deployedLayerEvidence(li, Organization, LoadEffectiveOptions{})
	if len(evErrs) > 0 {
		return nil, append(append([]error(nil), li.errors...), evErrs...)
	}
	cfg, err := li.bind(doc)
	if err != nil {
		return nil, []error{err}
	}
	if organizationHasRemoteRequirements(cfg) {
		if trustErr := ValidateOrganizationTrustUnit(li.Path, true, true); trustErr != nil {
			return nil, []error{trustErr}
		}
	}
	return cfg, nil
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
