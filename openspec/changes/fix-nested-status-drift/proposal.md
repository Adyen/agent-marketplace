## Why

When a personal-local configuration is in a nested directory, `status` resolves its resources but previews their output at the repository root. This can report writes and parent-root removals even after `sync` has reconciled the nested output.

## What Changes

- Make `status` inspect the same materialization root as `sync --check` for nested personal-local configurations.
- Specify and test in-sync and drifted nested output, including unrelated parent-owned artifacts.
- Preserve root-level inspection, read-only behavior, and exact-lock error reporting.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `status-reporting`: Define truthful drift and orphan reporting when a nested personal-local configuration determines the output root.

## Impact

The `status` command, its command tests, and the status-reporting specification change. Synchronization and ownership policies do not change.
