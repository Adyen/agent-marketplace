package store

import (
	"errors"
	"fmt"
)

// Sentinel errors for the Marketplace persistence layer.
var (
	// ErrNotFound is returned when a requested record or blob does not exist.
	ErrNotFound = errors.New("marketplace/store: not found")

	// ErrConflict is returned when a write would violate a uniqueness constraint
	// or an idempotency key has already been used.
	ErrConflict = errors.New("marketplace/store: conflict")

	// ErrStaleGeneration is returned when a publication's expected generation is
	// no longer desired for its registry.
	ErrStaleGeneration = fmt.Errorf("%w: stale generation", ErrConflict)

	// ErrLeaseExpired is returned when a worker attempts to extend, release,
	// complete, or fail a job that is no longer held by that worker.
	ErrLeaseExpired = errors.New("marketplace/store: lease expired")

	// ErrTopology is returned when an operation is requested on an invalid or
	// unsupported storage topology.
	ErrTopology = errors.New("marketplace/store: topology")

	// ErrBlobMissing is returned by readiness checks when a published artifact
	// has no corresponding blob in the blob store.
	ErrBlobMissing = errors.New("marketplace/store: blob missing")
)
