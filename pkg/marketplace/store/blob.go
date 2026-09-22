package store

import (
	"context"
	"errors"
	"io"
	"math"
)

var ErrBlobTooLarge = errors.New("marketplace/store: blob exceeds bounded read limit")

type BoundedBlobStore interface {
	GetBounded(context.Context, BlobDigest, int64) ([]byte, error)
}

func ReadBlobBounded(reader io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 || limit == math.MaxInt64 {
		return nil, ErrBlobTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, ErrBlobTooLarge
	}
	return data, nil
}

// BlobStore is the capability-oriented interface for content-addressed blobs.
// All digests are archive digests; callers verify digests before invoking
// these methods.
type BlobStore interface {
	// Probe verifies that the blob backend is reachable using one bounded
	// request. It must not enumerate or download catalog blobs.
	Probe(ctx context.Context) error

	// Get returns the blob bytes for the given digest.
	// It returns [ErrNotFound] when the blob is missing.
	Get(ctx context.Context, digest BlobDigest) ([]byte, error)

	// Put stores data under the given digest, atomically replacing any
	// existing content at that address.
	Put(ctx context.Context, digest BlobDigest, data []byte) error

	// Exists reports whether a blob exists for the given digest.
	Exists(ctx context.Context, digest BlobDigest) (bool, error)

	// Delete removes the blob for the given digest.
	Delete(ctx context.Context, digest BlobDigest) error
}
