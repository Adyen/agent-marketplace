## Why

Source-revision primary/support hashing still reopens paths after containment checks. Unsafe post-ingest payload shapes can contribute to a revision; read errors may degrade to canonical IR fallback or omitted-resource warnings instead of refusing the revision.

## What Changes

- Capture the revision root once using the admitted source view and hash primary/support payloads through regular singly-linked nonblocking stable snapshots.
- Propagate typed catalog/snapshot boundary refusals as hard revision errors with no partial payload, retaining established package identities.
- Preserve source-revision compatibility bytes/schema, canonical collection serialization, existing root/link checks and genuine absence fallback.

## Capabilities

### Modified Capabilities

- `registry-symlink-containment`: source-revision hashing authority and refusal disposition.

## Impact

R2 hash-reader child of #479. No new absolute input/tree/work ceiling, whole-source atomic snapshot or reader/output/prune/aggregate/restore/ownership closure.
