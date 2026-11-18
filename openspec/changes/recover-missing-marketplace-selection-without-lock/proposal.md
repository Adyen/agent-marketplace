## Why

When a repository selects a Marketplace resource that was removed upstream before the repository ever recorded it in its acquisition lock, a writing `sync` cannot resolve it (latest-revision lookup returns 404). Today that one selection fails lock creation for the whole layer: no lock is written and no other selected resource installs. `sync --check` and `status` then report `v4.marketplace-lock-required` with a hint to run `sync`, which is the command that just failed, `sync --prune-stale` skips pruning because the catalog could not be trusted, and `remove` refuses with the same lock-required error. The only way out is editing configuration by hand, and no message says so.

## What Changes

- `remove` accepts a named Marketplace selection from a writable layer even when that layer has no acquisition lock or its lock lacks a record for the selection. It edits configuration only and then syncs as usual.
- A writing `lock` or `sync` that adds a lock record for a non-required Marketplace selection whose latest-revision lookup returns 404 records no entry for it, reports a warning naming the resource with the `agent-marketplace remove <installed name>` remedy, and records and materializes every other selection. The run exits 0 when nothing else fails.
- A required selection in the same situation still fails, with an error that names the resource and the configuration entry that marks it required, and writes no lock.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `resource-removal`: Marketplace selections are removable without an acquisition lock record.
- `deployed-acquisition-locks`: lock creation skips non-required Marketplace selections removed upstream.

## Impact

Marketplace lock acquisition (`internal/pipeline`, `internal/marketplace`), named removal (`cmd/agent-marketplace` remove path and `internal/config` v4 editor), and their tests. No configuration, lock, or Marketplace API format change. Read-only modes keep reporting `v4.marketplace-lock-mismatch` for a selection with no lock record. Organization locks are never written implicitly.
