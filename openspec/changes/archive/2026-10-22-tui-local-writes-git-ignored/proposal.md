## Why

A TUI write to the personal-local layer (`l` or `e` in the toggle modal, or a local bulk toggle) created `.agents/config.local.yaml` without git-ignoring it. Only a pipeline sync updated `.gitignore`, so until the next sync `git status` listed the personal-local files as untracked, and `git add -A` would commit them (#870). The CLI's write commands sync immediately, so only the TUI had the gap.

## What Changes

- After a successful personal-local write, the TUI's scoped and bulk toggle commands call `pipeline.EnsureLocalConfigGitignored`, the same idempotent helper sync uses.
- `tui-interactions` adds "Personal-local writes are git-ignored at once".

## Capabilities

### New Capabilities

### Modified Capabilities
- `tui-interactions`: personal-local writes keep that layer out of Git immediately.

## Impact

`internal/tui` scoped and bulk toggle commands. The helper is a no-op when the personal-local config does not exist and appends only missing entries.
