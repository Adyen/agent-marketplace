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
tar -czf agent-marketplace-machine-state.tar.gz \
  ~/.agents/config.yaml \
  ~/.agents/lock.yaml \
  "${XDG_STATE_HOME:-$HOME/.local/state}"/agent-marketplace
```

## Restore

`backup restore` unpacks a backup that is already on the same machine. To restore on a new machine, first copy the backup storage to that machine.

1. Install `agent-marketplace` on the new machine.
2. Copy the state archive to the new machine and extract it.
3. Run `agent-marketplace sync --check` to verify the lock matches the source.
4. Run `agent-marketplace sync` to materialize any missing output.
5. If a harness needs the generated output restored first, use `agent-marketplace backup restore --target <harness>` before or after `sync`.

## Related

- [How to remove agent-marketplace safely](./remove-agent-marketplace-safely.md)
- [Command-line reference](../reference/cli.md)
