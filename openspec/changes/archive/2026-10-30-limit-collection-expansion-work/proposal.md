## Why

Collection cycle detection is branch-local, so a nested collection shared by two branches expands once per include path. A registry can ship a small collection DAG that expands exponentially during resolution: it takes about 0.4 s at depth 16, and each further level doubles the time (#434, R11).

## What Changes

The maintainer-approved D2 decision for #434 gives one resolve the same work limit that lock bootstrap uses: 13,981 units, which is `lockfile.MaxFileSize` divided by the 300-byte minimum Marketplace lock entry. Each expanded collection costs one unit, plus one per member and one per include. A resolve that exceeds the limit reports `ErrCollectionExpansionLimit` as a hard error, so sync refuses before materializing or pruning anything.

The decision's first option was to expand each collection once per resolve. That was rejected because nested expansion depends on the parent: the via label, narrowed harnesses, inherited source and required flags, and branch-local cycle ancestry. Per-path diagnostics are also reported once per path, so expanding once would change diagnostics. As the decision allows, the limit replaces it.

## Release note

Collection graphs whose nested expansion exceeds 13,981 work units are now refused with a collection-expansion error instead of being expanded. This prevents a registry from stalling commands with deeply shared nested collections.

## Capabilities

### Modified Capabilities

- `resource-selection`: collection expansion work limit.

## Impact

Affects `internal/resolver` collection expansion. The lock-bootstrap limit moves into `internal/lockfile` as `MaxMarketplaceLockResources`, the single authority shared with `internal/marketplacelock`. There is no override and no new dependency.
