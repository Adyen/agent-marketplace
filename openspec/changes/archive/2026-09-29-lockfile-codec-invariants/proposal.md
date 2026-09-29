## Why

Acquisition lock encoding is shipped behavior, verified by existing golden
and normalization tests and by the Rapid property suite added in this PR, but
its normative contract is not yet in OpenSpec. This change backfills the spec
and binds each scenario to tests.

## What Changes

- Adds the `acquisition-lock-codec` capability covering only verified
  user-observable behavior: deterministic canonical lock bytes, ordering-
  and duplicate-insensitive emission, and normalized decode round trips.
- Binds scenarios to the existing `internal/lockfile` golden and consumer
  tests with `// spec:` annotations.

## Capabilities

### New Capabilities

- `acquisition-lock-codec`: canonical bytes, stable ordering, exact duplicate
  collapse, and normalized round trips for written acquisition locks.

### Modified Capabilities

None.

## Impact

- Spec and test-annotation only; no behavior changes.
