## Why

Journal cleanup pins its directory and journal generation but does not recheck that the directory is still the named metadata parent. Replacing that public name with a foreign directory makes Apply delete the journal from a detached original directory and report success.

## What Changes

- Reuse the existing metadata-parent generation validator immediately before journal deletion.
- Refuse detached/replaced named parents in real Apply and restart Recover, preserving original/foreign metadata and committed native state.
- Keep genuine committed-generation recovery and existing journal leaf/durability/error contracts.

## Capabilities

### Modified Capabilities

- `owned-entry-reconciliation`: journal cleanup retains named metadata parent authority.

## Impact

R2 child of #587, stack #560. Existing checker and captured removal reused; no new production helper/type/public API/dependency/budget/source-comment/content/durability policy. Native/state publication can already be committed at this boundary; refusal does not promise transaction rollback, complete snapshots or atomic uncooperative-writer exclusion. Remaining ownership/recovery/namespace/editor/deinit/backup inventory/report-appendix closure stays open.
