package api

import (
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestDefaultCapabilitiesMatchesSupportedKinds(t *testing.T) {
	caps := DefaultCapabilities()
	if !slices.Equal(caps.ResourceKinds, SupportedResourceKinds) {
		t.Fatalf("ResourceKinds = %v, want %v", caps.ResourceKinds, SupportedResourceKinds)
	}
}

func TestDefaultCapabilitiesAdvertisesV1(t *testing.T) {
	caps := DefaultCapabilities()
	if !slices.Contains(caps.APIVersions, APIVersionV1) {
		t.Fatalf("APIVersions %v does not contain %q", caps.APIVersions, APIVersionV1)
	}
	if !slices.Contains(caps.ArtifactSchemaVersions, ArtifactSchemaVersionV1) {
		t.Fatalf("ArtifactSchemaVersions %v does not contain %q", caps.ArtifactSchemaVersions, ArtifactSchemaVersionV1)
	}
}

// spec: marketplace-api/capabilities-document-declares-acquisition-modes-and-server-features
func TestDefaultCapabilitiesListsAcquisitionModes(t *testing.T) {
	caps := DefaultCapabilities()
	for _, want := range []string{AcquisitionIndexOnly, AcquisitionMirror} {
		if !slices.Contains(caps.AcquisitionModes, want) {
			t.Fatalf("AcquisitionModes %v does not contain %q", caps.AcquisitionModes, want)
		}
	}
}

// spec: marketplace-api/capabilities-document-declares-acquisition-modes-and-server-features
func TestDefaultCapabilitiesAdvertisesCatalogProjectionV1Alpha1(t *testing.T) {
	caps := DefaultCapabilities()
	if !slices.Contains(caps.Features, "catalog-projection-v1alpha1") {
		t.Fatalf("Features %v does not advertise the catalog projection contract", caps.Features)
	}
}

// spec: marketplace-api/well-known-document-exposes-browser-templates-only-when-configured
func TestDefaultWellKnownAdvertisesBrowserTemplatesOnlyWhenConfigured(t *testing.T) {
	withoutBrowser := DefaultWellKnown("mp", "https://api.example")
	if withoutBrowser.BrowserBase != "" || withoutBrowser.BrowserLatestRoute != "" || withoutBrowser.BrowserExactRoute != "" {
		t.Fatalf("well-known without browser base = %+v, want no browser fields", withoutBrowser)
	}

	withBrowser := DefaultWellKnown("mp", "https://api.example", "https://market.example/")
	if withBrowser.BrowserBase != "https://market.example" ||
		withBrowser.BrowserLatestRoute != "https://market.example/r/{registry}/{kind}/{name}" ||
		withBrowser.BrowserExactRoute != "https://market.example/r/{registry}/{kind}/{name}/revisions/{revision}" {
		t.Fatalf("well-known with browser base = %+v", withBrowser)
	}
}

func TestIsCanonicalSHA256Digest(t *testing.T) {
	valid := "sha256:" + strings.Repeat("a", 64)
	if !IsCanonicalSHA256Digest(valid) {
		t.Fatalf("valid digest rejected: %q", valid)
	}
	for _, invalid := range []string{
		"sha256:short",
		"sha256:" + "A" + valid[len("sha256:")+1:],
		"sha256:" + valid[len("sha256:"):len(valid)-1],
		"sha256:" + valid[len("sha256:"):] + "0",
		"sha512:" + valid[len("sha256:"):],
	} {
		if IsCanonicalSHA256Digest(invalid) {
			t.Errorf("invalid digest accepted: %q", invalid)
		}
	}
}

func TestIsCanonicalGitCommit(t *testing.T) {
	if !IsCanonicalGitCommit(strings.Repeat("a", 40)) {
		t.Fatal("full lowercase Git object ID rejected")
	}
	for _, invalid := range []string{"abc123", strings.Repeat("A", 40), strings.Repeat("a", 39), strings.Repeat("a", 41), strings.Repeat("g", 40)} {
		if IsCanonicalGitCommit(invalid) {
			t.Errorf("non-canonical commit accepted: %q", invalid)
		}
	}
}

// spec: marketplace-api/selectable-resource-kinds
func TestIsMarketplaceKind(t *testing.T) {
	if want := []string{"skill", "command", "subagent", "hook", "mcpserver", "collection"}; !slices.Equal(SupportedResourceKinds, want) {
		t.Fatalf("SupportedResourceKinds = %v, want %v", SupportedResourceKinds, want)
	}
	for _, kind := range SupportedResourceKinds {
		if !IsMarketplaceKind(kind) {
			t.Errorf("IsMarketplaceKind(%q) = false, want true", kind)
		}
	}
	for _, kind := range []string{"rule", "plugin", "", "SKILL"} {
		if IsMarketplaceKind(kind) {
			t.Errorf("IsMarketplaceKind(%q) = true, want false", kind)
		}
	}
}

// spec: marketplace-api/bidirectional-kind-conversion
func TestKindIRConversion(t *testing.T) {
	pairs := []struct {
		kind string
		ir   ir.ResourceKind
	}{
		{"skill", ir.KindSkill},
		{"command", ir.KindCommand},
		{"subagent", ir.KindSubagent},
		{"hook", ir.KindHook},
		{"mcpserver", ir.KindMcpServer},
	}
	for _, p := range pairs {
		if got, ok := ToIR(p.kind); !ok || got != p.ir {
			t.Errorf("ToIR(%q) = %v, %v; want %v, true", p.kind, got, ok, p.ir)
		}
		if got := FromIR(p.ir); got != p.kind {
			t.Errorf("FromIR(%v) = %q, want %q", p.ir, got, p.kind)
		}
	}
	for _, kind := range []string{"collection", "rule", "", "Skill"} {
		if got, ok := ToIR(kind); ok {
			t.Errorf("ToIR(%q) = %v, true; want false", kind, got)
		}
	}
}
