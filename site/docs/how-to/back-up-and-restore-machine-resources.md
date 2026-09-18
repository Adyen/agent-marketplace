# How to back up and restore machine resources

Back up machine-scope resources and keep generated output separate from config, locks, and durable ownership state.

## What to back up

`agent-marketplace` keeps three separate groups on the machine:

1. **Generated output**: managed skill directories, subagent files, hooks, and MCP servers under harness roots. Recreate these with `sync` from the lock.
2. **Config and locks**: `~/.agents/config.yaml` and `~/.agents/lock.yaml`. Copy these to a new machine to restore the registry selections.
3. **Durable ownership state and mandatory safety backups**: under `$XDG_STATE_HOME/agent-marketplace/` (default `~/.local/state/agent-marketplace/`). This proves which aggregate entries `agent-marketplace` owns.

The cache at `~/.cache/agent-marketplace/` does not need to travel with the backup, but it can be rebuilt only when the configured remotes or Marketplace are still available and still serve the exact locked commits.

## Back up generated output

Create a machine-scoped backup of generated harness output:

```bash
agent-marketplace backup create
```

List available backups:

```bash
agent-marketplace backup list
```

Restore one harness root:

```bash
agent-marketplace backup restore --target <harness>
```

## Back up config, locks, and durable state

`backup` covers generated output. Copy the durable state and config separately:

```bash
state_dir="${XDG_STATE_HOME:-$HOME/.local/state}/agent-marketplace"
files=()
for f in ~/.agents/config.yaml ~/.agents/lock.yaml "$state_dir"; do
  [ -e "$f" ] && files+=("$f")
done
tar -czf agent-marketplace-machine-state.tar.gz "${files[@]}"
```

On-demand `backup create` archives live under `~/.agent-marketplace/backups/<harness>/`; include that directory too if you want them to travel.

## Restore

`backup restore` unpacks an archive that is already present under `~/.agent-marketplace/backups/` on the same machine. To restore on a new machine, copy that directory across first.

1. Install `agent-marketplace` on the new machine.
2. Copy the state archive to the new machine and extract it. This restores `~/.agents/config.yaml`, `~/.agents/lock.yaml`, and the durable ownership state.
3. Fetch the exact locked snapshots and materialize the output without changing config or locks:

   ```bash
   agent-marketplace sync --locked
   ```

   A fresh machine has an empty cache, so a bare `sync --check` at this point fails with `lock.snapshot-unavailable`. `sync --locked` populates the cache from the locked commits and exits non-zero with `lock.missing` or `lock.stale` if the restored lock does not match the restored config.
4. Confirm everything is in sync:

   ```bash
   agent-marketplace sync --check
   ```

5. If a harness needs generated output that only the archive holds (for example, entries you had hand-adopted), copy `~/.agent-marketplace/backups/` over and run `agent-marketplace backup restore --target <harness>`.

Ownership state records the canonical harness root paths. If the new machine uses a different home directory, the restored state does not match the new roots; run `agent-marketplace doctor --repair` and adopt the entries it reports as exact matches.

## Related

- [How to remove agent-marketplace safely](./remove-agent-marketplace-safely.md)
- [Command-line reference](../reference/cli.md)
