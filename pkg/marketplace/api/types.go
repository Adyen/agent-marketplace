package api

import (
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func IsCanonicalSHA256Digest(value string) bool {
	if len(value) != len("sha256:")+64 || value[:len("sha256:")] != "sha256:" {
		return false
	}
	for i := len("sha256:"); i < len(value); i++ {
		c := value[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// IsCanonicalGitCommit reports whether value is the full, lowercase hexadecimal
// object ID emitted by git for a commit. Short IDs and symbolic/development
// labels are intentionally not valid public provenance.
func IsCanonicalGitCommit(value string) bool {
	if len(value) != 40 {
		return false
	}
	for i := range value {
		c := value[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

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
	InstanceID         string `json:"instance_id"`
	Version            string `json:"version"`
	CanonicalURL       string `json:"canonical_url"`
	APIBase            string `json:"api_base"`
	BrowserBase        string `json:"browser_base,omitempty"`
	BrowserLatestRoute string `json:"browser_latest_route_template,omitempty"`
	BrowserExactRoute  string `json:"browser_exact_route_template,omitempty"`
	Capabilities       `json:",inline"`
}

// ExactRegistryOccurrence is the immutable registry occurrence selected for an
// index-only install. AcquisitionOrigin is the current operator-approved Git
// origin; clients must apply their local allowed_registries policy before use.
type ExactRegistryOccurrence struct {
	RegistryID                     store.RegistryID      `json:"registry_id"`
	Ref                            string                `json:"ref"`
	Roots                          []store.RegistryRoot  `json:"roots"`
	Commit                         string                `json:"commit"`
	RegistryRevisionManifestDigest store.RevisionDigest  `json:"registry_revision_manifest_digest"`
	Acquisition                    store.AcquisitionMode `json:"acquisition"`
	AcquisitionOrigin              string                `json:"acquisition_origin"`
	OccurredAt                     time.Time             `json:"occurred_at"`
}

// Instance describes a Marketplace endpoint as configured by a consumer.
type Instance struct {
	URL        string `json:"url"`
	InstanceID string `json:"instance_id"`
	Alias      string `json:"alias,omitempty"`
}

// Availability tells a client whether a listed resource can be installed from
// this instance, without a per-resource detail fetch.
//
// It is a string enum rather than an `installable` boolean because the reason
// matters to the caller: a policy-blocked resource is a permanent property of
// that revision, while a revoked one is an operator decision that can be
// reversed, and a list card renders them differently. New states can be added
// without adding another field.
type Availability string

const (
	// AvailabilityAvailable means the revision is installable from this
	// instance.
	AvailabilityAvailable Availability = "available"

	// AvailabilityBlocked means the revision is listed for transparency but is
	// not installable for a reason an older peer did not distinguish.
	AvailabilityBlocked Availability = "blocked"

	// AvailabilityPolicyBlocked means the current assessment rejected the
	// revision, so its artifact cannot be served.
	AvailabilityPolicyBlocked Availability = "policy-blocked"

	// AvailabilityOperatorExposed means the assessment remains rejected but
	// the Marketplace operator explicitly permits distribution.
	AvailabilityOperatorExposed Availability = "operator-exposed"

	// AvailabilityNoArtifact means a mirrored revision has no artifact that
	// this instance can serve for installation.
	AvailabilityNoArtifact Availability = "no-artifact"

	// AvailabilityRevoked means an operator revoked the revision.
	AvailabilityRevoked Availability = "revoked"
)

// CatalogAPIVersionV1Alpha1 identifies the versioned, catalog-neutral
// projection returned by GET /api/v1/catalog.
const CatalogAPIVersionV1Alpha1 = "agent-marketplace.io/catalog/v1alpha1"

// CatalogKeysetCursor is an opaque, stateless cursor for the final immutable
// resource coordinate on the preceding catalog page. Clients must not inspect
// or construct its value.
type CatalogKeysetCursor string

// CatalogETag is the strong, quoted validator for all catalog-visible current
// state. It is an HTTP header value, not a resource revision digest.
type CatalogETag string

// CatalogPage is one stateless keyset page from GET /api/v1/catalog. A scan
// starts without a cursor and continues until Complete is true. NextCursor is
// omitted on the final page.
type CatalogPage struct {
	APIVersion     string              `json:"api_version"`
	MarketplaceID  string              `json:"marketplace_id"`
	CatalogVersion string              `json:"catalog_version"`
	Complete       bool                `json:"complete"`
	NextCursor     CatalogKeysetCursor `json:"next_cursor,omitempty"`
	Resources      []CatalogResource   `json:"resources"`
}

// CatalogResource is one current catalog-visible Marketplace resource. Its
// identity is registry ID, kind, and registry-local name; neither registry
// slug, source provenance, nor revision changes that identity.
type CatalogResource struct {
	RegistryID   string             `json:"registry_id"`
	RegistrySlug string             `json:"registry_slug"`
	Kind         string             `json:"kind"`
	Name         string             `json:"name"`
	Description  string             `json:"description,omitempty"`
	Revision     string             `json:"revision"`
	License      string             `json:"license,omitempty"`
	Availability Availability       `json:"availability"`
	Source       *CatalogSource     `json:"source,omitempty"`
	Collection   *CatalogCollection `json:"collection,omitempty"`
}

// CatalogSource records sanitized immutable repository provenance for a
// catalog resource. It is absent for legacy occurrences that lack trustworthy
// provenance.
type CatalogSource struct {
	RepositoryURL string `json:"repository_url"`
	Path          string `json:"path"`
	Commit        string `json:"commit"`
}

// CatalogCollection carries only direct collection relations. Resources and
// Includes are never recursively flattened; unresolved declarations remain
// explicit rather than becoming dangling coordinates.
type CatalogCollection struct {
	Resources            []CatalogCollectionResourceReference `json:"resources"`
	Includes             []CatalogCollectionIncludeReference  `json:"includes"`
	UnresolvedReferences []CatalogUnresolvedReference         `json:"unresolved_references"`
}

// CatalogCollectionResourceReference is a resolved direct collection resource
// member. It identifies a catalog-visible resource by immutable Marketplace
// coordinate.
type CatalogCollectionResourceReference struct {
	RegistryID string `json:"registry_id"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
}

// CatalogCollectionIncludeReference is a resolved direct collection include.
// Kind is always "collection" on the wire.
type CatalogCollectionIncludeReference struct {
	RegistryID string `json:"registry_id"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
}

// CatalogUnresolvedReference preserves a direct collection declaration that
// cannot be safely resolved. Reason is a stable machine-readable reason code.
type CatalogUnresolvedReference struct {
	ReferenceType string `json:"reference_type"`
	RegistryID    string `json:"registry_id,omitempty"`
	Kind          string `json:"kind"`
	Name          string `json:"name"`
	Reason        string `json:"reason"`
}

// ResourceListItem is a single resource returned by the listing endpoints.
type ResourceListItem struct {
	RegistryID    string `json:"registry_id"`
	RegistrySlug  string `json:"registry_slug,omitempty"`
	Kind          string `json:"kind"`
	Name          string `json:"name"`
	Revision      string `json:"revision"`
	ContentDigest string `json:"content_digest"`
	Description   string `json:"description,omitempty"`
	// Availability is always populated by this server. It stays omitempty so
	// the field remains optional in the contract and a peer implementation
	// that cannot determine availability may leave it out.
	Availability Availability      `json:"availability,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	Score        float64           `json:"score,omitempty"`
}

// ArtifactRequest is the body of `POST /api/v1/artifacts/{kind}/{name}` when
// asking the server to produce an artifact for a specific revision.
type ArtifactRequest struct {
	Revision      string `json:"revision"`
	ContentDigest string `json:"content_digest,omitempty"`
}
