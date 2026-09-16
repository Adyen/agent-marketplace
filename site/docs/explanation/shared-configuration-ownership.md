# Shared configuration ownership

`agent-marketplace` can manage parts of Devin and Codex configuration files, but it must not delete or overwrite user-authored entries. Ownership records make that safe.

## Why ownership is outside the files

An in-file `_managed_by` marker can be written by any tool, so it cannot prove that a user did not add it. `agent-marketplace` keeps durable ownership in XDG state under `$XDG_STATE_HOME/agent-marketplace/` (default `~/.local/state/agent-marketplace/`) instead.

## What the state records

For each canonical target root, the ownership journal records which aggregate files the CLI wrote, the content hash at the time of writing, and the operation generation.

## Fail closed

When ownership state is missing or corrupt, every existing aggregate entry is treated as unmanaged. The CLI then:

- preserves every existing entry;
- refuses collisions with generated entries; and
- prunes nothing.

This avoids deleting hand-written configuration when the state is lost.

## Adoptions, orphans, and conflicts

`agent-marketplace doctor --repair` handles three cases:

- **Exact match**: the existing entry matches the desired one; the CLI can adopt it.
- **Orphan**: the entry was generated but is no longer selected; it can be removed.
- **Conflict**: the key matches but the value differs; the user must confirm replacement.

Replacements take a fresh safety backup first and never happen with `--yes` alone.

## Serialized mutations

Devin aggregate updates use advisory `flock(2)` per canonical root. `flock` normally serializes concurrent writers for the same open file description. When `flock` is unavailable, the ownership store falls back to compare-and-swap on the journal generation. `ownership.generation-conflict` occurs only when the fallback detects a conflict, not merely because `flock` is absent.

## Related documentation

- [How to repair owned shared configuration](../how-to/repair-owned-shared-configuration.md)
- [Native harness materialization](./native-harness-materialization.md)
