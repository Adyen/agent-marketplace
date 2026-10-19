## Why

Shared-aggregate repair can use mutable Git content or publish after configuration changes invalidate its preview. Cache collection can delete from reachability computed before a deployed acquisition lock or configuration changes during preflight. Both operations must refuse stale authority without reviving network-dependent collection.

## What Changes

- Resolve repair selections from validated acquisition locks and exact snapshots, refusing missing or invalid Git evidence and incomplete organization acquisition.
- Revalidate repair policy inside the ownership mutation and immediately before publication, preserving confirmation, backup, and no-lock-across-prompt behavior.
- Revalidate captured configuration and deployed acquisition evidence before real collection, retaining network-independent GC.
- Preserve independent configuration errors when reporting organization acquisition failures.
- Replace obsolete network-acquisition and provisional-lock test expectations with current consumer behavior.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `owned-entry-reconciliation`: Exact repair authority and policy-change refusal at confirmation and publication boundaries.
- `cache-garbage-collection`: Configuration and deployed-lock change refusal after preflight.
- `deployed-acquisition-locks`: Organization acquisition failure diagnostics preserve independent errors.

## Impact

`doctor --repair`, shared-aggregate ownership publication, `gc`, acquisition evidence, and command and built-binary regression tests. Git-backed repair requires a valid acquisition lock; directory resources remain live. No configuration or acquisition-lock schema changes and no network acquisition is added to GC.
