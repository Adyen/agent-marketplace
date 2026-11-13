# How to back up and restore machine resources

Keep a copy of your machine-wide `agent-marketplace` setup and restore it on the same machine or a new one.

## What to back up

`agent-marketplace` keeps four kinds of data on your machine. Each needs different handling:

1. Harness folders. These are the machine-wide folders that `agent-marketplace` writes into, such as `~/.claude/skills/`, plus shared configuration files such as `~/.claude.json`. They contain generated output and may also contain files you wrote yourself. `sync` can recreate the generated part from your configuration and lock. The `backup` command archives these folders.
2. Configuration and lock: `~/.agents/config.yaml` and `~/.agents/lock.yaml`, plus any files the configuration pulls in with `include`. Each harness backup also writes an `agents-config` archive with these files (see below), so `backup create` and `sync` cover them. You can still copy them yourself if you want them outside the backups folder.
3. Durable state under `$XDG_STATE_HOME/agent-marketplace/` (default `~/.local/state/agent-marketplace/`). It holds the [ownership state](../glossary.md#ownership-state) that records which entries in shared configuration files `agent-marketplace` wrote, and the safety backups it takes before changing those files. Copy this yourself.
4. The [cache](../glossary.md#cache) at `~/.cache/agent-marketplace/`. You do not need to back it up. `sync` downloads it again, as long as the configured remotes or Marketplace are reachable and still serve the exact commits in your lock.

## Back up the harness folders

1. Create a backup:

   ```bash
   agent-marketplace backup create
   ```

   The command prints the targets it covered, for example `Backup created for: agents-config, claude`. It archives the folders `agent-marketplace` writes into (skills, commands, agents, hooks, MCP configuration), including any of your own files inside them. Skills installed as symlinks are archived as symlinks, so their content stays in the cache. If backups are turned off in your configuration, the command prints `Backup is disabled in config; skipping.`

2. List the backups:

   ```bash
   agent-marketplace backup list
   ```

   Archives are stored under `~/.agent-marketplace/backups/<harness>/`. Most harnesses get one timestamped `.tar.gz` file per backup. Codex writes to two folders, so it gets one archive per folder plus a `.set.json` file that groups them. The `agents-config` group holds the configuration archive described below. With one backup for Claude, the output looks like this:

   ```text
   Backups directory: /home/you/.agent-marketplace/backups

   agents-config (1):
     2026-10-12-230443

   claude (1):
     2026-10-12-230443
   ```

   With no backups it prints `No backups found.`

`sync` also creates one of these backups before it changes anything, unless you pass `--no-backup`. Old archives are deleted automatically: by default `agent-marketplace` keeps at most 10 per day and archives from the 14 most recent days. To change this, add a `backup:` block to `~/.agents/config.yaml` with `enabled`, `max_days`, or `max_per_day`.

## The `agents-config` archive

Whenever `sync` or `backup create` writes harness backups, it also writes `~/.agent-marketplace/backups/agents-config/<timestamp>.tar.gz` with permission 0600. The archive holds `config.yaml`, `lock.yaml`, and every `include` fragment inside `~/.agents/`, named by their paths relative to `~/.agents/`, with the bytes on disk when the command started. A fragment outside `~/.agents/` is skipped and named in a debug log line. A missing file is left out, and no archive is written when none of these files exist.

No `agents-config` archive is written when backups are disabled in configuration, with `sync --no-backup`, or by the read-only modes `sync --check`, `sync --locked`, and `lock --check`. The archives follow the same `max_days` and `max_per_day` rotation as the harness archives, and `backup list` shows them in their own `agents-config` group.

`backup restore --target agents-config` refuses, because `backup restore` only overlays harness folders. Inspect an archive with `tar -tzf` and restore it by hand:

```bash
tar -tzf ~/.agent-marketplace/backups/agents-config/<timestamp>.tar.gz
tar -xzf ~/.agent-marketplace/backups/agents-config/<timestamp>.tar.gz -C ~/.agents
```

Extracting overwrites `config.yaml`, `lock.yaml`, and the archived include fragments under `~/.agents/`.

## Restore a harness folder from a backup

```bash
agent-marketplace backup restore --target claude
```

Replace `claude` with the harness you want. Valid values are `claude`, `codex`, `cursor`, `devin`, `factory`, `gemini`, and `windsurf`. It prints `Overlay-restored claude from backup <timestamp>`.

The command copies the newest archive for that harness back into the machine-wide harness folder. Files in the archive overwrite files at the same paths. Files created after the backup stay in place.

## Back up configuration, lock, and durable state

The `agents-config` archive described above already covers `~/.agents/config.yaml`, `~/.agents/lock.yaml`, and the `include` fragments inside `~/.agents/`; keeping `~/.agent-marketplace/backups/` keeps them. `backup` does not cover durable state. This snippet (bash or zsh) packs the files that exist into one archive in the current folder:

```bash
state_dir="${XDG_STATE_HOME:-$HOME/.local/state}/agent-marketplace"
files=()
for f in ~/.agents/config.yaml ~/.agents/lock.yaml "$state_dir"; do
  [ -e "$f" ] && files+=("$f")
done
tar -czf agent-marketplace-machine-state.tar.gz "${files[@]}"
```

The snippet leaves `agent-marketplace-machine-state.tar.gz` in the current folder. Because the paths are absolute, `tar` may print a `Removing leading '/'` note while it builds the archive; that is expected and the archive is still usable.

If your configuration uses `include` fragments outside `~/.agents/`, add those files to the list. The `agents-config` archive only covers fragments inside `~/.agents/`. To take the harness-folder and configuration archives with you as well, add `~/.agent-marketplace/backups`.

## Restore on a new machine

`backup restore` only reads archives under `~/.agent-marketplace/backups/` on the machine where you run it. To use them on a new machine, copy that folder across first.

1. [Install `agent-marketplace`](../install.md) on the new machine.
2. Copy `agent-marketplace-machine-state.tar.gz` to the new machine and extract it. `tar` drops the leading `/` from paths when it creates an archive, so if your home folder has the same path on both machines, extract from the root folder: `tar -xzf agent-marketplace-machine-state.tar.gz -C /` (prints nothing on success). Otherwise extract it anywhere and move the files to the same places under the new home folder.
3. Download the locked snapshots and write the harness output without changing your configuration or lock:

   ```bash
   agent-marketplace sync --locked
   ```

   On success it prints the same `Installed ... Removed ...` summary as a normal `sync`. If the restored lock does not match the restored configuration, this stops with `lock.missing` or `lock.stale` and changes nothing. Run `agent-marketplace sync --check` only after this step: on a fresh machine the cache is empty, so a check fails with `lock.snapshot-unavailable`.
4. Confirm that everything is in sync:

   ```bash
   agent-marketplace sync --check
   ```

   When everything matches, it exits with status 0 and prints no drift summary.
5. To get back your own files from the harness folders, copy `~/.agent-marketplace/backups/` to the new machine and run `agent-marketplace backup restore --target <harness>`. If your home folder path is different on the new machine, a restore of an archive that contains symlinked skills fails with `invalid absolute symlink target`, because the links point into the old home folder's cache. In that case, rely on step 3 to recreate the skills, and extract your own files from the archive by hand with `tar`.

If your state archive did not include the configuration and lock, restore them from the newest `agents-config` archive instead: `tar -xzf ~/.agent-marketplace/backups/agents-config/<timestamp>.tar.gz -C ~/.agents`.

Ownership state is keyed by the full path of each harness folder. If your home folder path is different on the new machine, the restored state does not match the new paths. Run `agent-marketplace doctor --repair` and adopt the entries it lists as exact matches. See [How to repair owned shared configuration](./repair-owned-shared-configuration.md).

## Related

- [How to remove agent-marketplace safely](./remove-agent-marketplace-safely.md)
- [Command-line reference](../reference/cli.md)
