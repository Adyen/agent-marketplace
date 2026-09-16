package store

import "context"

// BlobStore is the capability-oriented interface for content-addressed blobs.
// All digests are archive digests; callers verify digests before invoking
// these methods.
type BlobStore interface {
	// Get returns the blob bytes for the given digest.
	// It returns [ErrNotFound] when the blob is missing.
	Get(ctx context.Context, digest ArchiveDigest) ([]byte, error)

	// Put stores data under the given digest, atomically replacing any
	// existing content at that address.
	Put(ctx context.Context, digest ArchiveDigest, data []byte) error

	// Exists reports whether a blob exists for the given digest.
	Exists(ctx context.Context, digest ArchiveDigest) (bool, error)

	// Delete removes the blob for the given digest.
	Delete(ctx context.Context, digest ArchiveDigest) error
}
