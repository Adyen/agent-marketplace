## Why

A Marketplace resolves a content digest (for example, the bytes behind
`/api/v1/content/sha256/{digest}` or the revisions carrying a mirrored
artifact) by asking its metadata store which resource revisions own that
digest. As indexed revision history grows, answering that question without a
digest-scoped index degenerates into scanning every retained resource
revision and current resource assessment for the Marketplace. The store
already gained the required non-unique indexes on both backends; this change
records the durable contract so the indexes cannot be dropped or weakened
without a spec review.

## What Changes

- The Marketplace metadata store MUST maintain non-unique indexes over
  (Marketplace, content digest) on its resource-revision and
  current-assessment relations in both supported store backends.
- The pre-existing SQLite resource-revision digest index MUST remain intact;
  no index is removed or altered.
- No API, wire format, configuration, or CLI behavior changes.

## Capabilities

### New Capabilities
- `digest-owner-lookup`: the metadata-store indexing contract that keeps
  resolving the owners of a content digest bounded by matching rows rather
  than by total retained revision history.

### Modified Capabilities
- None.

## Impact

- Specs: new `digest-owner-lookup` capability.
- Tests: `// spec:` annotations on the existing migration assertions in
  `pkg/marketplace/store/postgres/store_test.go` and
  `pkg/marketplace/store/sqlite/store_test.go`.
- No production-latency improvement is claimed: the retained PostgreSQL
  benchmark (8 samples per side, p=0.083) is suggestive, not statistically
  conclusive.
