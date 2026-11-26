## Why

The right-hand detail summary pane displays full 71-character `sha256:<64 hex>` strings for both `Revision:` and `Digest:`, wrapping awkwardly and cluttering the pane with unmemorable hashes—often showing the exact same digest twice when a Marketplace resource revision equals its content digest.

## What Changes

- Abbreviate algorithm-qualified `sha256:` digests and revisions in the TUI right-hand summary pane to `sha256:<first 12 hex>…` (similar to short Git commit hashes), while leaving non-digest human-readable revisions (such as tags or short refs) unchanged.
- Omit the redundant `Revision:` line in the summary pane when `Revision` and `Digest` are identical.
- Retain the complete, unabbreviated digest in the full-screen detail view (`enter`) and never truncate underlying verification or installation digests.

## Capabilities

### Modified Capabilities

- `tui-rendering`: abbreviated digest display in the summary pane with full digest preserved in full detail.

## Impact

`internal/tui/detail.go`, detail unit tests, and `tools/tui-qa/tests/`. Verification, locking, and installation continue to use full exact digests.

## Release note

Abbreviated SHA-256 digests in the TUI summary detail pane and removed duplicate Revision/Digest lines while keeping full digests in the full detail view.
