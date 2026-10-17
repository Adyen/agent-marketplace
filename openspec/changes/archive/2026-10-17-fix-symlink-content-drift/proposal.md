## Why

`sync --check` compares cache-backed symlink targets but skips their cached
payloads. A resource can change while retaining its stable cache path, leaving
stale installed content behind a successful check.

## What Changes

- Compare desired compiled resources and support files with their installed
  cache content during read-only checking.
- Report each affected native resource path as a pending write without
  rewriting the cache or symlink.
- Preserve unchanged-link no-ops, file-mode behavior and user-owned collisions.

## Capabilities

### Modified Capabilities

- `materialization`: read-only checks detect content drift behind stable
  cache-backed symlinks.

## Impact

Shared materialization paths, cross-target materialization tests and CLI E2E
coverage. Cache identities and acquisition-lock formats remain unchanged.
