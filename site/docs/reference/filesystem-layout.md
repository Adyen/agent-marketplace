# Filesystem layout reference

The `XDG_*` defaults below apply only when those variables are not set.

## Repository paths

- `.agents/config.yaml`: committed configuration.
- `.agents/lock.yaml`: committed generated Git lock.
- `.agents/config.local.yaml`: gitignored personal override.
- `.agents/lock.local.yaml`: gitignored generated lock for the override.
- `.agents/materialization-manifest.yaml`: generated management state used to track and reconcile regular-file mode output. It is not source to commit.

## Machine paths

- `~/.agents/config.yaml`: user personal configuration.
- `~/.agents/lock.yaml`: user personal generated lock.
- `~/.cache/agent-marketplace/`: disposable cache of clones, snapshots, and compiled artifacts.
- `$XDG_STATE_HOME/agent-marketplace/` (default `~/.local/state/agent-marketplace/`): durable state for Devin and Codex ownership and safety backups.
- `$XDG_STATE_HOME/agent-marketplace/diagnostic/`: retained `--diagnostic` telemetry payloads awaiting review or export.
- `~/.agent-marketplace/backups/<harness>/`: timestamped on-demand and pre-sync backups of machine-scope harness output (`backup create`, `backup list`, `backup restore`).
- `~/.agent-marketplace/materialization-manifest.yaml`: generated management state for file-mode machine output.
- `~/.agent-marketplace/logs/agent-marketplace.log`: default structured log file.

## Harness output paths

For per-harness generated paths, see the [harnesses and output paths reference](./harnesses-and-output-paths.md).

## State separation

- Configuration and locks are committed or persistent.
- Cache is disposable.
- Ownership state is outside the cache so cache deletion does not affect owned aggregates.
- Logs are runtime state outside the cache.
- Materialization manifests are generated management state, not committed source.

## Related documentation

- [Configuration layers reference](./configuration-layers.md)
- [Harnesses and output paths reference](./harnesses-and-output-paths.md)
