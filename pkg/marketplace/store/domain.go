package store

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// Strong ID types for the Marketplace domain.
type (
	// MarketplaceID identifies a Marketplace deployment instance.
	MarketplaceID string

	// RegistryID identifies an approved registry lineage. It is immutable and never
	// reassigned, even when the registry slug or origin changes.
	RegistryID string

	// RevisionDigest is the content-addressed digest of a canonical resource
	// payload.
	RevisionDigest string

	// ArchiveDigest is the content-addressed digest of a complete resource
	// artifact archive.
	ArchiveDigest string

	// AssessmentDigest is the content-addressed identity of a resource
	// assessment.
	AssessmentDigest string

	// PolicyDigest identifies the exact policy used to assess a resource.
	PolicyDigest string

	// JobID uniquely identifies a durable job.
	JobID string

	// CatalogVersion is the durable, monotonically increasing version of all
	// catalog-visible state.
	CatalogVersion uint64
)

func (m MarketplaceID) String() string    { return string(m) }
func (s RegistryID) String() string       { return string(s) }
func (r RevisionDigest) String() string   { return string(r) }
func (a ArchiveDigest) String() string    { return string(a) }
func (a AssessmentDigest) String() string { return string(a) }
func (p PolicyDigest) String() string     { return string(p) }
func (j JobID) String() string            { return string(j) }
func (c CatalogVersion) String() string   { return fmt.Sprintf("%d", c) }

// ResourceCoordinate identifies a resource revision lineage.
type ResourceCoordinate struct {
	// Marketplace is the Marketplace instance the resource belongs to.
	Marketplace MarketplaceID `json:"marketplace"`

	// Registry is the approved registry lineage that published the resource.
	Registry RegistryID `json:"registry"`

	// Kind is the resource kind (skill, command, subagent, hook, mcp, collection).
	Kind string `json:"kind"`

	// Name is the registry-local resource name.
	Name string `json:"name"`
}

// String returns the canonical string form of a ResourceCoordinate.
func (c ResourceCoordinate) String() string {
	return fmt.Sprintf("%s/%s/%s/%s", c.Marketplace, c.Registry, c.Kind, c.Name)
}

// IsZero reports whether the coordinate has no components.
func (c ResourceCoordinate) IsZero() bool {
	return c.Marketplace == "" && c.Registry == "" && c.Kind == "" && c.Name == ""
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
		Registry:    RegistryID(parts[1]),
		Kind:        parts[2],
		Name:        parts[3],
	}, nil
}

// RevisionRevocationTarget is the exact coordinate-plus-content-digest identity
// used by operator revision revocations.
type RevisionRevocationTarget struct {
	Coordinate    ResourceCoordinate
	ContentDigest RevisionDigest
}

// ParseRevisionRevocationTarget parses marketplace/registry/kind/name@sha256:<64
// lowercase hexadecimal characters>. A digest without its coordinate is not an
// exact identity and is intentionally rejected.
func ParseRevisionRevocationTarget(raw string) (RevisionRevocationTarget, error) {
	input := strings.TrimSpace(raw)
	if strings.Count(input, "@") != 1 {
		return RevisionRevocationTarget{}, fmt.Errorf("invalid revision revocation %q: expected marketplace/registry/kind/name@sha256:<64 lowercase hex>", raw)
	}
	at := strings.LastIndexByte(input, '@')
	coordinate, err := ParseResourceCoordinate(input[:at])
	if err != nil {
		return RevisionRevocationTarget{}, err
	}
	for label, value := range map[string]string{
		"marketplace": string(coordinate.Marketplace), "registry": string(coordinate.Registry),
		"kind": coordinate.Kind, "name": coordinate.Name,
	} {
		if value == "" || strings.ContainsAny(value, "/@") {
			return RevisionRevocationTarget{}, fmt.Errorf("invalid revision revocation %q: %s must be a non-empty route token", raw, label)
		}
	}
	if !IsMarketplaceResourceKind(coordinate.Kind) {
		return RevisionRevocationTarget{}, fmt.Errorf("invalid revision revocation %q: unsupported resource kind %q", raw, coordinate.Kind)
	}
	digest := input[at+1:]
	if len(digest) != len("sha256:")+64 || !strings.HasPrefix(digest, "sha256:") {
		return RevisionRevocationTarget{}, fmt.Errorf("invalid revision revocation %q: content digest must be sha256:<64 lowercase hex>", raw)
	}
	for i := len("sha256:"); i < len(digest); i++ {
		c := digest[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return RevisionRevocationTarget{}, fmt.Errorf("invalid revision revocation %q: content digest must be sha256:<64 lowercase hex>", raw)
		}
	}
	return RevisionRevocationTarget{Coordinate: coordinate, ContentDigest: RevisionDigest(digest)}, nil
}

// String returns the canonical exact revision revocation target.
func (t RevisionRevocationTarget) String() string {
	return t.Coordinate.String() + "@" + string(t.ContentDigest)
}

// IsMarketplaceResourceKind reports whether kind is valid in a public
// Marketplace resource coordinate. Keeping this validation in the store parser
// prevents operator revocation declarations from accepting identities that the
// consumer-side override parser would reject.
func IsMarketplaceResourceKind(kind string) bool {
	switch kind {
	case "skill", "command", "subagent", "hook", "mcpserver", "collection":
		return true
	default:
		return false
	}
}

// AcquisitionMode describes how a registry's artifacts are handled.
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

// RegistryRoot describes one root within a registry repository.
type RegistryRoot struct {
	// Path is the repository-relative path for this root.
	Path string `json:"path"`

	// SourceHarness is the harness layout expected at this root, when known.
	SourceHarness string `json:"source_harness,omitempty"`
}

// Publisher records publisher metadata for a registry.
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

// RegistryRefreshStatus describes the health of the most recent registry refresh.
type RegistryRefreshStatus string

const (
	// RegistryRefreshStatusUnknown means the registry has not been refreshed yet.
	RegistryRefreshStatusUnknown RegistryRefreshStatus = "unknown"

	// RegistryRefreshStatusOK means the last refresh succeeded.
	RegistryRefreshStatusOK RegistryRefreshStatus = "ok"

	// RegistryRefreshStatusFailed means the last refresh failed.
	RegistryRefreshStatusFailed RegistryRefreshStatus = "failed"
)

// Registry is an approved registry lineage in a Marketplace.
type Registry struct {
	// ID is the immutable registry identifier.
	ID RegistryID `json:"id"`

	// Slug is the current canonical registry slug.
	Slug string `json:"slug"`

	// Aliases are permanent prior slugs for this registry.
	Aliases []string `json:"aliases,omitempty"`

	// Origins are the current and historical Git origins for this registry.
	Origins []string `json:"origins,omitempty"`

	// Ref is the configured Git ref to follow.
	Ref string `json:"ref"`

	// Roots are the registry roots to ingest.
	Roots []RegistryRoot `json:"roots,omitempty"`

	// Acquisition is the default artifact acquisition mode for this registry.
	Acquisition AcquisitionMode `json:"acquisition"`

	// Publisher is publisher metadata for this registry.
	Publisher Publisher `json:"publisher,omitempty"`

	// License is the default SPDX license identifier for this registry.
	License string `json:"license,omitempty"`

	// CredentialsRef is an opaque reference to server-side credentials.
	CredentialsRef string `json:"credentials_ref,omitempty"`

	// PolicyOverrides are optional policy and limit overrides for this registry.
	PolicyOverrides map[string]any `json:"policy_overrides,omitempty"`

	// Listed controls whether the registry is visible in discovery. Unlisted
	// registries keep their immutable identifiers and history but do not appear in
	// browse or search results.
	Listed bool `json:"listed"`

	// RefreshStatus is the outcome of the most recent refresh attempt.
	RefreshStatus RegistryRefreshStatus `json:"refresh_status,omitempty"`

	// RefreshError carries the error message from the last failed refresh.
	RefreshError string `json:"refresh_error,omitempty"`

	// RefreshedAt is the time the last refresh completed, successful or not.
	RefreshedAt time.Time `json:"refreshed_at,omitempty"`

	// CreatedAt is the time the registry was first recorded.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is the time the registry was last modified.
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

	Location *FindingLocation `json:"location,omitempty"`
}

type FindingLocation struct {
	Path      string `json:"path"`
	Line      int    `json:"line,omitempty"`
	Column    int    `json:"column,omitempty"`
	StartByte int64  `json:"start_byte,omitempty"`
	EndByte   int64  `json:"end_byte,omitempty"`
}

// AssessmentDecision is the explicit outcome of a resource assessment.
type AssessmentDecision string

const (
	// AssessmentDecisionAllow permits use of the assessed resource.
	AssessmentDecisionAllow AssessmentDecision = "allow"

	// AssessmentDecisionWarn permits use while surfacing assessment findings.
	AssessmentDecisionWarn AssessmentDecision = "warn"

	// AssessmentDecisionReject refuses use of the assessed resource.
	AssessmentDecisionReject AssessmentDecision = "reject"
)

// ResourceAssessment is an immutable assessment of one exact resource revision.
// Its AssessmentDigest identifies every field except AssessedAt and the digest
// itself.
type ResourceAssessment struct {
	Coordinate ResourceCoordinate `json:"coordinate"`

	ContentDigest RevisionDigest `json:"content_digest"`

	AssessmentDigest AssessmentDigest `json:"assessment_digest"`

	AnalyzerVersion string `json:"analyzer_version"`

	PolicyDigest PolicyDigest `json:"policy_digest"`

	Decision AssessmentDecision `json:"decision"`

	Findings []Finding `json:"findings,omitempty"`

	InspectionManifestDigest RevisionDigest `json:"inspection_manifest_digest"`

	AssessedAt time.Time `json:"assessed_at"`
}

// Provenance records registry identity metadata for a resource revision.
type Provenance struct {
	// RegistryID identifies the registry lineage.
	RegistryID RegistryID `json:"registry_id,omitempty"`

	// Commit is the registry commit this payload was built from.
	Commit string `json:"commit,omitempty"`

	// ManifestDigest is the digest of the registry revision manifest.
	ManifestDigest RevisionDigest `json:"manifest_digest,omitempty"`

	InspectionManifestDigest RevisionDigest `json:"inspection_manifest_digest,omitempty"`
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
	// RevocationRegistry revokes an entire registry lineage.
	RevocationRegistry RevocationKind = "registry"

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

	// License is the SPDX license identifier for this revision.
	License string `json:"license,omitempty"`

	// Description is the author-written one-line summary of the resource. It
	// originates in a third-party registry, so it is bounded and normalized by
	// BoundDescription before it is stored or served.
	Description string `json:"description,omitempty"`
}

// ResourceOccurrence records one resource revision's membership in a registry occurrence.
type ResourceOccurrence struct {
	Coordinate     ResourceCoordinate `json:"coordinate"`
	ContentDigest  RevisionDigest     `json:"content_digest"`
	Commit         string             `json:"commit"`
	ManifestDigest RevisionDigest     `json:"manifest_digest"`

	// SourcePath is the normalized repository-relative path of this resource
	// in this exact occurrence. It is optional for rows written before source
	// provenance was captured.
	SourcePath string `json:"source_path,omitempty"`
}

// Equal reports whether two resource occurrences have the same immutable
// fields. Source provenance may be enriched once when legacy fields are null.
func (o ResourceOccurrence) Equal(other ResourceOccurrence) bool {
	if o.Coordinate != other.Coordinate || o.ContentDigest != other.ContentDigest ||
		o.Commit != other.Commit || o.ManifestDigest != other.ManifestDigest ||
		o.SourcePath != other.SourcePath {
		return false
	}
	return true
}

// AssessedResourceRevision is the current read projection for one immutable resource revision.
type AssessedResourceRevision struct {
	ResourceRevision
	Assessment            ResourceAssessment `json:"assessment"`
	Occurrence            ResourceOccurrence `json:"occurrence"`
	Revocation            *Revocation        `json:"revocation,omitempty"`
	EffectiveAvailability string             `json:"-"`
}

// RegistryOccurrence connects a registry revision manifest to an exact registry commit.
type RegistryOccurrence struct {
	// RegistryID identifies the registry lineage.
	RegistryID RegistryID `json:"registry_id"`

	// Ref is the resolved Git ref.
	Ref string `json:"ref"`

	Roots []RegistryRoot `json:"roots"`

	// Commit is the exact registry commit.
	Commit string `json:"commit"`

	// RegistryRevisionManifestDigest is the digest of the registry revision manifest.
	RegistryRevisionManifestDigest RevisionDigest `json:"registry_revision_manifest_digest"`

	// Acquisition is the acquisition mode used for this occurrence.
	Acquisition AcquisitionMode `json:"acquisition"`

	// MirroredArtifactDigest is the artifact archive digest, when mirrored.
	MirroredArtifactDigest ArchiveDigest `json:"mirrored_artifact_digest,omitempty"`

	// SourceOrigin is the sanitized public repository origin used for this
	// exact occurrence. It is optional for legacy rows.
	SourceOrigin string `json:"source_origin,omitempty"`

	// OccurredAt is when this occurrence was recorded.
	OccurredAt time.Time `json:"occurred_at"`
}

// Equal reports whether two registry occurrences have the same immutable fields.
// OccurredAt is assigned by the store on first publication and is intentionally
// excluded so retrying a publication with a fresh timestamp is idempotent.
func (o RegistryOccurrence) Equal(other RegistryOccurrence) bool {
	return o.RegistryID == other.RegistryID &&
		o.Ref == other.Ref &&
		slices.Equal(o.Roots, other.Roots) &&
		o.Commit == other.Commit &&
		o.RegistryRevisionManifestDigest == other.RegistryRevisionManifestDigest &&
		o.Acquisition == other.Acquisition &&
		o.MirroredArtifactDigest == other.MirroredArtifactDigest &&
		o.SourceOrigin == other.SourceOrigin
}

// CollectionRevision is an immutable manifest of direct resource members and
// direct collection includes.
type CollectionRevision struct {
	// ID is the collection revision identifier.
	ID string `json:"id"`

	// RegistryID identifies the registry that published the collection.
	RegistryID RegistryID `json:"registry_id"`

	// ManifestDigest is the digest of the collection manifest.
	ManifestDigest RevisionDigest `json:"manifest_digest"`

	// Members are the exact member resource coordinates.
	Members []ResourceCoordinate `json:"members,omitempty"`

	// Includes are the exact direct collection include coordinates.
	Includes []ResourceCoordinate `json:"includes,omitempty"`

	// CreatedAt is when this collection revision was published.
	CreatedAt time.Time `json:"created_at"`
}

// Equal reports whether two collection revisions have the same immutable
// metadata. CreatedAt is assigned by the store on first publication and is
// intentionally excluded so retries are idempotent.
func (c CollectionRevision) Equal(other CollectionRevision) bool {
	if c.ID != other.ID || c.RegistryID != other.RegistryID || c.ManifestDigest != other.ManifestDigest ||
		!slices.Equal(c.Members, other.Members) || !slices.Equal(c.Includes, other.Includes) {
		return false
	}
	return true
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
