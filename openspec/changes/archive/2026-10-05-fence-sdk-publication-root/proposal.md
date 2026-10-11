## Why

SDK Materialize checks output root during planning, but after staging it can rename a foreign staged payload through a replaced root. Path-based attempt cleanup can also remove foreign staging. A replaced staging directory under an unchanged root similarly supplies foreign payloads.

## What Changes

- Retain captured output directory and original root/staging-directory identity after stage creation.
- Revalidate original root and staging directory before artifact publication; perform destination directory creation and rename relative to the captured root.
- Reuse captured inode-aware cleanup on all exits, joining refusal/close errors through named results.
- Recheck root before stale action; stale discovery/removal remains separately audited.

## Capabilities

### Modified Capabilities

- `materialization`: SDK root/staging-directory publication authority.

## Impact

R2 child of #541 in stack #478. Public SDK signature types remain unchanged; no dependency/helper/type/budget/source-comment change. Existing all-artifacts staging, modes, dry-run and partial-result behavior retained. Initial stage creation/writes and individual destination/staged-leaf generations remain follow-on boundaries; no whole-tree transaction or atomic uncooperative-writer exclusion.
