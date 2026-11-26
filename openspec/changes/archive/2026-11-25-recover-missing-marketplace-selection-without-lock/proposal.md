## Why

When an unrecorded optional direct Marketplace selection disappears before its first lock, its latest-resource lookup returns resource-not-found (HTTP 404). Today that selection fails lock creation for the whole layer: no lock is written and no other selected resource installs. `sync --check` and `status` then report `v4.marketplace-lock-required` with a hint to run `sync`, which is the command that just failed, `sync --prune-stale` skips pruning because the catalog could not be trusted, and `remove` refuses with the same lock-required error. The only way out is editing configuration by hand, and no message says so.

## What Changes

- `remove` accepts a named Marketplace selection from a writable layer even when that layer has no acquisition lock or its lock lacks a record for the selection. It edits configuration only and then syncs as usual.
- A writing `lock` or `sync` skips only an unrecorded optional direct Marketplace selection without an explicit revision when its latest-resource lookup reports resource-not-found (HTTP 404). Skipping MUST NOT discard an existing pin; an explicit revision requested by configuration may replace it. The CLI warns with the `agent-marketplace remove <installed name>` remedy. `lock` does not install resources; plain `sync` installs the remaining selections and exits 0 when nothing else fails. `sync --strict` completes the same work, then exits 1 with the remedy.
- A missing required selection in the same situation still fails with an error naming the resource and its required configuration entry, and writes no new lock.
- An unavailable explicit revision, missing registry, endpoint-level 404, or other Marketplace failure still fails without writing a new lock.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `resource-removal`: Marketplace selections are removable without an acquisition lock record.
- `deployed-acquisition-locks`: lock creation skips non-required Marketplace selections removed upstream.

## Impact

Marketplace lock acquisition (`internal/pipeline`, `internal/marketplace`), named removal (`cmd/agent-marketplace` remove path and `internal/config` v4 editor), and their tests. No configuration, lock, or Marketplace API format change. Read-only modes keep reporting `v4.marketplace-lock-mismatch` for a selection with no lock record. Organization locks are never written implicitly.

## Release note

If an unrecorded optional direct Marketplace selection without an explicit revision disappears, plain `sync` warns and installs the remaining selections, and `lock` records them without installing. `sync --strict` completes the same work and exits 1 with a removal remedy. You can remove the missing selection by name without editing the configuration by hand.
