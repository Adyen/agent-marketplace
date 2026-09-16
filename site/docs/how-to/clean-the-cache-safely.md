# How to clean the cache safely

Delete disposable cache entries without breaking committed locks or shared ownership.

## 1. Inspect what would be collected

```bash
agent-marketplace gc --dry-run
```

## 2. Collect the cache

```bash
agent-marketplace gc
```

## What `gc` reclaims

- `~/.cache/agent-marketplace/` holds cloned Git object stores and compiled snapshots.
- `gc` removes unreferenced exact Git snapshots, abandoned staging directories, and unreferenced hosted Marketplace artifacts from the current context.
- The current context is the organization, user, current repository, and personal-local layers and their includes.
- `gc` does not inventory other repositories. Running it in one repository can evict a snapshot that another repository still needs. The other repository will reacquire the exact commit from the remote on its next `sync`. Reacquisition can fail offline or when the remote no longer serves the locked commit.
- Durable ownership state under `$XDG_STATE_HOME/agent-marketplace/` (default `~/.local/state/agent-marketplace/`) is not part of the cache and is not touched.
- Committed `.agents/lock.yaml` is not part of the cache and is not touched.

## When to clean

- The cache has grown large.
- `sync` reports a stale snapshot.
- You need to force a fresh clone after a private registry URL changes.

## Recovery

- **`sync` is slow after cleaning**: this is expected, because it is refetching exact commits.
- **Offline `sync` fails after cleaning**: run a normal `sync` while online to reacquire the exact commits recorded in the lock.
