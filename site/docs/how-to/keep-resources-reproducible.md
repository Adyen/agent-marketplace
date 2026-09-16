# How to keep resources reproducible

Keep a repository's generated resources locked to exact Git commits so every clone produces the same files.

## Prerequisites

- A repository with `.agents/config.yaml` already configured.
- The repository has at least one remote Git registry.

## 1. Commit the generated lock

Run a normal sync to create or update `.agents/lock.yaml`:

```bash
agent-marketplace sync
```

Check the file exists:

```bash
test -f .agents/lock.yaml && echo "lock present"
```

Expected:

```text
lock present
```

Commit the lock with the configuration:

```bash
git add .agents/config.yaml .agents/lock.yaml
git commit -m "Lock agent-marketplace dependencies"
```

## 2. Verify lock freshness in CI

A fresh CI runner has the committed config and lock, but not the snapshot cache or the ignored generated output. `sync --check` acquires neither, so a bare check fails even on a healthy checkout with `lock.snapshot-unavailable` or materialization drift. Bootstrap first, then check:

```bash
agent-marketplace sync --locked
agent-marketplace sync --check
```

- `sync --locked` fetches the exact snapshots into the cache and materializes the generated output without writing config or locks. It exits non-zero with `lock.missing` or `lock.stale` when the committed lock is absent or no longer matches the configuration, so a stale lock still fails CI here.
- `sync --check` then runs as the drift gate against the prepared checkout: a healthy one prints no drift text and exits `0`; stale dependencies exit `1` and list the required changes.

Do not bootstrap with a plain writing `agent-marketplace sync`: it minimally reconciles a stale writable lock, hiding the stale-lock condition CI is meant to catch.

## 3. Advance locks deliberately

To move a Git registry to the current `ref`:

```bash
agent-marketplace lock --upgrade
```

Then commit the updated `.agents/lock.yaml`.

## 4. Avoid editing locks by hand

The lockfile is generated. Manual changes bypass the source manifest verification and are not supported.

## Troubleshooting

- **`sync --check` reports `lock.snapshot-unavailable`**: The exact snapshot is absent from the cache. Run `agent-marketplace sync --locked` to fetch it without touching config or locks, or a writing `agent-marketplace sync` or `lock` when reconciling is acceptable.
- **Lock changes without code changes**: A healthy, compatible lock does not silently advance when `main` moves. Only a deliberate `agent-marketplace lock --upgrade` or `agent-marketplace update` can move the lock; commit the result if you intended the change.
