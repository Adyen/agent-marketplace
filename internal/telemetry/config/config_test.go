package telemetryconfig

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

func TestNormalizeLevel(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"", LevelOff, false},
		{"off", LevelOff, false},
		{"OFF", LevelOff, false},
		{"basic", LevelBasic, false},
		{"Basic", LevelBasic, false},
		{"detailed", LevelDetailed, false},
		{"DETAILED", LevelDetailed, false},
		{"diagnostic", "", true},
		{"invalid", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := NormalizeLevel(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("NormalizeLevel(%q) error = %v, wantErr %v", tc.input, err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("NormalizeLevel(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestMaxLevel(t *testing.T) {
	tests := []struct {
		a, b, want string
	}{
		{LevelOff, LevelOff, LevelOff},
		{LevelOff, LevelBasic, LevelBasic},
		{LevelBasic, LevelDetailed, LevelDetailed},
		{LevelDetailed, LevelBasic, LevelDetailed},
		{LevelBasic, LevelBasic, LevelBasic},
	}
	for _, tc := range tests {
		t.Run(tc.a+"_"+tc.b, func(t *testing.T) {
			if got := MaxLevel(tc.a, tc.b); got != tc.want {
				t.Fatalf("MaxLevel(%q, %q) = %q, want %q", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestValidateEmptyTelemetry(t *testing.T) {
	tc := &TelemetryConfig{}
	if errs := Validate(tc, "organization"); len(errs) > 0 {
		t.Fatalf("expected empty telemetry config to be valid, got %v", errs)
	}
}

func TestValidateRejectsRepoAndLocalLayers(t *testing.T) {
	tc := &TelemetryConfig{Level: LevelBasic}
	for _, layer := range []string{"repository committed", "repository personal", "repo", "local"} {
		t.Run(layer, func(t *testing.T) {
			errs := Validate(tc, layer)
			if len(errs) == 0 {
				t.Fatalf("expected layer %q to reject telemetry", layer)
			}
		})
	}
}

func TestValidateEndpoint(t *testing.T) {
	tests := []struct {
		name          string
		endpoint      string
		allowInsecure bool
		wantErr       bool
	}{
		{"https public", "https://otel.example.com", false, false},
		{"http loopback", "http://localhost:4318", false, false},
		{"http 127.0.0.1", "http://127.0.0.1:4318", false, false},
		{"http private allowed", "http://192.168.1.1", true, false},
		{"http private disallowed", "http://192.168.1.1", false, true},
		{"http public", "http://otel.example.com", false, true},
		{"invalid scheme", "ftp://otel.example.com", false, true},
		{"missing host", "https:///path", false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateEndpoint(tc.endpoint, tc.allowInsecure)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateEndpoint(%q, %v) error = %v, wantErr %v", tc.endpoint, tc.allowInsecure, err, tc.wantErr)
			}
		})
	}
}

func TestValidateOrganizationRequiresRetention(t *testing.T) {
	tc := &TelemetryConfig{
		Level:    LevelBasic,
		Endpoint: "https://otel.example.com",
	}
	errs := Validate(tc, "organization")
	if len(errs) == 0 {
		t.Fatalf("expected organization endpoint to require retention_policy")
	}

	tc.RetentionPolicy = "https://example.com/telemetry-retention"
	errs = Validate(tc, "organization")
	if len(errs) > 0 {
		t.Fatalf("expected valid org telemetry with retention policy, got %v", errs)
	}
}

func TestValidateHeaders(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "tenant")
	if err := os.WriteFile(existing, []byte("tenant-a"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		headers map[string]HeaderSource
		wantErr bool
	}{
		{
			name: "env source",
			headers: map[string]HeaderSource{
				"Authorization": {Env: "EXAMPLE_OTEL_AUTHORIZATION"},
			},
		},
		{
			name: "file source",
			headers: map[string]HeaderSource{
				"X-Tenant": {File: existing},
			},
		},
		{
			name: "both sources",
			headers: map[string]HeaderSource{
				"Authorization": {Env: "FOO", File: existing},
			},
			wantErr: true,
		},
		{
			name: "missing source",
			headers: map[string]HeaderSource{
				"Authorization": {},
			},
			wantErr: true,
		},
		{
			name: "missing file",
			headers: map[string]HeaderSource{
				"Authorization": {File: filepath.Join(dir, "missing")},
			},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &TelemetryConfig{Headers: tc.headers}
			errs := Validate(cfg, "user personal")
			if tc.wantErr {
				if len(errs) == 0 {
					t.Fatalf("expected header validation error")
				}
			} else if len(errs) > 0 {
				t.Fatalf("expected no header errors, got %v", errs)
			}
		})
	}
}

func TestValidateCAFile(t *testing.T) {
	dir := t.TempDir()
	cert := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(cert, []byte("cert"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		caFile  string
		wantErr bool
	}{
		{cert, false},
		{filepath.Join(dir, "missing"), true},
		{dir, true},
	}
	for _, tc := range tests {
		t.Run(tc.caFile, func(t *testing.T) {
			cfg := &TelemetryConfig{CAFile: tc.caFile}
			errs := Validate(cfg, "user personal")
			if tc.wantErr {
				if len(errs) == 0 {
					t.Fatalf("expected ca_file validation error")
				}
			} else if len(errs) > 0 {
				t.Fatalf("expected no ca_file errors, got %v", errs)
			}
		})
	}
}

func TestSchemaVersionAndAllowlist(t *testing.T) {
	if SchemaVersion != "1" {
		t.Fatalf("SchemaVersion = %q, want 1", SchemaVersion)
	}
	if SchemaMajor != 1 || SchemaMinor != 0 {
		t.Fatalf("SchemaMajor/Minor = %d/%d, want 1/0", SchemaMajor, SchemaMinor)
	}
	want := []ResourceAttribute{
		ResourceAttrSchemaVersion,
		ResourceAttrCLIVersion,
		ResourceAttrOSName,
		ResourceAttrArch,
		ResourceAttrCommandName,
		ResourceAttrHarnesses,
		ResourceAttrManaged,
		ResourceAttrOutcomeCode,
		ResourceAttrCoarseDuration,
		ResourceAttrInstallationID,
	}
	for _, attr := range want {
		if !slices.Contains(ResourceSchemaAttributes, attr) {
			t.Fatalf("expected %q in ResourceSchemaAttributes", attr)
		}
	}
	if len(ResourceSchemaAttributes) != len(want) {
		t.Fatalf("ResourceSchemaAttributes length = %d, want %d", len(ResourceSchemaAttributes), len(want))
	}
	if !reflect.DeepEqual(ResourceSchemaAttributes, want) {
		t.Fatalf("ResourceSchemaAttributes order mismatch: got %v, want %v", ResourceSchemaAttributes, want)
	}
}
