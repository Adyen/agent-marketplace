## Why

When materialization refuses an unsafe output-directory ancestor, users see only `fsboundary: unsafe path: unsafe path component skills`. The message names a bare component, not the file to fix, so users cannot tell which directory caused the refusal.

## What Changes

- Name the absolute path of the refused ancestor in `materialize.destination.error` refusals and state whether it is a symbolic link or not a directory.
- Include the offending path in the related fsboundary and harness-root refusals that currently omit it.
- Keep every refusal decision, error identity, and preserved external state unchanged.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `materialization`: Output-directory ancestor refusals name the offending absolute path and reason.

## Impact

Fsboundary descriptor naming and refusal messages, harness root capture messages, materialization destination messages, and their regression tests. No configuration, lock, ownership-state, or wire-format change; no new dependencies.
