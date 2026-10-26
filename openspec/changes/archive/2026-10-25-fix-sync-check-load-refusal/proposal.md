## Why

A registry-load refusal makes the selected hook inventory incomplete. Reporting
that missing inventory as pending deselection predicts changes that ordinary
synchronization refuses to make.

## What Changes

- Refuse synchronization checks before predicting materialized output changes
  when configuration, catalog loading, or resource resolution has a hard error.
- Preserve the actual refusal diagnostics without a hook review or drift count
  fabricated from incomplete inventory.
- Enable the unloadable-registry regression with native-file and ownership-state
  preservation assertions and loadable-source replacement/removal controls.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `hook-sync-diagnostics`: distinguish source-load refusal from genuine pending
  hook operations.

## Impact

The synchronization check uses the existing strict inspection policy already
used by status. Warning-only checks and materialization-time diagnostics retain
their existing behavior. No compilation, selection, ownership, configuration,
acquisition-lock, or TUI behavior changes.

## Release note

`sync --check` now reports registry-loading errors without incorrectly predicting
hook removals or output drift.
