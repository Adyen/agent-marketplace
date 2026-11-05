## Why

Native marker aggregates detach supported aliases before SDK merge preparation. A verified target read currently outlives its captured authority: later pathname replacement or removal can overwrite a concurrent destination with stale bytes, or remove a newer destination when the captured target was empty. Actual native synchronization reproduces both data-loss paths.

## What Changes

- Retain original target authority, logical alias identity and admitted parent identity through detachment.
- Refuse observed parent, alias or target generation changes before staging or mutation, including byte-identical target replacement and same-text alias replacement.
- Bind replacement staging, publication and attempt-owned cleanup to the captured logical parent, leaving the original target untouched.
- Preserve existing alias admission, native handwritten merges, shared permissions, empty-target private creation, stable refusal diagnostics, read-only nonmutation and repeat no-ops.

## Capabilities

### Modified Capabilities

- `materialization`: native marker alias detachment retains the generation that supplied its input before SDK merging begins.

## Impact

The shared native marker alias path and captured destination validation, actual Factory/Cursor/Windsurf repository/machine regressions, existing disk-I/O benchmark and a focused contract delta. No new dependency, public SDK API, ownership policy, retry, global processing budget, hostile-writer exclusion or whole-operation transaction.
