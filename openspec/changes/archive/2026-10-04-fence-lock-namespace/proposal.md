## Why

Ownership lock directory creation/opening still resolves public paths. Retargeting locks/ after Store.Open lets Lock rewrite a foreign regular diagnostic leaf despite safe final-leaf checks.

## What Changes

- Record canonical selected lock namespace at Store.Open without changing read-only failure timing.
- Reuse captured strict/private absolute directory preparation/capture for state and selected lock namespaces.
- Create/reuse the persistent lock descriptor directly under the captured parent, and revalidate named identity before admission, after waiting and before diagnostic writes.
- Preserve existing initial selected state/shared lock-directory aliases and kernel exclusion.

## Capabilities

### Modified Capabilities

- `owned-entry-reconciliation`: selected lock namespace authority.

## Impact

R2 child of #523. No stale-lock deletion/recreation, new budget or overall transaction/exclusion. Restart recovery/evidence-action and remaining backup/restore cases/final audit remain open.
