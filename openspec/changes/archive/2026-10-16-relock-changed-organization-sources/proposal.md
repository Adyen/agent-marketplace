## Why

When an administrator changes a Git source or `ref` in the organization configuration, `lock --scope organization` (with or without `--upgrade`) refuses with `lock.stale`, because loading the configuration rejects the stale deployed organization lock before the lock command can reconcile it. The only way out today is deleting `/etc/agent-marketplace/lock.yaml`, which also discards the exact commits of every unchanged entry.

## What Changes

- An explicit `lock --scope organization` write no longer refuses a valid organization lock just because it no longer matches the configuration. Plain `lock --scope organization` re-resolves only the entries whose source or `ref` changed, keeps unchanged entries at their exact locked commits, and drops entries that are no longer configured.
- `lock --scope organization --upgrade` keeps its existing meaning (move every entry to its current ref) and now also succeeds after a source or `ref` change.
- Consumer and read-only paths (`sync`, `sync --locked`, `status`, `lock --check`, and `lock` without `--scope organization`) still refuse a stale organization lock with `lock.stale`.
- Malformed, wrong-scope, or untrusted organization locks are still refused and never replaced.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `deployed-acquisition-locks`: the stale-organization-lock refusal gains an exception for the explicit organization lock write, and a new requirement defines how that write relocks changed sources.

## Impact

- `internal/pipeline` configuration loading (deployed organization lock validation).
- `cmd/agent-marketplace/lock.go` (`lock --scope organization`).
- Command tests for organization locks. No lock format change.
