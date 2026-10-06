## Why

SDK mismatch restoration links the quarantine name back to the stale destination and unlinks quarantine without retaining the actual entry observed after quarantine rename. A later quarantine replacement is restored and deleted under stale authority.

## What Changes

- Capture descriptor-relative no-follow metadata of the actual quarantined entry before verification, not the older discovery leaf or newly created reservation.
- Require the observed device/inode/mode/size/link shape before restoration link and quarantine unlink; account only for the link created by this restoration attempt.
- Preserve later/unknown generations, primary stale-prune refusal and existing valid regular/FIFO/changed-leaf restoration and real healthy pruning.

## Capabilities

### Modified Capabilities

- `materialization`: SDK mismatch-restoration actions retain observed quarantine generation.

## Impact

R2 child of #561 in continuation stack #560. Existing restore function and platform Stat_t/no-follow relative syscalls reused; no new helper/type/public API/dependency/budget/source-comment change. Namespace state is bound by object/shape, not new content policy. No whole-operation transaction or atomic namespace compare-and-act against uncooperative writers. Removal reporting remains separately audited; final admitted-case audit/appendix still open.
