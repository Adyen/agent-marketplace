# Proposal

## Why

The Marketplace worker refreshes every configured registry on a fixed interval, cloning and ingesting even when nothing changed, and it cannot coordinate that work across replicas beyond the job queue. Change-aware reindexing needs durable per-registry scheduler state, an exclusive registry work lease, and a deployment-wide host budget before any worker can safely decide when to observe or refresh a registry. This change adds that foundation to the metadata store without changing how the worker refreshes today.

## What Changes

- Add durable registry observation state to the Marketplace metadata store, kept apart from the published registry record, its head, and its immutable occurrences. It records observed, desired, and successfully indexed state, probe health, scheduling times, the registry work lease, and the active refresh.
- Add an exclusive, expiring registry work lease whose superseded holders cannot renew or release it.
- Add deterministic selection of due observations that skips leased and not-yet-due registries.
- Add compare-and-swap observation updates that reject stale writers without partial writes.
- Add deployment-wide host budgets that bound concurrent remote operations and acquisitions per host across all workers.
- Add atomic refresh requests that update desired state and coalesce onto one active refresh, and clear that active refresh when its job fails terminally.
- Upgrade existing SQLite and PostgreSQL metadata databases in place with an appended schema migration.
- The worker, its refresh interval, Git acquisition, and operator configuration loading are unchanged.

## Capabilities

### New Capabilities

- `marketplace-registry-observation`: durable registry observation state, registry work leases, host budgets, and refresh coalescing in the Marketplace metadata store, identical on SQLite and PostgreSQL.

### Modified Capabilities

None.

## Impact

- Marketplace metadata store interface and both backends (SQLite and PostgreSQL), including one new forward migration per backend.
- Terminal job failure in the metadata store also clears a matching active refresh.
- No API, CLI, TUI, web, configuration, or operator documentation change.

## Release note

none — internal Marketplace metadata-store foundation; the running worker and every operator-visible behavior are unchanged in this release.
