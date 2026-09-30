## Why

Cache garbage collection previously depended on acquisition of unrelated organization Marketplace resources, so a permanently absent organization lock could prevent valid collection.

## What Changes

- Derive retained cache objects from local configuration and exact deployed evidence, without network acquisition for a missing organization lock.
- Preserve fail-closed behavior for malformed or stale present locks and required evidence.
- Leave live collection roots untouched and make repeat collection idempotent.

## Capabilities

### New Capabilities

- `cache-garbage-collection`: Local reachability, missing organization locks, and cache deletion boundaries.

### Modified Capabilities

None.

## Impact

`gc`, cache roots, Marketplace artifacts, and acquisition-lock validation.
