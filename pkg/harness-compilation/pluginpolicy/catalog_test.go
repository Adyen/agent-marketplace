package pluginpolicy

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

const validFormat = `schema: 1
format: claude-plugin
role: package
evidence:
  checked: "2026-10-15"
  refs: [synthetic-test-fixture]
versions: []
manifest:
  fields:
    name: {required: true, type: string, invalid: refuse-package}
components:
  skills: immediate-children
dependencies: refuse-package
`

var validAgentFormat = strings.Replace(validFormat,
	"format: claude-plugin", "format: agent-plugin", 1)

const validCatalogFormat = `schema: 1
format: agent-marketplace
role: catalog
evidence:
  checked: "2026-10-15"
  refs: [synthetic-test-fixture]
versions: []
manifest:
  fields:
    name: {required: true, type: string, invalid: refuse-package}
components:
  skills: immediate-children
catalog:
  sources:
    path: acquire
dependencies: refuse-package
`

const validHarness = `schema: 1
harness: devin
evidence:
  checked: "2026-10-15"
  refs: [synthetic-test-fixture]
accepts: [claude-plugin]
delivery:
  decompose: {status: verified}
  native: {status: unverified}
`

func loadAll(t *testing.T, format, harness string, probes []string) error {
	formats := map[string]string{}
	if format != "" {
		formats["claude-plugin"] = format
	}
	return loadAllFormats(t, formats, harness, probes)
}

func loadAllFormats(t *testing.T, formats map[string]string, harness string, probes []string) error {
	t.Helper()
	files := fstest.MapFS{}
	for name, data := range formats {
		files["formats/"+name+".yaml"] = &fstest.MapFile{Data: []byte(data)}
	}
	if harness != "" {
		files["catalogs/devin.yaml"] = &fstest.MapFile{Data: []byte(harness)}
	}
	_, err := loadCatalogs(files, "formats/*.yaml", "catalogs/*.yaml", probes)
	return err
}

// spec: plugin-policy-catalogs/every-harness-target-and-package-format-ships-a-plugin-catalog
func TestShippedCatalogsValidate(t *testing.T) {
	if err := ValidateCatalogs([]string{ClaudeNativePluginProbeID}); err != nil {
		t.Fatal(err)
	}
}

// spec: plugin-policy-catalogs/every-harness-target-and-package-format-ships-a-plugin-catalog
func TestEveryTargetShipsACatalog(t *testing.T) {
	r, err := loadCatalogs(catalogFiles, "formats/*.yaml", "catalogs/*.yaml", []string{ClaudeNativePluginProbeID})
	if err != nil {
		t.Fatal(err)
	}
	for _, tg := range target.Targets() {
		if _, ok := r.catalogs[tg]; !ok {
			t.Fatalf("no plugin catalog for target %s", tg)
		}
	}
	want := map[string]bool{
		"claude-plugin": true, "factory-plugin": true, "codex-plugin": true,
		"agent-plugin": true, "claude-marketplace": true,
		"factory-marketplace": true, "codex-marketplace": true,
	}
	for format := range want {
		if _, ok := r.formats[format]; !ok {
			t.Fatalf("no plugin catalog for format %s", format)
		}
	}
	if len(r.formats) != len(want) {
		t.Fatalf("formats = %v, want exactly %d", r.formats, len(want))
	}
	layouts := target.PackageLayouts()
	if len(layouts) != len(r.formats) {
		t.Fatalf("package layouts = %v, want one per format %v", layouts, r.formats)
	}
	for format := range r.formats {
		if _, ok := layouts[format]; !ok {
			t.Fatalf("no package layout for format %s", format)
		}
	}
}

// spec: plugin-policy-catalogs/every-harness-target-and-package-format-ships-a-plugin-catalog
func TestDecomposeDeliveryVerifiedEverywhere(t *testing.T) {
	r, err := loadCatalogs(catalogFiles, "formats/*.yaml", "catalogs/*.yaml", []string{ClaudeNativePluginProbeID})
	if err != nil {
		t.Fatal(err)
	}
	for _, tg := range target.Targets() {
		c := r.catalogs[tg]
		if c.Delivery.Decompose.Status != DeliveryVerified {
			t.Fatalf("%s decompose status = %q, want verified", tg, c.Delivery.Decompose.Status)
		}
		if tg == target.TargetClaude {
			if c.Delivery.Native.Status != DeliveryVerified || c.Delivery.Native.Format != "claude-plugin" || c.Delivery.Native.Probe != "claude-native-plugin" {
				t.Fatalf("Claude native delivery = %+v, want verified claude-plugin with claude-native-plugin", c.Delivery.Native)
			}
			continue
		}
		if c.Delivery.Native.Status != DeliveryUnverified {
			t.Fatalf("%s native status = %q, want unverified", tg, c.Delivery.Native.Status)
		}
	}
}

// spec: plugin-policy-catalogs/shipped-claude-catalog-delivers-claude-plugin-packages-natively
func TestClaudeNativeDeliveryCatalogRequiresItsDefinedProbe(t *testing.T) {
	if err := ValidateCatalogs([]string{ClaudeNativePluginProbeID}); err != nil {
		t.Fatalf("Claude catalog rejected its defined probe: %v", err)
	}
	policy, err := LookupNativeDelivery(target.TargetClaude)
	if err != nil {
		t.Fatal(err)
	}
	if policy.Status != DeliveryVerified || policy.Format != "claude-plugin" || policy.Probe != "claude-native-plugin" || policy.Refresh != "restart-required" {
		t.Fatalf("Claude native delivery policy = %+v, want verified claude-plugin with claude-native-plugin and restart-required refresh", policy)
	}
	if err := ValidateCatalogs(nil); err == nil || !strings.Contains(err.Error(), ClaudeNativePluginProbeID) {
		t.Fatalf("catalog validation without native probe = %v, want undefined-probe failure", err)
	}
}

// spec: plugin-policy-catalogs/malformed-plugin-catalogs-fail-the-check
func TestMalformedCatalogsRejected(t *testing.T) {
	for _, tc := range []struct {
		name    string
		format  string
		harness string
		want    string
	}{
		{
			name:    "unknown key in format catalog",
			format:  strings.Replace(validFormat, "versions: []", "bogus_field: 1\nversions: []", 1),
			harness: validHarness,
			want:    "bogus_field",
		},
		{
			name:    "duplicate key in format catalog",
			format:  strings.Replace(validFormat, "schema: 1", "schema: 1\nschema: 1", 1),
			harness: validHarness,
			want:    "duplicate",
		},
		{
			name:    "field outcome outside the allowed set",
			format:  strings.Replace(validFormat, "invalid: refuse-package", "invalid: shred-package", 1),
			harness: validHarness,
			want:    "shred-package",
		},
		{
			name:    "blocking outcome without a diagnostic code",
			harness: strings.Replace(validHarness, "delivery:", "surfaces:\n  plugin:\n    skill:\n      permissions: {block-member: true}\ndelivery:", 1),
			format:  validFormat,
			want:    "block-member",
		},
		{
			name: "unknown policy reference",
			format: strings.Replace(strings.Replace(validFormat,
				"role: package", "role: catalog", 1),
				"dependencies: refuse-package", "catalog:\n  entry_overlay: entry-overlay.bogus\n  sources:\n    path: acquire\ndependencies: refuse-package", 1),
			harness: validHarness,
			want:    "entry-overlay.bogus",
		},
		{
			name:    "unknown format reference in accepts",
			format:  validFormat,
			harness: strings.Replace(validHarness, "accepts: [claude-plugin]", "accepts: [bogus-plugin]", 1),
			want:    "bogus-plugin",
		},
		{
			name:    "missing evidence refs",
			format:  strings.Replace(validFormat, "  refs: [synthetic-test-fixture]\n", "", 1),
			harness: validHarness,
			want:    "evidence",
		},
		{
			name:    "catalog role without catalog block",
			format:  strings.Replace(validFormat, "role: package", "role: catalog", 1),
			harness: validHarness,
			want:    "catalog",
		},
		{
			name:    "unknown component discovery rule",
			format:  strings.Replace(validFormat, "skills: immediate-children", "skills: recursively", 1),
			harness: validHarness,
			want:    "recursively",
		},
		{
			name:    "dependencies outcome outside refuse-package",
			format:  strings.Replace(validFormat, "dependencies: refuse-package", "dependencies: install", 1),
			harness: validHarness,
			want:    "dependencies",
		},
		{
			name:    "unknown surface outcome",
			format:  validFormat,
			harness: strings.Replace(validHarness, "delivery:", "surfaces:\n  plugin:\n    skill:\n      name: erase\ndelivery:", 1),
			want:    "erase",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := loadAll(t, tc.format, tc.harness, nil)
			if err == nil {
				t.Fatalf("invalid catalog accepted; want failure containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want mention of %q", err, tc.want)
			}
		})
	}
}

// spec: plugin-policy-catalogs/verified-native-delivery-without-a-probe-fails-the-check
func TestVerifiedNativeDeliveryProbeRequired(t *testing.T) {
	t.Run("verified without a probe", func(t *testing.T) {
		harness := strings.Replace(validHarness,
			"native: {status: unverified}",
			"native: {status: verified, format: claude-plugin}", 1)
		err := loadAll(t, validFormat, harness, []string{"probe-x"})
		if err == nil || !strings.Contains(err.Error(), "probe") {
			t.Fatalf("error = %v, want a probe failure", err)
		}
	})
	t.Run("verified with an undefined probe", func(t *testing.T) {
		harness := strings.Replace(validHarness,
			"native: {status: unverified}",
			"native: {status: verified, format: claude-plugin, probe: probe-undefined}", 1)
		err := loadAll(t, validFormat, harness, []string{"probe-x"})
		if err == nil || !strings.Contains(err.Error(), "probe-undefined") {
			t.Fatalf("error = %v, want a probe-undefined failure", err)
		}
	})
	t.Run("verified with a defined probe passes", func(t *testing.T) {
		harness := strings.Replace(validHarness,
			"native: {status: unverified}",
			"native: {status: verified, format: claude-plugin, probe: probe-x}", 1)
		if err := loadAll(t, validFormat, harness, []string{"probe-x"}); err != nil {
			t.Fatalf("defined probe rejected: %v", err)
		}
	})
}

// spec: plugin-policy-catalogs/verified-native-delivery-for-an-unaccepted-format-fails-the-check
func TestVerifiedNativeDeliveryForUnacceptedFormatFails(t *testing.T) {
	native := func(format string) string {
		return strings.Replace(validHarness,
			"native: {status: unverified}",
			"native: {status: verified, format: "+format+", probe: probe-x}", 1)
	}
	for _, tc := range []struct {
		name    string
		formats map[string]string
		format  string
	}{
		{
			name:    "package format outside accepts",
			formats: map[string]string{"claude-plugin": validFormat, "agent-plugin": validAgentFormat},
			format:  "agent-plugin",
		},
		{
			name:    "catalog-role format",
			formats: map[string]string{"claude-plugin": validFormat, "agent-marketplace": validCatalogFormat},
			format:  "agent-marketplace",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := loadAllFormats(t, tc.formats, native(tc.format), []string{"probe-x"})
			if err == nil || !strings.Contains(err.Error(), "does not accept") {
				t.Fatalf("error = %v, want a does-not-accept failure", err)
			}
			if !strings.Contains(err.Error(), tc.format) {
				t.Fatalf("error = %v, want mention of %q", err, tc.format)
			}
		})
	}
}

func TestNativeRefreshPolicyIsClosedEnum(t *testing.T) {
	verified := strings.Replace(validHarness,
		"native: {status: unverified}",
		"native: {status: verified, format: claude-plugin, probe: probe-x, refresh: restart-required}", 1)
	if err := loadAll(t, validFormat, verified, []string{"probe-x"}); err != nil {
		t.Fatalf("valid restart-required refresh policy rejected: %v", err)
	}
	invalid := strings.Replace(verified, "refresh: restart-required", "refresh: later", 1)
	if err := loadAll(t, validFormat, invalid, []string{"probe-x"}); err == nil || !strings.Contains(err.Error(), "refresh") {
		t.Fatalf("invalid refresh policy accepted or omitted refresh context: %v", err)
	}
}
