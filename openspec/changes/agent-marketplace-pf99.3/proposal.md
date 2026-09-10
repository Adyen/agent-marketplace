# Marketplace stores and durable jobs

## What

Implement the Marketplace persistence layer and durable job orchestration:

- `pkg/marketplace/store` — domain entities, metadata-store and blob-store interfaces,
  SQLite/PostgreSQL metadata adapters, filesystem/S3-compatible blob adapters.
- `pkg/marketplace/job` — durable leased jobs with idempotency keys, retry limits,
  exponential backoff, jitter, and lease expiry recovery.
- `pkg/marketplace/atomic` — atomic publication helpers: upload and verify blobs before
  a metadata transaction makes a revision visible.
- Conformance tests that exercise every supported metadata/blob pairing.

## Why

pf99.2 froze and implemented canonical artifacts and analyzers; pf99.3 adds the
stores and job machinery needed by the indexing worker and API to persist
Marketplace state durably and consistently.

## Impact

- New Go packages for Marketplace persistence with capability-oriented interfaces.
- Backend names stay out of the public API; consumers see only `MetadataStore` and
  `BlobStore` interfaces.
- Jobs survive process restarts; lease expiry and retry are covered by tests.
