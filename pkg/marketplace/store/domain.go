package store

import (
	"fmt"
	"strings"
	"time"
)

// Strong ID types for the Marketplace domain.
type (
	// MarketplaceID identifies a Marketplace deployment instance.
	MarketplaceID string

	// SourceID identifies an approved source lineage. It is immutable and never
	// reassigned, even when the source slug or origin changes.
	SourceID string

	// RevisionDigest is the content-addressed digest of a canonical resource
	// payload.
	RevisionDigest string

	// ArchiveDigest is the content-addressed digest of a complete resource
	// artifact archive.
	ArchiveDigest string

	// JobID uniquely identifies a durable job.
	JobID string
)

func (m MarketplaceID) String() string  { return string(m) }
func (s SourceID) String() string       { return string(s) }
func (r RevisionDigest) String() string { return string(r) }
func (a ArchiveDigest) String() string  { return string(a) }
func (j JobID) String() string          { return string(j) }

// ResourceCoordinate identifies a resource revision lineage.
type ResourceCoordinate struct {
	// Marketplace is the Marketplace instance the resource belongs to.
	Marketplace MarketplaceID `json:"marketplace"`

	// Source is the approved source lineage that published the resource.
	Source SourceID `json:"source"`

	// Kind is the resource kind (skill, command, subagent, hook, mcp, collection).
	Kind string `json:"kind"`

	// Name is the source-local resource name.
	Name string `json:"name"`
}

// String returns the canonical string form of a ResourceCoordinate.
func (c ResourceCoordinate) String() string {
	return fmt.Sprintf("%s/%s/%s/%s", c.Marketplace, c.Source, c.Kind, c.Name)
}

// IsZero reports whether the coordinate has no components.
func (c ResourceCoordinate) IsZero() bool {
	return c.Marketplace == "" && c.Source == "" && c.Kind == "" && c.Name == ""
}

// ParseResourceCoordinate parses the canonical string form produced by
// [ResourceCoordinate.String].
func ParseResourceCoordinate(s string) (ResourceCoordinate, error) {
	parts := strings.SplitN(s, "/", 4)
	if len(parts) != 4 {
		return ResourceCoordinate{}, fmt.Errorf("invalid resource coordinate %q", s)
	}
	return ResourceCoordinate{
		Marketplace: MarketplaceID(parts[0]),
		Source:      SourceID(parts[1]),
		Kind:        parts[2],
		Name:        parts[3],
	}, nil
}

// AcquisitionMode describes how a source's artifacts are handled.
type AcquisitionMode string

const (
	// AcquisitionIndexOnly records metadata without mirroring artifacts.
	AcquisitionIndexOnly AcquisitionMode = "index-only"

	// AcquisitionMirror stores immutable resource artifacts in the blob store.
	AcquisitionMirror AcquisitionMode = "mirror"
)

// Validate returns an error if m is not a known acquisition mode.
func (m AcquisitionMode) Validate() error {
	switch m {
	case AcquisitionIndexOnly, AcquisitionMirror:
		return nil
	default:
		return fmt.Errorf("invalid acquisition mode %q", m)
	}
}

// SourceRoot describes one root within a source repository.
type SourceRoot struct {
	// Path is the repository-relative path for this root.
	Path string `json:"path"`

	// SourceHarness is the harness layout expected at this root, when known.
	SourceHarness string `json:"source_harness,omitempty"`
}

// Publisher records publisher metadata for a source.
type Publisher struct {
	// Name is the human-readable publisher name.
	Name string `json:"name"`

	// URL is an optional publisher homepage or contact URL.
	URL string `json:"url,omitempty"`
}

// Marketplace is a single Marketplace deployment instance.
type Marketplace struct {
	// ID is the stable instance identifier.
	ID MarketplaceID `json:"id"`

	// Name is the display name of the Marketplace.
	Name string `json:"name"`

	// CanonicalURL is the canonical endpoint for the Marketplace.
	CanonicalURL string `json:"canonical_url"`

	// InstanceMetadata holds arbitrary instance-level metadata.
	InstanceMetadata map[string]any `json:"instance_metadata,omitempty"`
}

// SourceRefreshStatus describes the health of the most recent source refresh.
type SourceRefreshStatus string

const (
	// SourceRefreshStatusUnknown means the source has not been refreshed yet.
	SourceRefreshStatusUnknown SourceRefreshStatus = "unknown"

	// SourceRefreshStatusOK means the last refresh succeeded.
	SourceRefreshStatusOK SourceRefreshStatus = "ok"

	// SourceRefreshStatusFailed means the last refresh failed.
	SourceRefreshStatusFailed SourceRefreshStatus = "failed"
)

// Source is an approved source lineage in a Marketplace.
type Source struct {
	// ID is the immutable source identifier.
	ID SourceID `json:"id"`

	// Slug is the current canonical source slug.
	Slug string `json:"slug"`

	// Aliases are permanent prior slugs for this source.
	Aliases []string `json:"aliases,omitempty"`

	// Origins are the current and historical Git origins for this source.
	Origins []string `json:"origins,omitempty"`

	// Ref is the configured Git ref to follow.
	Ref string `json:"ref"`

	// Roots are the source roots to ingest.
	Roots []SourceRoot `json:"roots,omitempty"`

	// Acquisition is the default artifact acquisition mode for this source.
	Acquisition AcquisitionMode `json:"acquisition"`

	// Publisher is publisher metadata for this source.
	Publisher Publisher `json:"publisher,omitempty"`

	// License is the default SPDX license identifier for this source.
	License string `json:"license,omitempty"`

	// CredentialsRef is an opaque reference to server-side credentials.
	CredentialsRef string `json:"credentials_ref,omitempty"`

	// PolicyOverrides are optional policy and limit overrides for this source.
	PolicyOverrides map[string]any `json:"policy_overrides,omitempty"`

	// Listed controls whether the source is visible in discovery. Unlisted
	// sources keep their immutable identifiers and history but do not appear in
	// browse or search results.
	Listed bool `json:"listed"`

	// RefreshStatus is the outcome of the most recent refresh attempt.
	RefreshStatus SourceRefreshStatus `json:"refresh_status,omitempty"`

	// RefreshError carries the error message from the last failed refresh.
	RefreshError string `json:"refresh_error,omitempty"`

	// RefreshedAt is the time the last refresh completed, successful or not.
	RefreshedAt time.Time `json:"refreshed_at,omitempty"`

	// CreatedAt is the time the source was first recorded.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is the time the source was last modified.
	UpdatedAt time.Time `json:"updated_at"`
}

// Finding records a deterministic analyzer result for a resource revision.
type Finding struct {
	// Code is the stable finding code.
	Code string `json:"code"`

	// Severity is the finding severity.
	Severity string `json:"severity"`

	// Policy is the configured policy outcome for this finding.
	Policy string `json:"policy"`

	// Message is a human-readable description.
	Message string `json:"message"`

	// SourceRange is an optional source location, when available.
	SourceRange string `json:"source_range,omitempty"`
}

// Provenance records source identity metadata for a resource revision.
type Provenance struct {
	// SourceID identifies the source lineage.
	SourceID SourceID `json:"source_id,omitempty"`

	// Commit is the source commit this payload was built from.
	Commit string `json:"commit,omitempty"`

	// ManifestDigest is the digest of the source revision manifest.
	ManifestDigest RevisionDigest `json:"manifest_digest,omitempty"`
}

// Revocation records a control-plane revocation decision.
type Revocation struct {
	// Kind describes what kind of object was revoked.
	Kind RevocationKind `json:"kind"`

	// TargetID identifies the revoked object.
	TargetID string `json:"target_id"`

	// Reason is a human-readable revocation reason.
	Reason string `json:"reason"`

	// AppliedAt is when the revocation was applied.
	AppliedAt time.Time `json:"applied_at"`
}

// RevocationKind describes the scope of a revocation.
type RevocationKind string

const (
	// RevocationSource revokes an entire source lineage.
	RevocationSource RevocationKind = "source"

	// RevocationCoordinate revokes every revision of a resource coordinate.
	RevocationCoordinate RevocationKind = "coordinate"

	// RevocationRevision revokes a specific resource revision.
	RevocationRevision RevocationKind = "revision"
)

// ResourceRevision is an immutable canonical content and metadata record.
type ResourceRevision struct {
	// Coordinate identifies the resource lineage.
	Coordinate ResourceCoordinate `json:"coordinate"`

	// ContentDigest is the canonical content digest.
	ContentDigest RevisionDigest `json:"content_digest"`

	// ArchiveDigest is the artifact archive digest, when mirrored.
	ArchiveDigest ArchiveDigest `json:"archive_digest,omitempty"`

	// AnalyzerVersion is the deterministic analyzer version that produced
	// Findings.
	AnalyzerVersion string `json:"analyzer_version,omitempty"`

	// Findings are the analyzer findings for this revision.
	Findings []Finding `json:"findings,omitempty"`

	// License is the SPDX license identifier for this revision.
	License string `json:"license,omitempty"`

	// Provenance is optional source identity metadata.
	Provenance *Provenance `json:"provenance,omitempty"`

	// Revocation is the revocation state for this revision, if any.
	Revocation *Revocation `json:"revocation,omitempty"`
}

// SourceOccurrence connects a resource revision to an exact source commit.
type SourceOccurrence struct {
	// SourceID identifies the source lineage.
	SourceID SourceID `json:"source_id"`

	// Ref is the resolved Git ref.
	Ref string `json:"ref"`

	// Commit is the exact source commit.
	Commit string `json:"commit"`

	// SourceRevisionManifestDigest is the digest of the source revision manifest.
	SourceRevisionManifestDigest RevisionDigest `json:"source_revision_manifest_digest"`

	// Acquisition is the acquisition mode used for this occurrence.
	Acquisition AcquisitionMode `json:"acquisition"`

	// MirroredArtifactDigest is the artifact archive digest, when mirrored.
	MirroredArtifactDigest ArchiveDigest `json:"mirrored_artifact_digest,omitempty"`

	// OccurredAt is when this occurrence was recorded.
	OccurredAt time.Time `json:"occurred_at"`
}

// Equal reports whether two source occurrences have the same immutable fields.
// OccurredAt is assigned by the store on first publication and is intentionally
// excluded so retrying a publication with a fresh timestamp is idempotent.
func (o SourceOccurrence) Equal(other SourceOccurrence) bool {
	return o.SourceID == other.SourceID &&
		o.Ref == other.Ref &&
		o.Commit == other.Commit &&
		o.SourceRevisionManifestDigest == other.SourceRevisionManifestDigest &&
		o.Acquisition == other.Acquisition &&
		o.MirroredArtifactDigest == other.MirroredArtifactDigest
}

// CollectionRevision is an immutable manifest of exact member coordinates.
type CollectionRevision struct {
	// ID is the collection revision identifier.
	ID string `json:"id"`

	// SourceID identifies the source that published the collection.
	SourceID SourceID `json:"source_id"`

	// ManifestDigest is the digest of the collection manifest.
	ManifestDigest RevisionDigest `json:"manifest_digest"`

	// Members are the exact member resource coordinates.
	Members []ResourceCoordinate `json:"members,omitempty"`

	// CreatedAt is when this collection revision was published.
	CreatedAt time.Time `json:"created_at"`
}

// JobStatus describes the lifecycle state of a durable job.
type JobStatus string

const (
	// JobStatusPending means the job is waiting to be leased.
	JobStatusPending JobStatus = "pending"

	// JobStatusLeased means the job is held by a worker until lease_until.
	JobStatusLeased JobStatus = "leased"

	// JobStatusCompleted means the job finished successfully.
	JobStatusCompleted JobStatus = "completed"

	// JobStatusFailed means the job failed and may be retried until
	// max_attempts is reached.
	JobStatusFailed JobStatus = "failed"
)

// Job is a durable, leased unit of work.
type Job struct {
	// ID is the unique job identifier.
	ID JobID `json:"id"`

	// Kind is the job type.
	Kind string `json:"kind"`

	// Payload is the opaque job payload.
	Payload []byte `json:"payload"`

	// Status is the current job status.
	Status JobStatus `json:"status"`

	// LeaseUntil is the time until which the job is leased to a worker.
	LeaseUntil time.Time `json:"lease_until,omitempty"`

	// Attempt is the number of times the job has been leased.
	Attempt int `json:"attempt"`

	// MaxAttempts caps how many times the job may be leased.
	MaxAttempts int `json:"max_attempts"`

	// Backoff is the last computed retry backoff.
	Backoff time.Duration `json:"backoff"`

	// IdempotencyKey prevents duplicate enqueueing of the same logical job.
	IdempotencyKey string `json:"idempotency_key,omitempty"`

	// WorkerID identifies the worker that currently holds the lease, if any.
	WorkerID string `json:"worker_id,omitempty"`

	// CreatedAt is when the job was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the job was last modified.
	UpdatedAt time.Time `json:"updated_at"`
}
