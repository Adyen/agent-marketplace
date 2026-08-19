# Design: Marketplace stores and durable jobs

## Dependencies

- `agent-marketplace-pf99.2` provides canonical artifacts, analyzers, source revision
  manifests, and resource coordinates.

## Decisions

- Domain types live in `pkg/marketplace/store` and are plain Go structs/IDs.
- `MetadataStore` is the persistence contract for Marketplace, sources, resource
  revisions, source occurrences, collections, revocations, and jobs.
- `BlobStore` is the content-addressed storage contract for raw artifact archives,
  collection manifests, and source revision manifests.
- Adapters are isolated in subpackages:
  - `pkg/marketplace/store/sqlite`
  - `pkg/marketplace/store/postgres`
  - `pkg/marketplace/store/fsblob`
  - `pkg/marketplace/store/s3blob`
- SQLite is the single-writer default; PostgreSQL supports split API/worker roles.
- Filesystem blobs store by SHA-256 digest under a root directory; S3-compatible
  blobs use the same SHA-256 key layout over an S3-compatible HTTP API.
- Jobs are database rows with `id`, `kind`, `payload`, `status`, `lease_until`,
  `attempt`, `max_attempts`, `backoff`, `idempotency_key`, `created_at`, `updated_at`.
- Leases use `time.Now().UTC()` plus a duration; workers heartbeat to extend.
- Atomic publication uploads blobs first, then commits metadata in one transaction.
- Migrations are embedded SQL files applied by each adapter at `Open` time.
- Topology constraints are enforced by the store layer and verified by conformance tests.

## Files

- `pkg/marketplace/store/{domain,metadata,blob,errors}.go`
- `pkg/marketplace/store/sqlite/{store,migrate}.go`
- `pkg/marketplace/store/postgres/{store,migrate}.go`
- `pkg/marketplace/store/fsblob/{store}.go`
- `pkg/marketplace/store/s3blob/{store}.go`
- `pkg/marketplace/job/{job,lease,retry,scheduler}.go`
- `pkg/marketplace/store/conformance_test.go`
- Corresponding `*_test.go` files.
