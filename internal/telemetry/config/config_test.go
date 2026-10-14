package telemetryconfig

import (
	"errors"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// spec: configuration-settings/telemetry-endpoint-credentials-are-not-logged
func TestTelemetryURLValidationErrorsRedactCredentials(t *testing.T) {
	for _, tc := range []struct {
		name  string
		check func() error
		host  string
		parse bool
	}{
		{"syntax parse", func() error {
			return ValidateEndpointSyntax("https://am-user:am-secret@collector.example/otlp\x7f?token=am-query-secret")
		}, "collector.example/otlp", true},
		{"syntax scheme", func() error {
			return ValidateEndpointSyntax("ftp://am-user:am-secret@collector.example/otlp?token=am-query-secret")
		}, "collector.example/otlp", false},
		{"transport parse", func() error {
			return ValidateEndpoint("https://am-user:am-secret@collector.example/otlp\x7f?token=am-query-secret", false)
		}, "collector.example/otlp", true},
		{"transport HTTPS", func() error {
			return ValidateEndpoint("http://am-user:am-secret@203.0.113.10/otlp?token=am-query-secret", false)
		}, "203.0.113.10/otlp", false},
		{"retention parse", func() error {
			return ValidateRetentionPolicy("https://am-user:am-secret@policy.example/retention\x7f?token=am-query-secret")
		}, "policy.example/retention", true},
		{"retention scheme", func() error {
			return ValidateRetentionPolicy("ftp://am-user:am-secret@policy.example/retention?token=am-query-secret")
		}, "policy.example/retention", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.check()
			if err == nil {
				t.Fatal("invalid telemetry URL accepted")
			}
			message := err.Error()
			if strings.Contains(message, "am-user") || strings.Contains(message, "am-secret") || strings.Contains(message, "am-query-secret") || !strings.Contains(message, tc.host) {
				t.Fatalf("error = %q, want credentials removed and %s retained", message, tc.host)
			}
			var parseErr *url.Error
			if errors.As(err, &parseErr) != tc.parse {
				t.Fatalf("errors.As(*url.Error) = %v, want %v", !tc.parse, tc.parse)
			}
		})
	}
}

// spec: configuration-settings/untrusted-log-values-cannot-create-records
func TestEndpointValidationEscapesLogValues(t *testing.T) {
	oldLookup, oldLogger := LookupIP, slog.Default()
	t.Cleanup(func() { LookupIP = oldLookup; slog.SetDefault(oldLogger) })
	LookupIP = func(string) ([]net.IP, error) { return nil, errors.New("lookup failed\r\nforged record") }
	var logged string
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			if attr.Key == "error" {
				logged = attr.Value.String()
			}
			return attr
		},
	})))
	if err := ValidateEndpoint("http://private.example", true); err != nil {
		t.Fatal(err)
	}
	if logged != `lookup failed\r\nforged record` {
		t.Fatalf("log attribute = %q, want escaped line breaks", logged)
	}
}

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

func TestValidateEmptyTelemetry(t *testing.T) {
	tc := &TelemetryConfig{}
	if errs := Validate(tc, "organization"); len(errs) > 0 {
		t.Fatalf("expected empty telemetry config to be valid, got %v", errs)
	}
}

func TestValidateOffValidatesPopulatedSettings(t *testing.T) {
	tc := &TelemetryConfig{
		Level:           " OFF ",
		Endpoint:        "not a URL",
		RetentionPolicy: "not a URL",
		CAFile:          filepath.Join(t.TempDir(), "missing-ca.pem"),
		Headers: map[string]HeaderSource{
			"Authorization": {Env: "TOKEN", File: filepath.Join(t.TempDir(), "missing-header")},
		},
	}
	if errs := Validate(tc, "user personal"); len(errs) == 0 {
		t.Fatal("expected populated settings in disabled telemetry to be validated")
	}
}

func TestValidateRejectsRepoAndLocalLayers(t *testing.T) {
	tc := &TelemetryConfig{Level: LevelBasic}
	for _, layer := range []string{"", "user", "repository committed", "repository personal", "repo", "local"} {
		t.Run(layer, func(t *testing.T) {
			errs := Validate(tc, layer)
			if len(errs) == 0 {
				t.Fatalf("expected layer %q to reject telemetry", layer)
			}
		})
	}
}

func TestValidateEndpoint(t *testing.T) {
	old := LookupIP
	defer func() { LookupIP = old }()

	LookupIP = func(host string) ([]net.IP, error) {
		switch host {
		case "private.local":
			return []net.IP{net.ParseIP("10.0.0.5")}, nil
		case "public.local":
			return []net.IP{net.ParseIP("203.0.113.1")}, nil
		case "mixed.local":
			return []net.IP{net.ParseIP("10.0.0.5"), net.ParseIP("203.0.113.1")}, nil
		case "unresolvable.local":
			return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		case "linklocal.local":
			return []net.IP{net.ParseIP("169.254.1.1")}, nil
		case "loopback.local":
			return []net.IP{net.ParseIP("127.0.0.1")}, nil
		default:
			return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		}
	}

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
		{"http link local allowed", "http://169.254.1.1", true, false},
		{"http public", "http://otel.example.com", false, true},
		{"invalid scheme", "ftp://otel.example.com", false, true},
		{"missing host", "https:///path", false, true},
		{"http private hostname allowed", "http://private.local", true, false},
		{"http private hostname public", "http://public.local", true, true},
		{"http private hostname mixed", "http://mixed.local", true, true},
		{"http private hostname unresolved", "http://unresolvable.local", true, false},
		{"http link-local hostname allowed", "http://linklocal.local", true, false},
		{"http loopback hostname allowed", "http://loopback.local", true, false},
		{"http hostname without allow insecure", "http://private.local", false, true},
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

func TestValidateEndpointForDeliveryRejectsUnresolvedHostname(t *testing.T) {
	old := LookupIP
	defer func() { LookupIP = old }()
	LookupIP = func(host string) ([]net.IP, error) {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}

	if err := ValidateEndpoint("http://unresolvable.local", true); err != nil {
		t.Fatalf("configuration validation should defer DNS failure: %v", err)
	}
	if err := ValidateEndpointForDelivery("http://unresolvable.local", true); err == nil {
		t.Fatal("delivery validation should reject an unresolved insecure hostname")
	}
}

func TestValidateOrganizationAllowsDefaultRetention(t *testing.T) {
	tc := &TelemetryConfig{
		Level:    LevelBasic,
		Endpoint: "https://otel.example.com",
	}
	if errs := Validate(tc, "organization"); len(errs) > 0 {
		t.Fatalf("expected organization endpoint without retention policy to use the default, got %v", errs)
	}

	tc.RetentionPolicy = "https://example.com/telemetry-retention"
	if errs := Validate(tc, "organization"); len(errs) > 0 {
		t.Fatalf("expected valid org telemetry with retention policy, got %v", errs)
	}
}

func TestValidateHeaders(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	existing := filepath.Join(dir, "tenant")
	if err := os.WriteFile(existing, []byte("tenant-a"), 0o600); err != nil {
		t.Fatal(err)
	}

	fileWithNewline := filepath.Join(dir, "tenant-nl")
	if err := os.WriteFile(fileWithNewline, []byte("tenant-a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fileWithInternalNewline := filepath.Join(dir, "tenant-internal-nl")
	if err := os.WriteFile(fileWithInternalNewline, []byte("tenant\na"), 0o600); err != nil {
		t.Fatal(err)
	}

	badPerm := filepath.Join(dir, "tenant-bad")
	if err := os.WriteFile(badPerm, []byte("tenant-b"), 0o644); err != nil {
		t.Fatal(err)
	}

	linkTarget := filepath.Join(dir, "tenant-link-target")
	if err := os.WriteFile(linkTarget, []byte("tenant-c"), 0o600); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(dir, "tenant-symlink")
	if err := os.Symlink(linkTarget, symlink); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	badParent := filepath.Join(dir, "badparent", "tenant")
	if err := os.Mkdir(filepath.Dir(badParent), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(badParent, []byte("tenant-d"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("EXAMPLE_OTEL_AUTHORIZATION", "token-123")
	t.Setenv("OTEL_AUTH_WITH_NL", "token\n123")

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
			wantErr: false,
		},
		{
			name: "env value with line terminator",
			headers: map[string]HeaderSource{
				"Authorization": {Env: "OTEL_AUTH_WITH_NL"},
			},
			wantErr: true,
		},
		{
			name: "file value with trailing newline",
			headers: map[string]HeaderSource{
				"X-Tenant": {File: fileWithNewline},
			},
		},
		{
			name: "file value with internal newline",
			headers: map[string]HeaderSource{
				"X-Tenant": {File: fileWithInternalNewline},
			},
			wantErr: true,
		},
		{
			name: "file with bad permissions",
			headers: map[string]HeaderSource{
				"X-Tenant": {File: badPerm},
			},
			wantErr: true,
		},
		{
			name: "file parent with bad permissions",
			headers: map[string]HeaderSource{
				"X-Tenant": {File: badParent},
			},
			wantErr: true,
		},
		{
			name: "file symlink",
			headers: map[string]HeaderSource{
				"X-Tenant": {File: symlink},
			},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &TelemetryConfig{Level: LevelBasic, Headers: tc.headers}
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
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	cert := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(cert, []byte("cert\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	badPerm := filepath.Join(dir, "ca-bad.pem")
	if err := os.WriteFile(badPerm, []byte("cert\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	linkTarget := filepath.Join(dir, "ca-link-target.pem")
	if err := os.WriteFile(linkTarget, []byte("cert\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(dir, "ca-symlink.pem")
	if err := os.Symlink(linkTarget, symlink); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	tests := []struct {
		caFile  string
		wantErr bool
	}{
		{cert, false},
		{filepath.Join(dir, "missing"), false},
		{dir, true},
		{badPerm, true},
		{symlink, true},
	}
	for _, tc := range tests {
		t.Run(tc.caFile, func(t *testing.T) {
			cfg := &TelemetryConfig{Level: LevelBasic, CAFile: tc.caFile}
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
	if SchemaMajor != 1 || SchemaMinor != 2 {
		t.Fatalf("SchemaMajor/Minor = %d/%d, want 1/2", SchemaMajor, SchemaMinor)
	}
	want := []ResourceAttribute{
		ResourceAttrCommandName,
		ResourceAttrHarnesses,
		ResourceAttrManaged,
		ResourceAttrOutcomeCode,
	}
	for _, attr := range want {
		if !slices.Contains(MetricSchemaAttributes, attr) {
			t.Fatalf("expected %q in MetricSchemaAttributes", attr)
		}
	}
	if len(MetricSchemaAttributes) != len(want) {
		t.Fatalf("MetricSchemaAttributes length = %d, want %d", len(MetricSchemaAttributes), len(want))
	}
	if !reflect.DeepEqual(MetricSchemaAttributes, want) {
		t.Fatalf("MetricSchemaAttributes order mismatch: got %v, want %v", MetricSchemaAttributes, want)
	}
	if CommandCounterName != MetricNamespace+".command.invocations" {
		t.Fatalf("CommandCounterName = %q", CommandCounterName)
	}
	if CommandDurationName != MetricNamespace+".command.duration_ms" {
		t.Fatalf("CommandDurationName = %q", CommandDurationName)
	}
	if InventorySnapshotName != MetricNamespace+".config.snapshot" ||
		InventoryRegistryName != MetricNamespace+".config.registry" ||
		InventoryResourceName != MetricNamespace+".config.resource" ||
		InventoryOmissionsName != MetricNamespace+".config.inventory_omissions" {
		t.Fatalf("inventory instrument names = %q, %q, %q, %q", InventorySnapshotName, InventoryRegistryName, InventoryResourceName, InventoryOmissionsName)
	}
	if len(InventorySnapshotAttributes) != 5 || len(InventoryRegistryAttributes) != 10 || len(InventoryResourceAttributes) != 11 || len(InventoryOmissionAttributes) != 1 {
		t.Fatalf("inventory allowlist lengths = %d/%d/%d/%d", len(InventorySnapshotAttributes), len(InventoryRegistryAttributes), len(InventoryResourceAttributes), len(InventoryOmissionAttributes))
	}
	for _, attr := range []MetricAttribute{MetricAttribute("source.kind"), MetricAttribute("marketplace.id")} {
		if !slices.Contains(InventoryRegistryAttributes, attr) || !slices.Contains(InventoryResourceAttributes, attr) {
			t.Fatalf("hosted identity attribute %q missing from registry/resource allowlists", attr)
		}
		if slices.Contains(InventorySnapshotAttributes, attr) {
			t.Fatalf("hosted identity attribute %q was added to snapshot allowlist", attr)
		}
		if slices.Contains(MetricSchemaAttributes, ResourceAttribute(attr)) {
			t.Fatalf("hosted identity attribute %q was added to command metric allowlist", attr)
		}
	}
}
