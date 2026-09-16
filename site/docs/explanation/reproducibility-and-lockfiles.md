# Reproducibility and lockfiles

`agent-marketplace` separates selection policy from exact acquisition. The configuration file declares what you want; the lockfile records exactly what was resolved.

## Why two files

- `.agents/config.yaml` is human-authored. It contains registry URLs, refs, and resource selectors.
- `.agents/lock.yaml` is generated. It records the exact Git commit and source manifest that `sync` used.

This split lets teams review and commit the policy while trusting the lock to recreate the same bytes on any machine.

## What the lock records

For each remote Git requirement, the lock contains:

- The canonical origin.
- The declared ref.
- The exact resolved commit.
- A `revision_digest` for each ingested root, which is a SHA-256 digest of the source manifest.
- For each selected resource, the `content_digest` and `archive_digest`.

## How sync uses the lock

A normal `sync` opens the Git source at the exact commit in the lock, verifies the source manifest, and materializes the selected resources. If the lock is missing or stale for a writable layer, `sync` reconciles it first.

## Lock ownership

Each layer owns its lock. Organization, user, repository, and repository-personal locks are separate. A missing organization lock is intentional; it leaves that layer mutable. A present organization lock is exact and fail-closed.

## Plain local and Marketplace

Plain local-directory registries are live and are not locked. Hosted Marketplace resources use exact pins in consumer configuration instead of Git lockfile entries.

## Advancing locks

Locks advance when `agent-marketplace lock --upgrade` or `agent-marketplace update` resolves a Git requirement to a newer commit. The updated `.agents/lock.yaml` records the new exact state.

See [How to keep resources reproducible](../how-to/keep-resources-reproducible.md).

## Related documentation

- [Lockfiles reference](../reference/lockfiles.md)
- [How to keep resources reproducible](../how-to/keep-resources-reproducible.md)
