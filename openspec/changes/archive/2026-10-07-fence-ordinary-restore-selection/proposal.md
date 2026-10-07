## Why

Ordinary Restore selects latest archive by pathname and later recaptures its parent/payload after selection. Replacing the selected directory can provide a valid foreign archive that overwrites current native bytes and reports successful recovery.

## What Changes

- Capture the target directory before enumeration, retain the selected archive Destination generation before extraction and verify named parent authority before payload streaming.
- Reuse captured fs.Glob, strict singly linked/nonblocking bounded payload reader and existing extraction/publication, without a second writer or new read budget.
- Remove unused private path-based extraction wrapper; preserve missing-backup contract, initial aliases, mapped-only inputs, archived modes and restore-failure logging across selected-source refusals/close errors.

## Capabilities

### Modified Capabilities

- `backup-recovery`: ordinary/mapped restore retains selected archive and target directory generation authority.

## Impact

R2 child of #573, stack #560. Existing capture/read/parent-validation/extraction primitives reused; no new helper/type/public API/dependency/budget/source-comment/durability/content-policy change. Sampled selection binding is not an atomic file/namespace CAS, complete archive content freeze or all-output extraction transaction. Remaining namespace/action consumers and original report appendix/admitted-case audit remain open.
