## Why

Canonical hooks carry reserved null serializer metadata. Removing it in the
shared decoder changes existing native hook bytes and invalidates clean
ownership-proven installations even though the source revision is unchanged.

## What Changes

- Restore the historical decoder mapping for all resource kinds.
- Let OpenCode admit the reserved null field without admitting actual extensions.
- Preserve existing native hook bytes and ownership fingerprints.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-api`: canonical metadata admission without native projection drift.

## Impact

OpenCode hook admission only. No artifact, lock, ownership or native identity format changes.
