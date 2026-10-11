## Why

The shared ownership metadata writer validates produced staging state, then passes the staged name to publication. A replaced state stage is renamed over the original state; only its post-rename pin mismatch reports conflict. The original durable state is already overwritten.

## What Changes

- Move existing metadata parent/original-state validation and produced staged-reader validation to the final publication boundary.
- Refuse substituted stages before destination mutation, preserving replacements and recoverable journal evidence.
- Cover real Apply state, prepared-journal, journal-phase and BackupOnly safety-manifest consumers with genuine recovery/retry controls.

## Capabilities

### Modified Capabilities

- `owned-entry-reconciliation`: metadata publication retains the produced staged generation.

## Impact

R2 child of #585, stack #560. Existing validator/captured-reader/publisher/cleanup reused; no new production helper/type/public API/dependency/budget/source-comment/content-policy/durability change or duplicated I/O. Native bytes may already be published at later metadata phases; this does not add transaction rollback, complete snapshots or atomic uncooperative-writer exclusion. Backup inventory, ownership/recovery/editor/deinit and original131-report appendix closure remain tracked.
