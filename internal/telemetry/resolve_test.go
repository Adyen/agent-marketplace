package telemetry

import (
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	amconfig "github.com/Adyen/agent-marketplace/internal/config"
	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"
)

func boolPtr(b bool) *bool { return &b }

func setStateRoot(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	return dir
}

func expectedInstallID(dir string) string {
	return filepath.Join(dir, "agent-marketplace", "telemetry", "installation-id")
}

func TestResolveDefaultOff(t *testing.T) {
	dir := setStateRoot(t)
	state, err := ResolveTelemetryConfig(amconfig.Settings{}, amconfig.Settings{}, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.EffectiveLevel != tmcfg.LevelOff {
		t.Fatalf("effective level = %q, want off", state.EffectiveLevel)
	}
	if state.EffectiveLevelSource != sourceUser {
		t.Fatalf("level source = %q, want USER", state.EffectiveLevelSource)
	}
	if state.PrimaryEndpoint != "" {
		t.Fatalf("primary endpoint = %q, want empty", state.PrimaryEndpoint)
	}
	if state.CommunityReportingEnabled {
		t.Fatal("community reporting should be disabled when off")
	}
	if state.SchemaVersion != tmcfg.SchemaVersion {
		t.Fatalf("schema version = %q, want %q", state.SchemaVersion, tmcfg.SchemaVersion)
	}
	if state.InstallationIDPath != expectedInstallID(dir) {
		t.Fatalf("installation id path = %q, want %q", state.InstallationIDPath, expectedInstallID(dir))
	}
}

func TestResolveProjectEndpoint(t *testing.T) {
	dir := setStateRoot(t)
	state, err := ResolveTelemetryConfig(amconfig.Settings{}, amconfig.Settings{}, "https://project.example.com", "https://project.example.com/retention")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.PrimaryEndpoint != "https://project.example.com" {
		t.Fatalf("primary endpoint = %q", state.PrimaryEndpoint)
	}
	if state.PrimaryEndpointSource != sourceProject {
		t.Fatalf("primary source = %q, want PROJECT", state.PrimaryEndpointSource)
	}
	if state.RetentionPolicyURL != "https://project.example.com/retention" {
		t.Fatalf("retention = %q", state.RetentionPolicyURL)
	}
	if state.InstallationIDPath != expectedInstallID(dir) {
		t.Fatalf("installation id path = %q", state.InstallationIDPath)
	}
}

func TestResolveUserEndpointAndCommunity(t *testing.T) {
	setStateRoot(t)
	user := amconfig.Settings{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:              tmcfg.LevelBasic,
			Endpoint:           "https://user.example.com",
			CommunityReporting: boolPtr(true),
		},
	}
	state, err := ResolveTelemetryConfig(amconfig.Settings{}, user, "https://project.example.com", "https://project.example.com/retention")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.EffectiveLevel != tmcfg.LevelBasic {
		t.Fatalf("effective level = %q", state.EffectiveLevel)
	}
	if state.EffectiveLevelSource != sourceUser {
		t.Fatalf("level source = %q", state.EffectiveLevelSource)
	}
	if state.PrimaryEndpoint != "https://user.example.com" || state.PrimaryEndpointSource != sourceUser {
		t.Fatalf("primary route mismatch: %q/%q", state.PrimaryEndpoint, state.PrimaryEndpointSource)
	}
	if state.RetentionPolicyURL != "https://project.example.com/retention" {
		t.Fatalf("retention = %q", state.RetentionPolicyURL)
	}
	if !state.CommunityReportingEnabled {
		t.Fatal("community reporting should be enabled")
	}
	if state.CommunityReportingSource != sourceUser {
		t.Fatalf("community source = %q", state.CommunityReportingSource)
	}
	if state.CommunityReportingFoldedIntoPrimary {
		t.Fatal("community should not be folded when primary is user endpoint")
	}
	if state.CommunityEndpoint != "https://project.example.com" {
		t.Fatalf("community endpoint = %q", state.CommunityEndpoint)
	}
}

func TestResolveOrganizationEndpointLocksUser(t *testing.T) {
	setStateRoot(t)
	org := amconfig.Settings{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:           tmcfg.LevelDetailed,
			Endpoint:        "https://org.example.com",
			RetentionPolicy: "https://org.example.com/retention",
		},
	}
	user := amconfig.Settings{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:    tmcfg.LevelBasic,
			Endpoint: "https://user.example.com",
		},
	}
	state, err := ResolveTelemetryConfig(org, user, "https://project.example.com", "https://project.example.com/retention")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.PrimaryEndpoint != "https://org.example.com" || state.PrimaryEndpointSource != sourceOrganization {
		t.Fatalf("organization endpoint should lock primary route: %q/%q", state.PrimaryEndpoint, state.PrimaryEndpointSource)
	}
	if state.EffectiveLevel != tmcfg.LevelDetailed || state.EffectiveLevelSource != sourceOrganization {
		t.Fatalf("organization level should win: %q/%q", state.EffectiveLevel, state.EffectiveLevelSource)
	}
	if state.RetentionPolicyURL != "https://org.example.com/retention" {
		t.Fatalf("retention = %q", state.RetentionPolicyURL)
	}
}

func TestResolveOrganizationMinimum(t *testing.T) {
	setStateRoot(t)
	org := amconfig.Settings{
		Telemetry: &tmcfg.TelemetryConfig{Level: tmcfg.LevelBasic},
	}
	state, err := ResolveTelemetryConfig(org, amconfig.Settings{}, "https://project.example.com", "https://project.example.com/retention")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.EffectiveLevel != tmcfg.LevelBasic || state.EffectiveLevelSource != sourceOrganization {
		t.Fatalf("expected organization minimum basic: got %q/%q", state.EffectiveLevel, state.EffectiveLevelSource)
	}
	if state.PrimaryEndpointSource != sourceProject {
		t.Fatalf("primary source = %q, want PROJECT", state.PrimaryEndpointSource)
	}
}

func TestResolveOrganizationLevelPolicy(t *testing.T) {
	tests := []struct {
		name       string
		orgLevel   string
		userLevel  string
		wantLevel  string
		wantSource string
	}{
		{"organization detailed locks user basic", tmcfg.LevelDetailed, tmcfg.LevelBasic, tmcfg.LevelDetailed, sourceOrganization},
		{"organization off locks user detailed", tmcfg.LevelOff, tmcfg.LevelDetailed, tmcfg.LevelOff, sourceOrganization},
		{"organization basic locks user off", tmcfg.LevelBasic, tmcfg.LevelOff, tmcfg.LevelBasic, sourceOrganization},
		{"organization off locks user basic", tmcfg.LevelOff, tmcfg.LevelBasic, tmcfg.LevelOff, sourceOrganization},
		{"organization basic locks user detailed", tmcfg.LevelBasic, tmcfg.LevelDetailed, tmcfg.LevelBasic, sourceOrganization},
		{"organization detailed locks user off", tmcfg.LevelDetailed, tmcfg.LevelOff, tmcfg.LevelDetailed, sourceOrganization},
		{"organization level omitted leaves user preference", "", tmcfg.LevelDetailed, tmcfg.LevelDetailed, sourceUser},
		{"tie organization active", tmcfg.LevelBasic, tmcfg.LevelBasic, tmcfg.LevelBasic, sourceOrganization},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setStateRoot(t)
			org := amconfig.Settings{Telemetry: &tmcfg.TelemetryConfig{Level: tc.orgLevel}}
			user := amconfig.Settings{Telemetry: &tmcfg.TelemetryConfig{Level: tc.userLevel}}
			state, err := ResolveTelemetryConfig(org, user, "", "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if state.EffectiveLevel != tc.wantLevel || state.EffectiveLevelSource != tc.wantSource {
				t.Fatalf("got %q/%q, want %q/%q", state.EffectiveLevel, state.EffectiveLevelSource, tc.wantLevel, tc.wantSource)
			}
		})
	}
}

func TestResolveOrganizationLocksCommunity(t *testing.T) {
	setStateRoot(t)
	org := amconfig.Settings{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:              tmcfg.LevelBasic,
			CommunityReporting: boolPtr(false),
		},
	}
	user := amconfig.Settings{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:              tmcfg.LevelBasic,
			CommunityReporting: boolPtr(true),
		},
	}
	state, err := ResolveTelemetryConfig(org, user, "https://project.example.com", "https://project.example.com/retention")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.CommunityReportingEnabled {
		t.Fatal("organization community_reporting=false should lock user=true")
	}
	if state.CommunityReportingSource != sourceOrganization {
		t.Fatalf("community source = %q", state.CommunityReportingSource)
	}
}

func TestResolveDeduplication(t *testing.T) {
	setStateRoot(t)
	user := amconfig.Settings{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:              tmcfg.LevelBasic,
			Endpoint:           "https://project.example.com",
			CommunityReporting: boolPtr(true),
		},
	}
	state, err := ResolveTelemetryConfig(amconfig.Settings{}, user, "https://project.example.com", "https://project.example.com/retention")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !state.CommunityReportingEnabled {
		t.Fatal("community reporting should be enabled")
	}
	if !state.CommunityReportingFoldedIntoPrimary {
		t.Fatal("community report should be folded into primary when endpoints match")
	}
	if state.PrimaryEndpointSource != sourceUser {
		t.Fatalf("primary source = %q, want USER", state.PrimaryEndpointSource)
	}
}

func TestResolveEnvOverrides(t *testing.T) {
	tests := []struct {
		name          string
		org           amconfig.Settings
		user          amconfig.Settings
		envEndpoint   string
		envLevel      string
		envCommunity  string
		wantEndpoint  string
		wantSource    string
		wantLevel     string
		wantCommunity bool
	}{
		{
			name:         "env endpoint and level",
			wantEndpoint: "https://env.example.com",
			wantSource:   sourceUser,
			wantLevel:    tmcfg.LevelDetailed,
			envEndpoint:  "https://env.example.com",
			envLevel:     "detailed",
		},
		{
			name:          "env community reporting",
			wantEndpoint:  "https://project.example.com",
			wantSource:    sourceProject,
			wantLevel:     tmcfg.LevelBasic,
			wantCommunity: true,
			envLevel:      "basic",
			envCommunity:  "true",
		},
		{
			name:         "env endpoint ignored when org endpoint locked",
			org:          amconfig.Settings{Telemetry: &tmcfg.TelemetryConfig{Endpoint: "https://org.example.com", RetentionPolicy: "https://org.example.com/r"}},
			wantEndpoint: "https://org.example.com",
			wantSource:   sourceOrganization,
			wantLevel:    tmcfg.LevelOff,
			envEndpoint:  "https://env.example.com",
		},
		{
			name:         "env level ignored when organization policy has no level",
			org:          amconfig.Settings{Telemetry: &tmcfg.TelemetryConfig{Endpoint: "https://org.example.com", RetentionPolicy: "https://org.example.com/r"}},
			wantEndpoint: "https://org.example.com",
			wantSource:   sourceOrganization,
			wantLevel:    tmcfg.LevelOff,
			envLevel:     "detailed",
		},
		{
			name:         "env level ignored when org level locked",
			org:          amconfig.Settings{Telemetry: &tmcfg.TelemetryConfig{Level: tmcfg.LevelBasic}},
			wantEndpoint: "https://project.example.com",
			wantSource:   sourceProject,
			wantLevel:    tmcfg.LevelBasic,
			envLevel:     "detailed",
		},
		{
			name:          "env community ignored when org community locked",
			org:           amconfig.Settings{Telemetry: &tmcfg.TelemetryConfig{Level: tmcfg.LevelBasic, CommunityReporting: boolPtr(false)}},
			wantEndpoint:  "https://project.example.com",
			wantSource:    sourceProject,
			wantLevel:     tmcfg.LevelBasic,
			wantCommunity: false,
			envCommunity:  "true",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setStateRoot(t)
			t.Setenv(EnvEndpoint, tc.envEndpoint)
			t.Setenv(EnvLevel, tc.envLevel)
			t.Setenv(EnvCommunityReporting, tc.envCommunity)
			state, err := ResolveTelemetryConfig(tc.org, tc.user, "https://project.example.com", "https://project.example.com/retention")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if state.PrimaryEndpoint != tc.wantEndpoint || state.PrimaryEndpointSource != tc.wantSource {
				t.Fatalf("endpoint got %q/%q, want %q/%q", state.PrimaryEndpoint, state.PrimaryEndpointSource, tc.wantEndpoint, tc.wantSource)
			}
			if state.EffectiveLevel != tc.wantLevel {
				t.Fatalf("level = %q, want %q", state.EffectiveLevel, tc.wantLevel)
			}
			if state.CommunityReportingEnabled != tc.wantCommunity {
				t.Fatalf("community = %v, want %v", state.CommunityReportingEnabled, tc.wantCommunity)
			}
		})
	}
}

func TestResolveEnvOverrideInvalidEndpoint(t *testing.T) {
	setStateRoot(t)
	t.Setenv(EnvEndpoint, "http://public.example.com")
	_, err := ResolveTelemetryConfig(amconfig.Settings{}, amconfig.Settings{}, "", "")
	if err == nil {
		t.Fatal("expected error for insecure public env endpoint")
	}
}

func TestResolveCommunityDisabledWhenLevelOff(t *testing.T) {
	setStateRoot(t)
	user := amconfig.Settings{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:              tmcfg.LevelOff,
			CommunityReporting: boolPtr(true),
		},
	}
	state, err := ResolveTelemetryConfig(amconfig.Settings{}, user, "https://project.example.com", "https://project.example.com/retention")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.CommunityReportingEnabled {
		t.Fatal("community reporting should be disabled when level is off")
	}
}

func TestResolveAuthAndCA(t *testing.T) {
	setStateRoot(t)
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	tenant := filepath.Join(dir, "tenant")
	if err := os.WriteFile(tenant, []byte("tenant-a"), 0o600); err != nil {
		t.Fatal(err)
	}
	ca := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(ca, []byte("cert\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	user := amconfig.Settings{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:         tmcfg.LevelBasic,
			Endpoint:      "https://user.example.com",
			CAFile:        ca,
			AllowInsecure: false,
			Headers: map[string]tmcfg.HeaderSource{
				"Authorization": {Env: "OTEL_AUTH"},
				"X-Tenant":      {File: tenant},
			},
		},
	}
	state, err := ResolveTelemetryConfig(amconfig.Settings{}, user, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.PrimaryCAFile != ca {
		t.Fatalf("ca file = %q, want %q", state.PrimaryCAFile, ca)
	}
	if state.PrimaryAllowInsecure {
		t.Fatal("allow_insecure should be false")
	}
	want := map[string]tmcfg.HeaderSource{
		"Authorization": {Env: "OTEL_AUTH"},
		"X-Tenant":      {File: tenant},
	}
	if !reflect.DeepEqual(state.PrimaryHeaders, want) {
		t.Fatalf("headers = %#v, want %#v", state.PrimaryHeaders, want)
	}
}

func TestResolveRejectsDisabledUserTransportSettingsWhenOrganizationEnablesTelemetry(t *testing.T) {
	setStateRoot(t)
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	caFile := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(caFile, []byte("cert\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(caFile, 0o644); err != nil {
		t.Fatal(err)
	}

	org := amconfig.Settings{
		Telemetry: &tmcfg.TelemetryConfig{Level: tmcfg.LevelBasic},
	}
	user := amconfig.Settings{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:    tmcfg.LevelOff,
			Endpoint: "https://user.example.com",
			CAFile:   caFile,
		},
	}
	if _, err := ResolveTelemetryConfig(org, user, "", ""); err == nil {
		t.Fatal("expected disabled user transport settings to be validated when organization enables telemetry")
	}
}

func TestResolveOrganizationUsesDefaultRetention(t *testing.T) {
	setStateRoot(t)
	org := amconfig.Settings{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:    tmcfg.LevelBasic,
			Endpoint: "https://org.example.com",
		},
	}
	state, err := ResolveTelemetryConfig(org, amconfig.Settings{}, "https://project.example.com", "https://project.example.com/retention")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.RetentionPolicyURL != "https://project.example.com/retention" {
		t.Fatalf("retention = %q, want project default", state.RetentionPolicyURL)
	}
}

func TestResolveInsecurePublicEndpointErrors(t *testing.T) {
	setStateRoot(t)
	user := amconfig.Settings{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:    tmcfg.LevelBasic,
			Endpoint: "http://public.example.com",
		},
	}
	_, err := ResolveTelemetryConfig(amconfig.Settings{}, user, "", "")
	if err == nil {
		t.Fatal("expected error for insecure public endpoint")
	}
}

func TestResolveEnvLevelGatedByOrganization(t *testing.T) {
	setStateRoot(t)
	t.Setenv(EnvLevel, "detailed")
	org := amconfig.Settings{
		Telemetry: &tmcfg.TelemetryConfig{
			Level: tmcfg.LevelBasic,
		},
	}
	state, err := ResolveTelemetryConfig(org, amconfig.Settings{}, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.EffectiveLevel != tmcfg.LevelBasic || state.EffectiveLevelSource != sourceOrganization {
		t.Fatalf("organization level must not be overridden by env: got %q/%q", state.EffectiveLevel, state.EffectiveLevelSource)
	}
}

func TestResolveCommunityFoldingWithDefaultMetricsPath(t *testing.T) {
	setStateRoot(t)
	user := amconfig.Settings{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:              tmcfg.LevelBasic,
			Endpoint:           "https://project.example.com/v1/metrics",
			CommunityReporting: boolPtr(true),
		},
	}
	state, err := ResolveTelemetryConfig(amconfig.Settings{}, user, "https://project.example.com", "https://project.example.com/retention")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !state.CommunityReportingEnabled {
		t.Fatal("community reporting should be enabled")
	}
	if !state.CommunityReportingFoldedIntoPrimary {
		t.Fatal("community report should be folded into primary when default path is applied")
	}
	if state.PrimaryEndpointSource != sourceUser {
		t.Fatalf("primary source = %q, want USER", state.PrimaryEndpointSource)
	}
}

func TestResolveCommunityNotFoldedWithCustomPath(t *testing.T) {
	setStateRoot(t)
	user := amconfig.Settings{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:              tmcfg.LevelBasic,
			Endpoint:           "https://project.example.com/custom",
			CommunityReporting: boolPtr(true),
		},
	}
	state, err := ResolveTelemetryConfig(amconfig.Settings{}, user, "https://project.example.com", "https://project.example.com/retention")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !state.CommunityReportingEnabled {
		t.Fatal("community reporting should be enabled")
	}
	if state.CommunityReportingFoldedIntoPrimary {
		t.Fatal("community report should not be folded with a custom primary path")
	}
}

func TestResolveOrganizationPrivateHostnameEndpoint(t *testing.T) {
	setStateRoot(t)

	old := tmcfg.LookupIP
	tmcfg.LookupIP = func(host string) ([]net.IP, error) {
		if host == "private.example.com" {
			return []net.IP{net.ParseIP("10.0.0.5")}, nil
		}
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	defer func() { tmcfg.LookupIP = old }()

	org := amconfig.Settings{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:           tmcfg.LevelBasic,
			Endpoint:        "http://private.example.com",
			RetentionPolicy: "https://example.com/retention",
			AllowInsecure:   true,
		},
	}
	state, err := ResolveTelemetryConfig(org, amconfig.Settings{}, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.PrimaryEndpoint != "http://private.example.com" || state.PrimaryEndpointSource != sourceOrganization {
		t.Fatalf("primary route mismatch: %q/%q", state.PrimaryEndpoint, state.PrimaryEndpointSource)
	}
}

func TestResolveOrganizationPrivateHostnameOffline(t *testing.T) {
	setStateRoot(t)

	old := tmcfg.LookupIP
	tmcfg.LookupIP = func(host string) ([]net.IP, error) {
		return nil, &net.DNSError{Err: "network is unreachable", Name: host}
	}
	defer func() { tmcfg.LookupIP = old }()

	org := amconfig.Settings{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:           tmcfg.LevelBasic,
			Endpoint:        "http://private.example.com",
			RetentionPolicy: "https://example.com/retention",
			AllowInsecure:   true,
		},
	}
	state, err := ResolveTelemetryConfig(org, amconfig.Settings{}, "", "")
	if err != nil {
		t.Fatalf("offline DNS failure should not reject valid configuration: %v", err)
	}
	if state.PrimaryEndpoint != "http://private.example.com" {
		t.Fatalf("primary endpoint = %q", state.PrimaryEndpoint)
	}
}

func TestResolveOrganizationPrivateHostnamePublicIP(t *testing.T) {
	setStateRoot(t)

	old := tmcfg.LookupIP
	tmcfg.LookupIP = func(host string) ([]net.IP, error) {
		if host == "private.example.com" {
			return []net.IP{net.ParseIP("203.0.113.1")}, nil
		}
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	defer func() { tmcfg.LookupIP = old }()

	org := amconfig.Settings{
		Telemetry: &tmcfg.TelemetryConfig{
			Level:           tmcfg.LevelBasic,
			Endpoint:        "http://private.example.com",
			RetentionPolicy: "https://example.com/retention",
			AllowInsecure:   true,
		},
	}
	_, err := ResolveTelemetryConfig(org, amconfig.Settings{}, "", "")
	if err == nil {
		t.Fatal("expected error when private hostname resolves to a public IP")
	}
}

// spec: config-layering/existing-unsupported-identifier-is-retained-without-materialization-authority
func TestResolveHarnessesFiltersUnsupportedTargets(t *testing.T) {
	setStateRoot(t)

	basicUser := func(harnesses []string) amconfig.Settings {
		return amconfig.Settings{
			Harnesses: harnesses,
			Telemetry: &tmcfg.TelemetryConfig{
				Level:              tmcfg.LevelBasic,
				Endpoint:           "https://user.example.com",
				CommunityReporting: boolPtr(false),
			},
		}
	}
	user := basicUser([]string{"factory", "future-harness"})
	state, err := ResolveTelemetryConfig(amconfig.Settings{}, user, "https://project.example.com", "https://project.example.com/retention")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if state.EffectiveLevel != tmcfg.LevelBasic {
		t.Fatalf("effective level = %q, want basic", state.EffectiveLevel)
	}
	if state.PrimaryEndpoint != "https://user.example.com" || state.PrimaryEndpointSource != sourceUser {
		t.Fatalf("primary route = %q/%q", state.PrimaryEndpoint, state.PrimaryEndpointSource)
	}
	if got := state.HarnessesString(); got != "factory" {
		t.Fatalf("harness attribute = %q, want %q", got, "factory")
	}

	state, err = ResolveTelemetryConfig(amconfig.Settings{Harnesses: []string{"windsurf"}}, user, "https://project.example.com", "https://project.example.com/retention")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if state.EffectiveLevel != tmcfg.LevelBasic {
		t.Fatalf("effective level = %q, want basic", state.EffectiveLevel)
	}
	if got := state.HarnessesString(); got != "factory" {
		t.Fatalf("user set takes precedence: %q, want %q", got, "factory")
	}

	state, err = ResolveTelemetryConfig(amconfig.Settings{Harnesses: []string{"windsurf"}}, basicUser([]string{"future-harness"}), "https://project.example.com", "https://project.example.com/retention")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if state.EffectiveLevel != tmcfg.LevelBasic {
		t.Fatalf("effective level = %q, want basic", state.EffectiveLevel)
	}
	if got := state.HarnessesString(); got != "" {
		t.Fatalf("unsupported-only user set must not fall back or leak the identifier: %q", got)
	}
}
