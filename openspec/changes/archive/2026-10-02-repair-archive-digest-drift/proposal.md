## Why

The mirror-support archive change released in v4.0.10-rc.1 moved skill support
files into a `support/` archive directory with per-file executable modes. That
changes the canonical archive encoding for every mirrored skill with support
files while leaving its content digest unchanged, and the store treats
`archive_digest` as immutable per content digest. Since 2026-09-28 every
scheduled awesome-ai publication has therefore returned
`store.ErrConflict` (`resource revision … already has different immutable
fields`) and rolled back, freezing that registry's catalog: new registry
content, including the `genai-psx` MCP server from awesome-ai MR !304, is
invisible on the Marketplace. A locked consumer that loses its local cache
after the catalog republishes would hit the mirror-image failure mode, a hard
`locked archive digest mismatch` resolution error that `update` cannot repair
because it skips pins whose content digest is unchanged.

## What Changes

- Publication repairs archive-digest drift in place: publishing a resource
  revision whose content digest matches an existing row but whose archive
  digest differs uploads the new resource artifact and updates the stored
  archive digest for that revision instead of returning `store.ErrConflict`;
  license and description remain immutable, the old artifact remains
  content-addressed and downloadable, and the repair is observable in
  publication diagnostics.
- Locked consumers refresh archive evidence: when resolution of a locked
  Marketplace record fetches revision metadata whose content digest matches
  the pin but whose archive digest differs, the CLI refreshes the lock's
  archive-digest evidence and re-acquires the resource artifact, reporting a
  warning, instead of failing resolution; an update that skips a pin because
  its content digest is unchanged performs the same evidence refresh. The
  stored lock's byte format is unchanged.
- Failed managed refresh attempts record their cause: a failed refresh
  attempt persists a stable failure code plus a sanitized cause message, the
  scheduled worker logs the cause, and the admin attempts surface exposes it.
- Clients that predate `support/` archive members keep failing canonical
  digest validation on such archives rather than silently installing them
  (unchanged behavior).

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `marketplace-api`: the canonical artifact format contract changes from
  "`archive_digest` is immutable per content digest, so `support/` members can
  only ship under a new content revision, and consumers must fail closed
  until a new content revision or an index-only source supplies the bytes" to
  repair-in-place semantics: a same-content revision may carry a repaired
  archive digest, and current clients refresh their locked archive evidence
  instead of failing closed.
- `marketplace-publication`: publication transaction behavior gains the
  repair-in-place requirement for drifted archive digests on identical
  content, and failed managed refresh attempts gain the recorded-cause
  requirement.

## Impact

- Store and publication path: `pkg/marketplace/store` (postgres and sqlite
  `putResourceRevision`), publication SQL glue, and the managed worker loop's
  failure recording and logging.
- Consumer path: `internal/marketplace` resolver validation of locked archive
  evidence and lock evidence refresh, `internal/pipeline` update skip
  handling, and the lock writer path that records archive digests.
- Wire format: the admin attempts surface gains failure code and cause fields
  (marketplace-api OpenAPI contract).
- Production effect once deployed: the next scheduled awesome-ai refresh
  repairs the stored archive digests and republishes the catalog, including
  `genai-psx`, without `serve reset`; occurrence and publication history are
  preserved. Consumers should receive the lock-evidence refresh in the same
  release train as (or before) the server repair so the catalog flip never
  strands a cache-miss consumer.
