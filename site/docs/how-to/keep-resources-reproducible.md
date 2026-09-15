# How to keep resources reproducible

This guide shows how to keep a repository's generated resources locked to exact Git commits so every clone produces the same files.

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

Add a CI step that fails when the lock or generated output is stale:

```bash
agent-marketplace sync --check
```

A healthy checkout prints no drift text and exits `0`. Stale dependencies exit `1` and list the required changes.

## 3. Advance locks deliberately

To move a Git registry to the current `ref`:

```bash
agent-marketplace lock --upgrade
```

Then commit the updated `.agents/lock.yaml`.

## 4. Avoid editing locks by hand

The lockfile is generated. Manual changes bypass the source manifest verification and are not supported.

## Troubleshooting

- **`sync --check` reports `lock.snapshot-unavailable`**: The exact snapshot is absent from the cache. Run a writing `agent-marketplace sync` or `lock` to fetch it.
- **Lock changes without code changes**: A healthy, compatible lock does not silently advance when `main` moves. Only a deliberate `agent-marketplace lock --upgrade` or `agent-marketplace update` can move the lock; commit the result if you intended the change.
