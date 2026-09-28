# How to keep resources reproducible

Pin a repository's resources to exact Git commits so every clone and every CI run produces the same files.

## Prerequisites

- A repository with `.agents/config.yaml` already configured (see [how to adopt agent-marketplace in a repository](./adopt-agent-marketplace-in-a-repository.md)).
- At least one remote Git registry in that configuration. Directory sources are read live and never appear in a lock.
- `agent-marketplace` v4.0.9 or newer everywhere the lock is read, including CI. Sync writes the lock in the `agent-marketplace-lock/v2` format, and older versions cannot read it.

## 1. Commit the generated lock

Run a normal sync to create or update `.agents/lock.yaml`:

```bash
agent-marketplace sync
```

Check that the file exists:

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

If the repository has an older `agent-marketplace-lock/v1` lock, the next writing `sync` or `lock` rewrites it as v2, and `lock --check` reports `lock.version-upgrade-required` until then. Commit the rewritten lock too.

## 2. Verify lock freshness in CI

A fresh CI runner has the committed configuration and lock, but not the [cache](../glossary.md#cache) of registry snapshots or the ignored harness output. `sync --check` fetches neither, so running it alone fails even on a healthy checkout, with `lock.snapshot-unavailable` or with output drift. Prepare the checkout first, then check it:

```bash
agent-marketplace sync --locked
agent-marketplace sync --check
```

- `sync --locked` fetches the exact commits recorded in the lock and writes the harness output. It never writes configuration or locks. If the committed lock is missing, it fails with `lock.missing`; if the lock no longer matches the configuration, it fails with `lock.stale`. Either way the CI job fails at this step.
- `sync --check` then compares the prepared checkout with what sync would produce. A healthy checkout prints no drift report and exits `0`. A stale lock or missing or extra output exits `1` and prints the reason.

Do not prepare the runner with a plain `agent-marketplace sync`. A plain sync updates a stale lock in place, which hides exactly the problem the CI job should catch.

## 3. Advance locks on purpose

A lock keeps its commit when the branch or tag it came from moves. To move a Git registry to the commit its `ref` points to now:

```bash
agent-marketplace lock --upgrade
```

`lock --upgrade` changes only the lock. Run `agent-marketplace sync` afterwards to update the harness files, or use `agent-marketplace update`, which advances the locks and syncs in one step. Then commit the updated `.agents/lock.yaml`.

## 4. Avoid editing locks by hand

The CLI generates the lockfile. Editing it by hand bypasses the checks the CLI makes against the source, and is not supported.

## Troubleshooting

- If `sync --check` reports `lock.snapshot-unavailable`, the exact snapshot is not in the cache. Run `agent-marketplace sync --locked` to fetch it without touching configuration or locks. If updating the lock is acceptable, run a writing `agent-marketplace sync` or `agent-marketplace lock` instead.
- If the lock changes although nobody changed the configuration, someone ran `agent-marketplace lock --upgrade` or `agent-marketplace update`. A healthy lock never moves on its own when `main` moves. Commit the result if the change was intended.
- If CI fails with `lock.invalid-document`, check `agent-marketplace --version` in the CI job first. Versions older than v4.0.9 cannot read the lock, so upgrade the job. If the version is current, the lockfile itself is damaged; see the [lockfile reference](../reference/lockfiles.md).

For the lock format and every lock error, see the [lockfiles reference](../reference/lockfiles.md).
