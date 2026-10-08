## Why

Prepared ownership recovery validates state/journal pins before reading the live aggregate, then decides cleanup from that read without rechecking original metadata. A late same-JSON state replacement causes recovery to delete valid stage/journal evidence and report a successful rollback.

## What Changes

- Reuse original state/journal validation after aggregate observation before prepared recovery decisions or cleanup.
- Preserve original/replacement metadata, staged/journal evidence and native state on refusal.
- Retain real rollback for present/absent aggregates, abandonment with foreign-change reporting and unchanged malformed-state compatibility.

## Capabilities

### Modified Capabilities

- `owned-entry-reconciliation`: recovery cleanup rechecks original state after aggregate observation.

## Impact

R2 child of #586, stack #560. Existing validator reused; no new production helper/type/public API/dependency/budget/source-comment/content/durability policy. This sampled check does not promise atomic uncooperative-writer exclusion, complete snapshots or a new rollback transaction. Backup inventory and remaining ownership/recovery/editor/deinit/report-appendix closure remain tracked.
