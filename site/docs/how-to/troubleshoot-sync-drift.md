# How to troubleshoot sync drift

Find out why `agent-marketplace sync --check` fails and fix the most common causes. [Drift](../glossary.md#drift) means the files in your harness folders, or your lock, no longer match what the configuration asks for.

## Prerequisites

- A repository configured with `.agents/config.yaml`.
- `agent-marketplace` on your `PATH`.

## 1. Read the status report

```bash
agent-marketplace status
```

Check these sections:

- `TARGET HARNESS SET` shows the harnesses that sync writes to, and the configuration layer that chose them.
- `EXACT REGISTRY LOCKS` shows the commit each Git registry is locked to.
- `SYNCED RESOURCES` lists the resources sync expects to write, with their harness, layer, and registry.
- `ORPHANED FILES`, `NAME COLLISIONS`, and `UNMET REQUIREMENTS` appear only when there is something to report.
- `DRIFT` shows `In sync`, a count of files to write and remove, `Unknown (exact registry view incomplete)` when an error stopped the check, or `Unknown (Marketplace cache evidence missing; run 'agent-marketplace sync')` when a selected Marketplace pin lacks verified local cache evidence.
- `ERRORS` lists failures such as unreachable registries, invalid configuration, or broken resources.

## 2. Inspect or fix

To see what sync would change without writing anything:

```bash
agent-marketplace sync --check
```

When there is drift, it prints a line such as `sync: 1 out of sync (1 missing)`. Then it lists each affected folder, with `+` for a file it would write and `-` for a file it would remove, and exits `1`.

To apply the changes and update a stale lock:

```bash
agent-marketplace sync
```

It prints the `Installed <n>  Removed <n>  Malformed <n>  Errors <n>` summary. When sync writes or removes output, it also prints a `→` line listing the changed output roots.

## 3. Common causes and fixes

| Symptom | Cause | Fix |
|---|---|---|
| `missing` in `sync --check` | Sync would write files that are absent or out of date, for example after you added a resource or deleted a generated folder | Run `agent-marketplace sync` |
| `orphaned` in `sync --check`, or an `ORPHANED FILES` section | Generated files remain for resources that are no longer selected | Run `agent-marketplace sync` to remove them |
| A `resources are provided by multiple registries` warning, or a `NAME COLLISIONS` section | Two registries provide a resource with the same name, and only one of them is written | Use `remove` with `--from` and `--path`, or add an `exclude` to the source you do not want |
| An `UNMET REQUIREMENTS` section, or a `requires … but it is not selected` warning | A selected resource needs another resource that is not selected | Install the required resource. The `FIX` column or the warning's `→` line shows the suggested command. See the [resource prerequisites reference](../reference/resource-prerequisites.md) |
| `Malformed` above `0` in the sync summary | A resource file failed validation and was left out | Run `agent-marketplace sync --verbose` to see the file and error, then fix the resource in its registry |
| `lock.stale` or `lock.missing` | The committed lock no longer matches the configuration (a Git registry, `ref`, or `path` was added, removed, or changed), or no lock was ever generated | Run a writing `agent-marketplace sync` or `agent-marketplace lock`, then commit the lock |
| `v4.marketplace-lock-required` or `v4.marketplace-lock-mismatch` | The configuration selects a hosted Marketplace resource that the layer's lock does not record, or the lock or endpoint record is missing | Run `agent-marketplace sync` while online (not `--check`, `--locked`, or `--offline`) to add the selected resource to the lock while keeping existing pins, then commit the lock |
| The lock does not change although the remote branch moved | This is expected. A healthy lock keeps its exact commit until you advance it | Run `agent-marketplace update` or `agent-marketplace lock --upgrade` when you want the new commit, then commit the lock |
| `locked archive digest drifted` from `sync --locked` or `sync --check` | The Marketplace repaired the archive for a resource whose content digest is unchanged, so the locked `archive_digest` no longer matches what it serves | Run a writing `agent-marketplace sync` or `agent-marketplace update` to refresh the lock's archive evidence (reported as `marketplace.archive-evidence-refreshed`), then commit the lock |
| `locked archive digest mismatch` from `sync`, `sync --locked`, or `sync --check` | Your CLI predates archive evidence refresh, so it cannot accept a repaired archive for unchanged content | Upgrade `agent-marketplace`, then run a writing `agent-marketplace sync` and commit the lock |
| `cannot verify archive digest` from a sync, or `cannot verify the cached copy of archive digest` from an offline sync | The Marketplace does not serve the placeholder archive for the resource yet, and the archive it serves, or your cached copy, was compressed by a different gzip encoder than this build. The resource content is intact | For `cannot verify archive digest`, ask the Marketplace operator to upgrade the Marketplace, then run `agent-marketplace sync`. For a cached copy, run `agent-marketplace sync` while online to replace it |
| A `selected but not found` warning | The registry renamed or removed a resource you select, or the resource is not in the locked commit yet | See [how to handle a renamed or removed resource](./handle-renamed-or-removed-resources.md) |
| `revision is revoked` | A Marketplace operator revoked a revision you pinned | See [how to respond to a revoked resource](./respond-to-a-revoked-resource.md) |

Do not set `required: true` to settle a collision. `required` enforces policy, and a required resource also ignores every exclusion.

## 4. Recover from a failed sync

If a `sync` stopped partway, run a writing `sync` again:

```bash
agent-marketplace sync
```

The retried run completes the pending work and prints the usual `Installed ... Removed ...` summary. An unfinished change blocks `sync --check`, `sync --locked`, `sync --offline`, and `lock --check` with `dependency.recovery-required`. A writing `sync` or `lock` recovers the unfinished change first.

## Related documentation

- [How to keep resources reproducible](./keep-resources-reproducible.md)
- [Reference: Diagnostics](../reference/diagnostics.md)
