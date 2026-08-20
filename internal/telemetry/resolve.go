package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	amconfig "github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"
)

const (
	sourceOrganization = "ORG"
	sourceUser         = "USER"
	sourceProject      = "PROJECT"
)

// TelemetryState is the fully resolved telemetry policy for one invocation.
// It carries effective values, the source (authority) that provided them, the
// primary and community routes, and the schema version contract.
type TelemetryState struct {
	EffectiveLevel          string
	EffectiveLevelSource    string
	PrimaryEndpoint         string
	PrimaryEndpointHostname string
	PrimaryEndpointSource   string

	PrimaryHeaders       map[string]tmcfg.HeaderSource
	PrimaryCAFile        string
	PrimaryAllowInsecure bool

	CommunityReportingEnabled           bool
	CommunityReportingSource            string
	CommunityReportingFoldedIntoPrimary bool
	CommunityEndpoint                   string

	RetentionPolicyURL string
	InstallationIDPath string
	Managed            bool
	SchemaVersion      string
}

// StateRoot returns the durable telemetry state directory (the directory
// containing the installation ID file). It returns "" when the installation
// ID path is empty.
func (s TelemetryState) StateRoot() string {
	if s.InstallationIDPath == "" {
		return ""
	}
	return filepath.Dir(s.InstallationIDPath)
}

// QueueRoot returns the durable telemetry queue directory under StateRoot.
// It returns "" when StateRoot is empty.
func (s TelemetryState) QueueRoot() string {
	root := s.StateRoot()
	if root == "" {
		return ""
	}
	return filepath.Join(root, "queue")
}

// StateRootFromInstallationIDPath returns the parent directory of the
// installation ID path, or "" when the path is empty. It is a path-based
// helper for call sites that do not have a TelemetryState value.
func StateRootFromInstallationIDPath(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Dir(path)
}

// queueRootFromInstallationIDPath returns the queue directory for an
// installation ID path. It returns "" when the path is empty.
func queueRootFromInstallationIDPath(path string) string {
	root := StateRootFromInstallationIDPath(path)
	if root == "" {
		return ""
	}
	return filepath.Join(root, "queue")
}

// LoadAndResolve loads the organization and user-personal telemetry config
// layers and resolves the effective TelemetryState.
func LoadAndResolve(ctx context.Context) (TelemetryState, error) {
	slog.InfoContext(ctx, "loading and resolving telemetry config")

	org, err := amconfig.LoadOrganizationConfig()
	if err != nil {
		return TelemetryState{}, fmt.Errorf("load organization config: %w", err)
	}

	userPath, err := amconfig.GlobalConfigPath()
	if err != nil {
		return TelemetryState{}, fmt.Errorf("resolve user config path: %w", err)
	}

	user, err := amconfig.LoadConfigFromPath(userPath)
	if err != nil {
		return TelemetryState{}, fmt.Errorf("load user config: %w", err)
	}

	return ResolveTelemetryConfig(org, user, DefaultProjectEndpoint, DefaultProjectRetentionURL)
}

// ResolveTelemetryConfig computes the effective telemetry state from the
// organization and user configuration layers. projectEndpoint and
// projectRetentionURL are release-configured defaults.
func ResolveTelemetryConfig(organization, user *amconfig.Config, projectEndpoint, projectRetentionURL string) (TelemetryState, error) {
	state := TelemetryState{SchemaVersion: tmcfg.SchemaVersion}
	slog.Info("resolving telemetry config")
	warnIgnoredOtelVars()

	orgTC := telemetryFromConfig(organization)
	userTC := telemetryFromConfig(user)
	orgActive := organization != nil && organization.Telemetry != nil

	var err error
	userTC, err = applyEnvOverrides(userTC, orgTC, orgActive)
	if err != nil {
		return state, err
	}

	if err := validateForResolve(orgTC, "organization"); err != nil {
		return state, err
	}
	if err := validateForResolve(userTC, "user personal"); err != nil {
		return state, err
	}

	orgLevel := safeLevel(orgTC)
	userLevel := safeLevel(userTC)
	effectiveLevel, levelSource := resolveEffectiveLevel(orgLevel, userLevel, orgActive)

	primaryEndpoint, primarySource, err := resolvePrimaryEndpoint(orgTC, userTC, projectEndpoint)
	if err != nil {
		return state, err
	}

	retention, err := resolveRetentionPolicy(primarySource, orgTC, userTC, projectRetentionURL)
	if err != nil {
		return state, err
	}

	communityEnabled, communitySource, folded, err := resolveCommunityReporting(orgTC, userTC, projectEndpoint, primaryEndpoint, primarySource, effectiveLevel, orgActive)
	if err != nil {
		return state, err
	}

	headers, caFile, allowInsecure := resolvePrimaryAuth(primarySource, orgTC, userTC)

	state.EffectiveLevel = effectiveLevel
	state.EffectiveLevelSource = levelSource
	state.PrimaryEndpoint = primaryEndpoint
	state.PrimaryEndpointHostname = hostnameFromURL(primaryEndpoint)
	state.PrimaryEndpointSource = primarySource
	state.PrimaryHeaders = headers
	state.PrimaryCAFile = caFile
	state.PrimaryAllowInsecure = allowInsecure
	state.CommunityReportingEnabled = communityEnabled
	state.CommunityReportingSource = communitySource
	state.CommunityReportingFoldedIntoPrimary = folded
	state.CommunityEndpoint = projectEndpoint
	state.RetentionPolicyURL = retention
	state.Managed = orgActive
	state.InstallationIDPath, err = installationIDPath()
	if err != nil {
		return state, fmt.Errorf("resolve installation id path: %w", err)
	}
	slog.Info("telemetry config resolved",
		"effective_level", state.EffectiveLevel,
		"level_source", state.EffectiveLevelSource,
		"primary_source", state.PrimaryEndpointSource,
		"primary_host", state.PrimaryEndpointHostname,
		"community_enabled", state.CommunityReportingEnabled,
		"community_source", state.CommunityReportingSource,
	)
	return state, nil
}

func telemetryFromConfig(cfg *amconfig.Config) *tmcfg.TelemetryConfig {
	if cfg == nil || cfg.Telemetry == nil {
		return &tmcfg.TelemetryConfig{}
	}
	return cfg.Telemetry
}

func safeLevel(tc *tmcfg.TelemetryConfig) string {
	level, _ := tmcfg.NormalizeLevel(tc.Level)
	return level
}

func validateForResolve(tc *tmcfg.TelemetryConfig, layer string) error {
	if errs := tmcfg.Validate(tc, layer); len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func resolveEffectiveLevel(orgLevel, userLevel string, orgActive bool) (string, string) {
	if tmcfg.LevelOrder(orgLevel) > tmcfg.LevelOrder(userLevel) {
		return orgLevel, sourceOrganization
	}
	if tmcfg.LevelOrder(userLevel) > tmcfg.LevelOrder(orgLevel) {
		return userLevel, sourceUser
	}
	if tmcfg.LevelOrder(orgLevel) > 0 && orgActive {
		return orgLevel, sourceOrganization
	}
	return userLevel, sourceUser
}

func resolvePrimaryEndpoint(orgTC, userTC *tmcfg.TelemetryConfig, projectEndpoint string) (string, string, error) {
	if orgTC.Endpoint != "" {
		if err := tmcfg.ValidateEndpoint(orgTC.Endpoint, orgTC.AllowInsecure); err != nil {
			return "", "", err
		}
		return orgTC.Endpoint, sourceOrganization, nil
	}
	if userTC.Endpoint != "" {
		// allow_insecure is an organization-only policy; user endpoints must be HTTPS or loopback.
		if err := tmcfg.ValidateEndpoint(userTC.Endpoint, false); err != nil {
			return "", "", err
		}
		return userTC.Endpoint, sourceUser, nil
	}
	if projectEndpoint != "" {
		if err := tmcfg.ValidateEndpoint(projectEndpoint, false); err != nil {
			return "", "", err
		}
	}
	return projectEndpoint, sourceProject, nil
}

func resolveRetentionPolicy(primarySource string, orgTC, userTC *tmcfg.TelemetryConfig, projectRetentionURL string) (string, error) {
	switch primarySource {
	case sourceOrganization:
		if orgTC.RetentionPolicy == "" {
			return "", fmt.Errorf("retention_policy is required when organization endpoint is the primary route")
		}
		return orgTC.RetentionPolicy, nil
	case sourceUser:
		if userTC.RetentionPolicy != "" {
			return userTC.RetentionPolicy, nil
		}
		return projectRetentionURL, nil
	default:
		return projectRetentionURL, nil
	}
}

func resolveCommunityReporting(orgTC, userTC *tmcfg.TelemetryConfig, projectEndpoint, primaryEndpoint, primarySource, effectiveLevel string, orgActive bool) (bool, string, bool, error) {
	enabled, source := false, ""
	if orgActive && orgTC.CommunityReporting != nil {
		// Organization policy explicitly manages community_reporting.
		source = sourceOrganization
		enabled = *orgTC.CommunityReporting
	} else if userTC.CommunityReporting != nil {
		enabled = *userTC.CommunityReporting
		source = sourceUser
	} else if orgActive {
		// Organization telemetry block is active but does not specify the
		// field; the default false is still managed by the organization.
		source = sourceOrganization
	}
	if !enabled {
		return false, source, false, nil
	}
	if effectiveLevel == tmcfg.LevelOff {
		return false, source, false, nil
	}
	if projectEndpoint == "" {
		return false, source, false, nil
	}
	folded := canonicalURLEqual(primaryEndpoint, projectEndpoint)
	return true, source, folded, nil
}

func resolvePrimaryAuth(primarySource string, orgTC, userTC *tmcfg.TelemetryConfig) (map[string]tmcfg.HeaderSource, string, bool) {
	switch primarySource {
	case sourceOrganization:
		return cloneHeaders(orgTC.Headers), orgTC.CAFile, orgTC.AllowInsecure
	case sourceUser:
		// allow_insecure is an organization-only policy.
		return cloneHeaders(userTC.Headers), userTC.CAFile, false
	}
	return nil, "", false
}

func applyEnvOverrides(userTC, orgTC *tmcfg.TelemetryConfig, orgActive bool) (*tmcfg.TelemetryConfig, error) {
	userTC = cloneTelemetryConfig(userTC)

	if ep := os.Getenv(EnvEndpoint); ep != "" {
		if orgActive && orgTC.Endpoint != "" {
			slog.Debug("skipping telemetry endpoint env override because organization policy is active", "var", EnvEndpoint)
		} else {
			slog.Info("applying telemetry endpoint override from environment", "var", EnvEndpoint, "endpoint", ep)
			userTC.Endpoint = ep
		}
	}

	if lv := os.Getenv(EnvLevel); lv != "" {
		if orgActive && orgTC.Level != "" {
			slog.Debug("skipping telemetry level env override because organization policy is active", "var", EnvLevel)
		} else {
			normalized, err := tmcfg.NormalizeLevel(lv)
			if err != nil {
				return nil, fmt.Errorf("environment variable %s: %w", EnvLevel, err)
			}
			slog.Info("applying telemetry level override from environment", "var", EnvLevel, "level", normalized)
			userTC.Level = normalized
		}
	}

	if v := os.Getenv(EnvCommunityReporting); v != "" {
		if orgActive && orgTC.CommunityReporting != nil {
			slog.Debug("skipping telemetry community reporting env override because organization policy is active", "var", EnvCommunityReporting)
		} else {
			b, err := strconv.ParseBool(v)
			if err != nil {
				return nil, fmt.Errorf("environment variable %s: %w", EnvCommunityReporting, err)
			}
			slog.Info("applying telemetry community reporting override from environment", "var", EnvCommunityReporting, "enabled", b)
			userTC.CommunityReporting = &b
		}
	}

	return userTC, nil
}

func cloneTelemetryConfig(tc *tmcfg.TelemetryConfig) *tmcfg.TelemetryConfig {
	if tc == nil {
		return &tmcfg.TelemetryConfig{}
	}
	c := *tc
	if tc.CommunityReporting != nil {
		b := *tc.CommunityReporting
		c.CommunityReporting = &b
	}
	c.Headers = cloneHeaders(tc.Headers)
	return &c
}

func cloneHeaders(headers map[string]tmcfg.HeaderSource) map[string]tmcfg.HeaderSource {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string]tmcfg.HeaderSource, len(headers))
	for k, v := range headers {
		out[k] = v
	}
	return out
}

func warnIgnoredOtelVars() {
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(key, "OTEL_") {
			slog.Warn("ignoring standard OTel environment variable for telemetry policy resolution", "var", key)
		}
	}
}

func canonicalURLEqual(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	ua, err := url.Parse(a)
	if err != nil {
		return false
	}
	ub, err := url.Parse(b)
	if err != nil {
		return false
	}
	if ua.Scheme != ub.Scheme {
		return false
	}
	if ua.Host != ub.Host {
		return false
	}
	pathA := canonicalMetricsPath(ua.Path)
	pathB := canonicalMetricsPath(ub.Path)
	return pathA == pathB
}

// canonicalMetricsPath applies the default metrics path when no path is present.
// This matches the exporter behavior, which only appends /v1/metrics when the
// configured endpoint has an empty path.
func canonicalMetricsPath(p string) string {
	p = strings.TrimSuffix(p, "/")
	if p == "" {
		return "/v1/metrics"
	}
	return p
}

func hostnameFromURL(endpoint string) string {
	if endpoint == "" {
		return ""
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func installationIDPath() (string, error) {
	root, err := ownership.StateRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, DefaultInstallationIDSubpath), nil
}
