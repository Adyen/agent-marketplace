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

	// AvailabilityNoArtifact means a mirrored revision has no artifact that
	// this instance can serve for installation.
	AvailabilityNoArtifact Availability = "no-artifact"

	// AvailabilityRevoked means an operator revoked the revision.
	AvailabilityRevoked Availability = "revoked"
)

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
