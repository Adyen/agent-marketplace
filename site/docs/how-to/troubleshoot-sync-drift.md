# How to troubleshoot sync drift

Diagnose and fix the most common causes of `agent-marketplace sync --check` failures.

## Prerequisites

- A repository configured with `.agents/config.yaml`.
- `agent-marketplace` on your `PATH`.

## 1. Read the status report

Run:

```bash
agent-marketplace status
```

Check these sections:

- **TARGET HARNESS SET** confirms the active harnesses.
- **EXACT REGISTRY LOCKS** shows the resolved commits.
- **SYNCED RESOURCES** lists the resources the CLI expects to materialize.
- **DRIFT** reports `In sync` or a summary of the difference.
- **ERRORS** lists fatal failures such as unreachable registries or malformed resources.

## 2. Decide whether to reconcile or inspect

If drift exists, choose one path:

- To see the planned changes without writing:

```bash
agent-marketplace sync --check
```

- To apply the changes and refresh the lock:

```bash
agent-marketplace sync
```

## 3. Common causes and fixes

| Symptom | Cause | Fix |
|---|---|---|---|
| `missing` resources | A selected resource was removed from the registry | Remove it from configuration or pick a different ref |
| `orphaned` output | A previously generated file no longer matches configuration | Run `agent-marketplace sync` to prune stale output |
| `name collision` warning | Two registries select the same resource name | Use `remove` with `--from` and `--path`, or add an explicit `exclude` on the lower-priority source |
| `unmet requirements` | A selected resource lists a missing prerequisite | Install the prerequisite explicitly |
| `Malformed` resources | A resource file failed ingestion | Open the resource source and fix the harness layout |
| `lock.stale` or `lock.missing` | The committed lock no longer matches the configuration (a registry, ref, or path was added, removed, or edited) or the lock was never generated | Run a writing `agent-marketplace sync` or `agent-marketplace lock` to reconcile, then commit the lock |
| Lock unchanged although the remote branch moved | Expected. A healthy lock keeps its exact commit until you advance it | Run `agent-marketplace update` or `lock --upgrade` when you want the new commit, then commit the lock |

Do not set `required: true` to silence a collision. `required` is for policy enforcement, not name disambiguation.

## 4. Recover from a failed sync

If a `sync` failed partway, run a writing `sync` again:

```bash
agent-marketplace sync
```

Pending transactions block `sync --check`, `sync --locked`, and `lock --check`. A writing run reconciles any unfinished transaction state.

## Related documentation

- [How to keep resources reproducible](./keep-resources-reproducible.md)
- [Reference: Diagnostics](../reference/diagnostics.md)
