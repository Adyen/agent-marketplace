package api

import (
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/publicurl"
)

// Marketplace API and artifact schema version constants.
const (
	APIVersionV1                     = "v1"
	ArtifactSchemaVersionV1          = "v1"
	ArtifactSchemaVersionV2          = "v2"
	AcquisitionMirror                = "mirror"
	AcquisitionIndexOnly             = "index-only"
	CatalogProjectionV1Alpha1Feature = "catalog-projection-v1alpha1"
)

// DefaultCapabilities returns the capability block advertised by this version
// of the agent-marketplace Marketplace server. It is the single source of truth
// for the values that also appear in `api/openapi.yaml`.
func DefaultCapabilities() Capabilities {
	return Capabilities{
		APIVersions:            []string{APIVersionV1},
		ArtifactSchemaVersions: []string{ArtifactSchemaVersionV2, ArtifactSchemaVersionV1},
		ResourceKinds:          SupportedResourceKinds,
		AcquisitionModes:       []string{AcquisitionIndexOnly, AcquisitionMirror},
		Features:               []string{"inspection", CatalogProjectionV1Alpha1Feature},
	}
}

// DefaultWellKnown returns the well-known response for a Marketplace instance.
func DefaultWellKnown(instanceID, canonicalURL string, browserBase ...string) WellKnown {
	wellKnown := WellKnown{
		InstanceID:   instanceID,
		CanonicalURL: publicurl.Sanitize(canonicalURL),
		APIBase:      "/api/v1",
		Capabilities: DefaultCapabilities(),
	}
	if len(browserBase) > 0 {
		wellKnown.BrowserBase = strings.TrimRight(publicurl.Sanitize(browserBase[0]), "/")
	}
	if wellKnown.BrowserBase != "" {
		wellKnown.BrowserLatestRoute = wellKnown.BrowserBase + "/r/{registry}/{kind}/{name}"
		wellKnown.BrowserExactRoute = wellKnown.BrowserBase + "/r/{registry}/{kind}/{name}/revisions/{revision}"
	}
	return wellKnown
}
