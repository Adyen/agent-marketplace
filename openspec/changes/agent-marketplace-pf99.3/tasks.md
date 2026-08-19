# Tasks: agent-marketplace-pf99.3

## Domain and interfaces
- [ ] `pkg/marketplace/store/domain.go` — Marketplace, Source, ResourceRevision,
  SourceOccurrence, CollectionRevision, Revocation, Job, ID types.
- [ ] `pkg/marketplace/store/metadata.go` — `MetadataStore` interface with methods
  for CRUD and query of all domain entities.
- [ ] `pkg/marketplace/store/blob.go` — `BlobStore` interface for content-addressed
  read/write/exists/delete of blobs by digest.
- [ ] `pkg/marketplace/store/errors.go` — typed errors (`ErrNotFound`, `ErrConflict`,
  `ErrLeaseExpired`, `ErrTopology`, etc.).

## Metadata adapters
- [ ] `pkg/marketplace/store/sqlite` — SQLite implementation with embedded migrations,
  WAL, busy timeout, retry, and split-process safety.
- [ ] `pkg/marketplace/store/postgres` — PostgreSQL implementation with same schema
  and concurrency behavior for distributed roles.

## Blob adapters
- [ ] `pkg/marketplace/store/fsblob` — filesystem blob store with SHA-256 key layout
  and atomic writes.
- [ ] `pkg/marketplace/store/s3blob` — S3-compatible blob store using the AWS SDK v2
  S3 client, with endpoint override for MinIO/Ceph/R2.

## Jobs
- [ ] `pkg/marketplace/job/job.go` — `Job` type and status constants.
- [ ] `pkg/marketplace/job/lease.go` — lease acquisition, extension, expiry, release.
- [ ] `pkg/marketplace/job/retry.go` — exponential backoff with jitter and max attempts.
- [ ] `pkg/marketplace/job/scheduler.go` — enqueue/dequeue/complete/fail helpers over
  `MetadataStore`.

## Atomic publication and consistency
- [ ] `pkg/marketplace/store/atomic.go` — `PublishSourceRevision` helper that uploads
  blobs before committing metadata.
- [ ] Startup consistency checks and missing-blob readiness failures.

## Tests and benchmarks
- [ ] Unit tests for each adapter.
- [ ] Conformance tests covering all metadata/blob pairings and topology constraints.
- [ ] Job lease expiry/recovery tests.
- [ ] Benchmarks for hot paths (job dequeue, blob read/write).

## Verification
- [ ] `go test ./pkg/marketplace/...`
- [ ] `make lint`
- [ ] `make build`

## OpenSpec
- [x] `openspec/changes/agent-marketplace-pf99.3/{.openspec.yaml,proposal.md,design.md,tasks.md}`
