package api

// Marketplace API and artifact schema version constants.
const (
	APIVersionV1            = "v1"
	ArtifactSchemaVersionV1 = "v1"
	AcquisitionMirror       = "mirror"
	AcquisitionIndex        = "index-only"
)

// DefaultCapabilities returns the capability block advertised by this version
// of the agent-marketplace Marketplace server. It is the single source of truth
// for the values that also appear in `api/openapi.yaml`.
func DefaultCapabilities() Capabilities {
	return Capabilities{
		APIVersions:            []string{APIVersionV1},
		ArtifactSchemaVersions: []string{ArtifactSchemaVersionV1},
		ResourceKinds:          SupportedResourceKinds,
		AcquisitionModes:       []string{AcquisitionIndex, AcquisitionMirror},
		Features:               []string{},
	}
}

// DefaultWellKnown returns the well-known response for a Marketplace instance.
func DefaultWellKnown(instanceID, canonicalURL string) WellKnown {
	return WellKnown{
		InstanceID:   instanceID,
		CanonicalURL: canonicalURL,
		APIBase:      "/api/v1",
		BrowserRoute: "/s/{source}/{kind}/{name}",
		Capabilities: DefaultCapabilities(),
	}
}
