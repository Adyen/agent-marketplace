# Proposal

## Why

Skipping an effective root selection can revive a superseded fragment pin, and removal can mistake an unrelated active native hook for missing evidence. These existing recovery and removal boundaries need explicit normative coverage and bindings to their regression tests.

## What Changes

- Specify that skipping a missing selection uses only the current effective selection for its exact source and locator.
- Specify that an unrelated active native hook does not block removal of an unrecorded optional direct non-hook selection, while hook and hook-containing collection removals retain their evidence requirements.
- Bind the new scenarios to the existing command and pipeline regression tests.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `deployed-acquisition-locks`: Preserve current selection precedence when a missing root selection is skipped.
- `resource-removal`: Distinguish unrelated active hooks from evidence required to remove hooks or collections.

## Impact

- OpenSpec deltas for `deployed-acquisition-locks` and `resource-removal`.
- Spec annotations on existing tests only; no runtime code changes.

## Release note

A missing optional Marketplace selection no longer restores a revision superseded by the root configuration. An unrelated active hook no longer blocks removal of an unrecorded skill, command, subagent, or MCP server.
