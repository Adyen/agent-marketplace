# How to remove agent-marketplace safely

Remove `agent-marketplace` artifacts from a repository or machine without deleting your own files.

## Remove from a repository

1. Preview what `deinit` would remove in the repository scope:

```bash
agent-marketplace deinit --scope repo --dry-run
```

2. Run the scoped `deinit`:

```bash
agent-marketplace deinit --scope repo
```

This removes generated harness output. It does not delete `.agents/config.yaml`, `.agents/lock.yaml`, `.agents/config.local.yaml`, `.agents/lock.local.yaml`, organization policy, the repository itself, or durable machine state.

3. Only purge the repository config and lock when you intend to:

```bash
agent-marketplace deinit --scope repo --purge-config
```

This removes `.agents/config.yaml` and `.agents/lock.yaml` in the repository. It does not remove `.agents/config.local.yaml` or `.agents/lock.local.yaml`; those require `--scope local`.

4. Commit any remaining `.gitignore` or tracked changes.

## Remove from a machine

1. Preview what `deinit` would remove in the user scope:

```bash
agent-marketplace deinit --scope user --dry-run
```

2. Run scoped `deinit`:

```bash
agent-marketplace deinit --scope user
```

This removes generated machine output and orphaned caches that are not resolvable from the current organization lock. It does not delete durable ownership and backup state.

3. Only purge the config intentionally:

```bash
agent-marketplace deinit --scope user --purge-config
```

This removes `~/.agents/config.yaml` and `~/.agents/lock.yaml`.

## What `deinit` never does

- It never removes durable ownership state under `${XDG_STATE_HOME:-$HOME/.local/state}/agent-marketplace/`.
- It never removes hand-authored harness files, comments, or unrelated settings.
- It never deletes aggregates or ownership state by hand.

## Troubleshooting

- **Generated directories remain after `deinit`**: run `agent-marketplace status` to see which scope still owns them.
- **An invalid organization lock blocks teardown**: the organization administrator must repair or regenerate the organization lock. Repository users must not remove or edit organization policy.
