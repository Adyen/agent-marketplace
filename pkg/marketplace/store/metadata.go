package store

import (
	"context"
	"time"
)

// ResourceSearchFilter selects resource revisions for Marketplace discovery
// endpoints.
type ResourceSearchFilter struct {
	// Marketplace restricts results to one Marketplace instance.
	Marketplace MarketplaceID

	// Source optionally matches a source ID or source slug.
	Source string

	// Kind optionally restricts results to one resource kind.
	Kind string

	// Query is an optional case-insensitive substring matched against the
	// resource name.
	Query string
}

// MaxSourceHistoryEntries is the maximum number of source occurrences returned
// by history queries. API handlers apply the same bound before serialization.
const MaxSourceHistoryEntries = 1000

// MetadataStore is the capability-oriented interface for Marketplace metadata.
// Backend names and details never appear in this interface.
type MetadataStore interface {
	// Open initializes the metadata store and applies any pending migrations.
	Open(ctx context.Context) error

	// Close releases any resources held by the store.
	Close() error

	// WithTx runs fn inside a single atomic transaction. The MetadataStore passed
	// to fn is transaction-scoped; commits are atomic and roll back on error.
	WithTx(ctx context.Context, fn func(context.Context, MetadataStore) error) error

	// Marketplace instance metadata.
	GetMarketplace(ctx context.Context, id MarketplaceID) (Marketplace, error)
	PutMarketplace(ctx context.Context, m Marketplace) error

	// Approved sources.
	GetSource(ctx context.Context, id SourceID) (Source, error)
	PutSource(ctx context.Context, src Source) error
	ListSources(ctx context.Context) ([]Source, error)
	GetSourceBySlug(ctx context.Context, slug string) (Source, error)
	// SourceHistory returns occurrences newest first, with a deterministic
	// bounded result and a non-nil empty slice when no occurrences exist.
	SourceHistory(ctx context.Context, id SourceID) ([]SourceOccurrence, error)

	// Resource revisions.
	GetResourceRevision(ctx context.Context, coord ResourceCoordinate, digest RevisionDigest) (ResourceRevision, error)
	PutResourceRevision(ctx context.Context, rev ResourceRevision) error
	ListResourceRevisions(ctx context.Context, coord ResourceCoordinate) ([]ResourceRevision, error)
	// ListResourceRevisionsByArchiveDigest returns every revision in this
	// Marketplace that references one immutable artifact digest.
	ListResourceRevisionsByArchiveDigest(ctx context.Context, marketplace MarketplaceID, digest ArchiveDigest) ([]ResourceRevision, error)
	// ListResourceRevisionsByContentDigest returns all immutable revisions with
	// one content digest. It is used for digest-addressed collection manifests,
	// whose route has no coordinate.
	ListResourceRevisionsByContentDigest(ctx context.Context, marketplace MarketplaceID, digest RevisionDigest) ([]ResourceRevision, error)

	// GetLatestResourceRevision returns the most recent revision for a coordinate,
	// including a revoked revision if no non-revoked revision exists.
	GetLatestResourceRevision(ctx context.Context, coord ResourceCoordinate) (ResourceRevision, error)

	// SearchResourceRevisions returns revisions for listed-source coordinates,
	// ordered with the latest revision first within each coordinate, optionally
	// filtered by source, kind, and name. Callers apply live revocation policy
	// and select the latest effective revision.
	SearchResourceRevisions(ctx context.Context, filter ResourceSearchFilter) ([]ResourceRevision, error)

	// Source occurrences.
	GetSourceOccurrence(ctx context.Context, source SourceID, commit string) (SourceOccurrence, error)
	PutSourceOccurrence(ctx context.Context, occ SourceOccurrence) error
	LatestSourceOccurrence(ctx context.Context, source SourceID) (SourceOccurrence, error)

	// Collection revisions.
	GetCollectionRevision(ctx context.Context, id string) (CollectionRevision, error)
	PutCollectionRevision(ctx context.Context, cr CollectionRevision) error
	ListCollectionRevisions(ctx context.Context, source SourceID) ([]CollectionRevision, error)

	// Revocations.
	GetRevocation(ctx context.Context, kind RevocationKind, targetID string) (Revocation, error)
	PutRevocation(ctx context.Context, r Revocation) error
	DeleteRevocation(ctx context.Context, kind RevocationKind, targetID string) error
	ListRevocations(ctx context.Context, kind RevocationKind) ([]Revocation, error)

	// Durable jobs.
	GetJob(ctx context.Context, id JobID) (Job, error)
	EnqueueJob(ctx context.Context, job Job) error
	LeaseJob(ctx context.Context, kind, workerID string, leaseDuration time.Duration) (Job, error)
	ExtendLease(ctx context.Context, id JobID, workerID string, d time.Duration) error
	ReleaseJob(ctx context.Context, id JobID, workerID string) error
	CompleteJob(ctx context.Context, id JobID, workerID string) error
	FailJob(ctx context.Context, id JobID, workerID string, backoff time.Duration) error

	// ReadyCheck verifies that every published artifact has a matching blob.
	// It returns [ErrBlobMissing] for the first missing digest.
	ReadyCheck(ctx context.Context, blobs BlobStore) error
}
