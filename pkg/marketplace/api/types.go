package api

// Capabilities is the well-known capability block returned by a Marketplace
// instance. Clients use it to negotiate API version, artifact schema version,
// supported resource kinds, and acquisition modes.
type Capabilities struct {
	APIVersions            []string `json:"api_versions"`
	ArtifactSchemaVersions []string `json:"artifact_schema_versions"`
	ResourceKinds          []string `json:"resource_kinds"`
	AcquisitionModes       []string `json:"acquisition_modes"`
	Features               []string `json:"features"`
}

// WellKnown is the response payload for `/.well-known/agent-marketplace.json`.
type WellKnown struct {
	InstanceID   string `json:"instance_id"`
	CanonicalURL string `json:"canonical_url"`
	APIBase      string `json:"api_base"`
	BrowserRoute string `json:"browser_route_template"`
	Capabilities `json:",inline"`
}

// Instance describes a Marketplace endpoint as configured by a consumer.
type Instance struct {
	URL        string `json:"url"`
	InstanceID string `json:"instance_id"`
	Alias      string `json:"alias,omitempty"`
}

// ResourceListItem is a single resource returned by the listing endpoints.
type ResourceListItem struct {
	SourceID      string            `json:"source_id"`
	SourceSlug    string            `json:"source_slug,omitempty"`
	Kind          string            `json:"kind"`
	Name          string            `json:"name"`
	Revision      string            `json:"revision"`
	ContentDigest string            `json:"content_digest"`
	Description   string            `json:"description,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	Score         float64           `json:"score,omitempty"`
}

// ArtifactRequest is the body of `POST /api/v1/artifacts/{kind}/{name}` when
// asking the server to produce an artifact for a specific revision.
type ArtifactRequest struct {
	Revision      string `json:"revision"`
	ContentDigest string `json:"content_digest,omitempty"`
}
