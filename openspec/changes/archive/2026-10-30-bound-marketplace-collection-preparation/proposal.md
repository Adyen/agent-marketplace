## Why

Marketplace `install` and `update` verify nested collections before consent, once per include path. Each visit makes three requests (registry, revision, manifest) plus cache writes, and `PrepareLocator` ran with an unlimited work budget. A Marketplace collection DAG could therefore make one command issue exponentially many requests before the user agreed to anything (#434, R06/R49).

## What Changes

This applies the maintainer-approved D2 decision for #434:

- Within one preparation, a content-addressed nested collection that has been fully verified is reused on later include paths instead of being fetched, cached and traversed again.
- `PrepareLocator`, which `install` and `update` use, applies the lock-bootstrap work limit of 13,981 units that `PrepareLocatorWithResourceLimit` callers already pass. Past the limit, preparation refuses with the existing resource-limit error before fetching any more manifests.

## Release note

`install` and `update` now fetch each nested Marketplace collection once per collection preparation. They refuse collection graphs whose traversal exceeds 13,981 work units, the same limit lock creation uses.

## Capabilities

### Modified Capabilities

- `marketplace-delivery-security`: bounded nested-collection preparation.

## Impact

Affects `internal/marketplace` collection preparation. It reuses `lockfile.MaxMarketplaceLockResources`, adds no override and no dependency.
