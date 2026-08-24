package api

import (
	"slices"
	"strings"
	"testing"
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

func TestDefaultCapabilitiesListsAcquisitionModes(t *testing.T) {
	caps := DefaultCapabilities()
	for _, want := range []string{AcquisitionIndexOnly, AcquisitionMirror} {
		if !slices.Contains(caps.AcquisitionModes, want) {
			t.Fatalf("AcquisitionModes %v does not contain %q", caps.AcquisitionModes, want)
		}
	}
}

func TestDefaultWellKnownAdvertisesBrowserTemplatesOnlyWhenConfigured(t *testing.T) {
	withoutBrowser := DefaultWellKnown("mp", "https://api.example")
	if withoutBrowser.BrowserBase != "" || withoutBrowser.BrowserLatestRoute != "" || withoutBrowser.BrowserExactRoute != "" {
		t.Fatalf("well-known without browser base = %+v, want no browser fields", withoutBrowser)
	}

	withBrowser := DefaultWellKnown("mp", "https://api.example", "https://market.example/")
	if withBrowser.BrowserBase != "https://market.example" ||
		withBrowser.BrowserLatestRoute != "https://market.example/s/{source}/{kind}/{name}" ||
		withBrowser.BrowserExactRoute != "https://market.example/s/{source}/{kind}/{name}/revisions/{revision}" {
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

func TestIsMarketplaceKind(t *testing.T) {
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
