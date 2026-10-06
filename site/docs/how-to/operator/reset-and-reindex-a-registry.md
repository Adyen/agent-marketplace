# How to reset and reindex a registry

Rebuild one registry's catalog from its current source when a plain reindex cannot repair it, without losing registry settings, policies, revocations, or blob bytes.

The examples use the registry slug `shared-agents` and the configuration file `/app/marketplace.yaml`. Replace both with your own.

## Prerequisites

- Operator access to the host or containers that run the API and worker, and permission to stop them.
- Access to the metadata store and blob store for a backup.
- A maintenance window. The registry serves no resources from the reset until the reindex succeeds.
- A shell where the worker runs, with the same configuration file, credentials, network access, and Git. The API image does not contain Git.

## 1. Try a plain reindex first

Run a reindex of the registry:

```bash
agent-marketplace serve reindex shared-agents --config /app/marketplace.yaml
```

- If it exits `0`, the server side is done. A reindex repairs a resource whose content is unchanged but whose archive encoding changed, keeps the previous archive downloadable, and logs `resource revision archive digest repaired`. A reset would make that previous archive return `404`. The repair changed the served `archive_digest` without changing the content, so consumers whose locks still reference the old digest will fail `sync --locked` or `sync --check`. For locks this client manages (user, repository, and personal-local), tell each affected owner to run `agent-marketplace sync` and commit the updated lock file. When the repaired artifact can be re-acquired, that run reports `marketplace.archive-evidence-refreshed` and updates one `archive_digest` line per resource; if it cannot, the lock and installation stay unchanged and the run reports the acquisition failure. Locks managed by another layer, such as a deployed organization lock, must be refreshed through that owning layer. No reset or manual lock edit is needed.
- If it fails with a cause that a reset clears, continue. For example, `conflict: resource revision "sha256:…" already has different immutable metadata` means the stored revision's license or description no longer matches the source for the same content.
- If it fails for any other reason, such as a clone, credential, or policy error, fix that cause instead. A reset does not help and leaves the registry empty until a reindex succeeds.

## 2. Record what clients see now

Save the current revision metadata for the resources your teams lock. You compare it after the rebuild to decide whether consumers must act:

```bash
BASE=https://marketplace.example.com
for r in skill/review command/hello; do
  printf '%s ' "$r"
  curl -fsS "$BASE/api/v1/resources/shared-agents/$r" \
    | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d["content_digest"],d.get("archive_digest") or "-")'
done | tee before-reset.txt
```

Expected output, one line per resource:

```text
skill/review sha256:e25a8833… sha256:3a16df2e…
command/hello sha256:aa2247a9… sha256:a1ddfa7c…
```

## 3. Preview the reset

```bash
agent-marketplace serve reset shared-agents --config /app/marketplace.yaml --dry-run
```

Expected output:

```text
Would reset catalog for registry "shared-agents" (src-shared).
  blob_owner_sources: 5
  blob_registry_owners: 2
  blob_resource_owners: 17
  collection_revisions: 1
  ...
  resource_revisions: 3
Registry configuration, policies, revocations and blobs are preserved. Run serve reindex for this registry to rebuild its catalog.
```

Check that the slug and ID in the first line name the registry you intend to rebuild. A reset accepts exactly one registry. With `management.mode: database`, the selector resolves from the managed configuration, not from the `registries:` list in the YAML file.

## 4. Stop every API and worker process

Stop all processes that use this metadata store, including every API replica and worker. Then confirm that none is still serving:

```bash
curl -fsS http://<api-host>:8080/healthz
```

Expected result: the connection is refused. `serve reset` refuses to run while a refresh job holds a live lease, but it cannot detect a running API.

## 5. Back up metadata and blobs

Take a backup of both stores from the same point, following [How to back up and restore a Marketplace](./back-up-and-restore.md). A reset deletes the registry's revision, occurrence, and publication history; only this backup can bring it back.

## 6. Reset the registry

```bash
agent-marketplace serve reset shared-agents --config /app/marketplace.yaml --confirm-maintenance
```

Expected output: the same table as the preview, starting with `Reset catalog for registry "shared-agents" (src-shared).`

For SQLite, you can confirm the catalog is empty:

```bash
sqlite3 /var/lib/agent-marketplace/marketplace.db \
  "SELECT COUNT(*) FROM resource_revisions WHERE registry_id='src-shared';"
```

Expected output: `0`.

## 7. Reindex the registry

Run the reindex immediately. It is not automatic:

```bash
agent-marketplace serve reindex shared-agents --config /app/marketplace.yaml
echo "exit=$?"
```

Expected output:

```text
exit=0
```

The reindex ends with an integrity check that every published artifact has a matching blob, so exit `0` also confirms the store is consistent. If it fails, see [Troubleshooting](#troubleshooting) before restarting traffic.

## 8. Restart the Marketplace and verify

Start the API and worker again. If a reverse proxy or CDN caches Marketplace API responses, purge them now so clients do not receive pre-reset metadata. Then check readiness and download one archive:

```bash
curl -fsS http://<api-host>:9090/readyz
curl -fsS "$BASE/api/v1/resources/shared-agents/skill/review" -o rev.json
hex=$(python3 -c 'import json;print(json.load(open("rev.json"))["archive_digest"].split(":")[1])')
curl -fsS -o /dev/null -w '%{http_code}\n' "$BASE/api/v1/artifacts/sha256/${hex}"
```

Expected output:

```text
{"status":"ready"}
200
```

Repeat step 2 with `tee after-reset.txt`, then compare:

```bash
diff before-reset.txt after-reset.txt && echo unchanged
```

## 9. Tell consumers what to do

Read the comparison:

- **`unchanged`:** consumers need to do nothing. Existing locks, including `sync --locked` in CI and teammates with empty caches, keep working without a lock change.
- **Same `content_digest`, different `archive_digest`:** the previous archive now returns `404`. `sync --locked` and `sync --check` fail with `locked archive digest drifted`. Ask each repository owner to run a writing `agent-marketplace sync`, which reports `marketplace.archive-evidence-refreshed` and changes one `archive_digest` line per resource in the lock, then commit the lock. Other teammates and CI then pass `sync --locked` with the committed lock.
- **Different `content_digest`:** the source changed since the old revision was published. Consumers take it through their normal update process.

## Troubleshooting

- **`reset requires --confirm-maintenance after stopping all API/workers, backing up metadata, and invalidating shared caches`**: you ran the reset without either flag. Run the preview in step 3, then complete steps 4 and 5 before adding `--confirm-maintenance`.
- **The reindex in step 7 fails**: the registry stays empty. Read the `error` attribute on the `reindex failed` log line, fix the cause, and run step 7 again. If you cannot fix it within the window, restore the backup from step 5 with both stores from the same point.
- **An old artifact URL returns `404` after the reset**: expected for archive digests the rebuild did not publish again. The blob bytes stay on disk, but nothing references them any more. Consumers recover with a writing `agent-marketplace sync` as described in step 9.
- **A consumer reports `locked archive digest mismatch`, not `drifted`**: their CLI predates archive evidence refresh and cannot repair the lock. They must upgrade the CLI, then run a writing `agent-marketplace sync`.

## Related documentation

- [How to reindex and monitor a Marketplace](./reindex-and-monitor.md)
- [How to back up and restore a Marketplace](./back-up-and-restore.md)
- [`serve reset` command reference](../../reference/cli.md#serve-reset)
- [How to troubleshoot sync drift](../troubleshoot-sync-drift.md)
