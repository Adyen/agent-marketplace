## Why

Ownership lock acquisition opens a final no-follow leaf but does not validate its opened shape/link count before acquiring or truncating diagnostics. A hardlinked lock path rewrites foreign bytes.

## What Changes

- Add nonblocking/close-on-exec open flags and shared opened regular/single-link validation before acquisition, after successful wait and before metadata truncation.
- Preserve best-effort diagnostics and kernel lock semantics.
- Prove actual LockContext foreign-byte preservation and real regular lock metadata control, bounded FIFO refusal and post-acquisition link preservation.

## Capabilities

### Modified Capabilities

- `owned-entry-reconciliation`: ownership lock leaf admission.

## Impact

R2 child of #522. Existing explicitly selected state-root and shared locks-directory aliases retain exclusion. Directory namespace pinning/restart/action leases/backup audit remain separate; no new budgets, whole mutation transaction or uncooperative-writer exclusion.
