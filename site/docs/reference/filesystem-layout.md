# Filesystem layout reference

This page lists every file and folder Agent Marketplace reads or writes outside the harness folders, and says which ones you commit, which ones you keep, and which ones you can delete. For the skill, command, subagent, hook, and MCP files that `sync` writes into each harness, see the [harnesses and output paths reference](./harnesses-and-output-paths.md).

`$XDG_STATE_HOME` below means the value of that environment variable. When it is not set, Agent Marketplace uses `~/.local/state` instead. The value must be an absolute path. Ownership and telemetry state ignore a relative value and use the default, but commands that update configuration and locks stop with the error `XDG_STATE_HOME must be absolute`.

## Repository paths

| Path | Commit it? | Contents |
|---|---|---|
| `.agents/config.yaml` | yes | Repository configuration. |
| `.agents/lock.yaml` | yes | Generated lock for the repository configuration. |
| `.agents/config.local.yaml` | no | Personal configuration for this checkout. |
| `.agents/lock.local.yaml` | no | Generated lock for the personal configuration. |
| `.agents/materialization-manifest.yaml` | no | Generated record of the regular files that `file` mode wrote for this repository, so later syncs can update and remove them. |

When `.agents/config.local.yaml` exists, the CLI adds `.agents/config.local.yaml` and `.agents/lock.local.yaml` to the repository's `.gitignore` if they are not already listed. It does not add harness output folders; decide yourself whether to commit or ignore them.

## Organization paths

| Path | Contents |
|---|---|
| `/etc/agent-marketplace/config.yaml` | Organization configuration, deployed by a system administrator. |
| `/etc/agent-marketplace/lock.yaml` | Optional organization lock. |

Consumer commands read these files and never write them. See the [configuration layers reference](./configuration-layers.md) for the ownership and permission rules they must meet.

## Machine paths

### Configuration

| Path | Contents |
|---|---|
| `~/.agents/config.yaml` | User configuration. |
| `~/.agents/lock.yaml` | Generated lock for the user configuration. |

### Cache

The [cache](../glossary.md#cache) always lives at `~/.cache/agent-marketplace/`. `XDG_CACHE_HOME` does not move it. Everything in it can be downloaded or rebuilt, so you can delete it; the next online `sync` fetches what it needs again. `sync --offline` uses only the cache, so it cannot install Git or Marketplace resources after the cache is deleted. In `symlink` mode, deleting the cache breaks the installed symlinks until that `sync` runs.

| Path | Contents |
|---|---|
| `~/.cache/agent-marketplace/compiled/<harness>/` | Resources compiled for each harness. Symlinks in harness folders point here. |
| `~/.cache/agent-marketplace/exact-git/` | Git objects and exact snapshots of locked commits (`objects/`, `snapshots/`, `locks/`). |
| `~/.cache/agent-marketplace/marketplace/` | Archives, collection manifests, and endpoint metadata downloaded from hosted Marketplaces. |
| `~/.cache/agent-marketplace.mutation.lock` | Lock file that stops two processes from changing the cache at the same time. |

### Durable state

Durable state lives in `$XDG_STATE_HOME/agent-marketplace/` (default `~/.local/state/agent-marketplace/`). It is kept outside the cache, so deleting the cache never affects it. Keep it: the paragraphs after the table explain what happens when the ownership records are lost.

| Path | Contents |
|---|---|
| `ownership/<root-id>/state.json` | Which entries in Devin CLI, Codex, and Gemini CLI shared files sync installed, one folder per harness root. |
| `locks/<root-id>.lock` | Mutex lock files that stop two processes from editing the same harness root at once. |
| `safety-backups/<root-id>/` | Copies of each shared file taken before sync changes it. Sync keeps the five newest copies per file, and takes them even when `backup.enabled` is `false`. |
| `dependencies/` | Journal of changes to configuration and lockfiles. If a command is interrupted halfway, read-only commands refuse to run and the next command that changes configuration finishes the interrupted change. |
| `telemetry/installation-id` | Random identifier for this installation, used by telemetry. |
| `telemetry/queue/` | Telemetry records waiting to be sent. |
| `telemetry/diagnostic/` | Payloads captured with `--diagnostic`, kept for review or export. |

One exception: the mutex lock files for `dependencies/` are always in `~/.local/state/agent-marketplace/dependencies/locks/`, even when `XDG_STATE_HOME` is set.

If the ownership records are deleted or damaged, sync treats every entry in the affected shared files as one you wrote. It keeps them all, removes nothing, and refuses to overwrite an entry with the same name. For Devin CLI and Codex files, `doctor --repair` can take ownership again of entries that exactly match what sync would install. Moving a repository to a new path has the same effect for that repository, because records are keyed by the root's full path.

### Backups, logs, and manifests

| Path | Contents |
|---|---|
| `~/.agent-marketplace/backups/<harness>/` | Timestamped `.tar.gz` archives of machine harness folders, taken before `sync` changes them and by `backup create`. Restore them with `backup restore`. The [`backup` field](./configuration.md#root-fields) controls retention. |
| `~/.agent-marketplace/logs/agent-marketplace.log` | Default structured log file. See the [logging reference](./logging.md). |
| `~/.agent-marketplace/materialization-manifest.yaml` | Generated record of the regular files that `file` mode wrote into machine harness folders. |

## What to keep and what to delete

- Configuration files and locks are the source of truth. Commit the repository ones and keep the user ones.
- The cache is disposable.
- Durable state records what sync owns. Keep it.
- Logs and backups are runtime data. Delete old ones when you no longer need them.
- Materialization manifests are generated. Do not commit them or edit them by hand.

## Related documentation

- [Configuration layers reference](./configuration-layers.md)
- [Harnesses and output paths reference](./harnesses-and-output-paths.md)
- [Lockfiles reference](./lockfiles.md)
