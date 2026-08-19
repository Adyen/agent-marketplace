package api

import (
	"slices"
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
	for _, want := range []string{AcquisitionIndex, AcquisitionMirror} {
		if !slices.Contains(caps.AcquisitionModes, want) {
			t.Fatalf("AcquisitionModes %v does not contain %q", caps.AcquisitionModes, want)
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
