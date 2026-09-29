## Why

Owned-entry reconciliation in shared harness aggregate files is shipped
behavior, verified by materialization/repair consumer tests and by the Rapid
property suite added in this PR, but its normative contract is not yet in
OpenSpec. This change backfills the spec and binds each scenario to tests.

## What Changes

- Adds the `owned-entry-reconciliation` capability covering only verified
  user-observable behavior: unproven entries preserved, missing ownership
  state fails closed, unmanaged collisions refuse only the generated entry,
  proven owned entries reconcile and prune, and repair candidates partition
  by value equality.
- Binds scenarios to the existing `internal/materialize` consumer tests with `// spec:` annotations.

## Capabilities

### New Capabilities

- `owned-entry-reconciliation`: preservation, refusal, reconciliation,
  pruning, and repair classification of aggregate entries by ownership
  evidence.

### Modified Capabilities

None.

## Impact

- Spec and test-annotation only; no behavior changes.
