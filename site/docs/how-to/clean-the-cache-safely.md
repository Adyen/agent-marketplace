# How to clean the cache safely

Free disk space by deleting cached registry copies that your current configuration no longer uses. The `gc` ("garbage collect") command decides what is unused. It never deletes committed locks or ownership state.

## Prerequisites

- `agent-marketplace` on your `PATH`.
- At least one configuration file (organization, user, repository, or personal-local). `gc` refuses to run without one, because it could not tell which cache entries are still needed.
- Run `gc` from inside a repository if you want that repository's configuration and lock to count as "in use".

## 1. Preview what would be deleted

```bash
agent-marketplace gc --dry-run
```

The output lists the folders it would delete, then two totals:

```text
Would remove 3 cache entries
Would remove 1 marketplace artifact directories
```

## 2. Delete the unused entries

```bash
agent-marketplace gc
```

The real run prints the same report with `Removed` instead of `Would remove`, for example `Removed 3 cache entries` and `Removed 1 marketplace artifact directories`. `gc` checks every entry again before it deletes anything, and stops if that check fails.

## What `gc` deletes

The [cache](../glossary.md#cache) lives in `~/.cache/agent-marketplace/`. `gc` deletes:

- cached clones and Git object stores for registries that no current configuration layer references;
- exact Git snapshots whose commit no current lockfile records;
- leftover temporary folders from interrupted downloads;
- downloaded Marketplace artifacts that no current configuration references.

"Current" means the organization, user, repository, and personal-local configuration files that apply where you run the command, including the files they pull in with `include`, and their lockfiles.

`gc` does not look at other repositories on your machine. Running it in one repository can delete a snapshot that another repository still uses. That repository downloads the locked commit again on its next `sync`, which fails if you are offline or the remote no longer serves that commit.

## What `gc` leaves alone

- The `compiled/` folder inside the cache, which holds the files that symlinked harness output points to.
- Durable ownership state under `$XDG_STATE_HOME/agent-marketplace/` (default `~/.local/state/agent-marketplace/`). It is outside the cache.
- Your `.agents/lock.yaml` and every other lockfile and configuration file.
- Backup archives under `~/.agent-marketplace/backups/`.

## When to clean

- The cache has grown large.
- You removed a registry or changed its URL, and the old copy still takes up space.

## Recovery

- `sync` is slower after cleaning: it downloads the exact commits recorded in the lock again.
- `sync --offline` fails after cleaning, because the snapshot it needs is gone. Run a normal `agent-marketplace sync` while online to download the locked commits again.
- `gc requires at least one effective configuration layer`: create a configuration (for example with `agent-marketplace init`) or run `gc` from a repository that has one.
