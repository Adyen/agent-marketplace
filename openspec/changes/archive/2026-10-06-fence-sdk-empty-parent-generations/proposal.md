## Why

SDK post-prune empty-parent cleanup inspects a captured inode, then reopens the directory by path and removes its name. Replacing that empty directory after inspection deletes the foreign generation and reports success.

## What Changes

- Reuse the existing captured output root, no-follow directory opener and descriptor-relative Rmdir for empty-parent removal.
- Match captured parent/directory identity and admitted ancestry immediately before removal; return typed generation/inspection/close refusal through public SDK sync.
- Preserve nonempty-directory stop behavior and real nested stale/empty-parent pruning.

## Capabilities

### Modified Capabilities

- `materialization`: SDK empty-parent cleanup retains directory generation authority.

## Impact

R2 child of #558 after merged #551; new continuation stack because #478 references deleted merged tail. No new helper/type/API/dependency/budget/source-comment change. Existing captured root/Dir/NativeRoot/candidate parent snapshots remain authority; no whole-operation atomicity/uncooperative-writer namespace CAS. Quarantined-payload final deletion/restoration and removal-report dispositions remain audited separately.
