package store

import (
	"context"
	"time"
)

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
	SourceHistory(ctx context.Context, id SourceID) ([]SourceOccurrence, error)

	// Resource revisions.
	GetResourceRevision(ctx context.Context, coord ResourceCoordinate, digest RevisionDigest) (ResourceRevision, error)
	PutResourceRevision(ctx context.Context, rev ResourceRevision) error
	ListResourceRevisions(ctx context.Context, coord ResourceCoordinate) ([]ResourceRevision, error)

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
