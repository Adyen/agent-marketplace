# How to back up and restore a Marketplace

Take a consistent backup of a hosted Marketplace and restore it to that point.

Back up metadata and blobs together. A metadata row refers to an archive or manifest blob by digest, so if you restore one without the other, the API fails readiness or serves incomplete history.

## Prerequisites

- Operator access to the database and blob store.
- The ability to stop the API and worker (SQLite topology) or to take a database snapshot (PostgreSQL topology).
- A record of the application version and operator configuration in use, stored with every backup.

## SQLite and filesystem

This topology runs in a single process, so a backup is consistent only when nothing is writing.

Docker Compose deployments keep state in the `marketplace-data` named volume, mounted at `/var/lib/agent-marketplace` inside the container, so the backup and restore commands run in a temporary helper container that mounts that volume. Native host deployments run the commands directly against the host path.

### Docker Compose

#### Back up

```bash
docker compose stop app

mkdir -p ./backups
docker run --rm \
  -v agent-marketplace-sqlite_marketplace-data:/var/lib/agent-marketplace \
  -v "$PWD/backups:/backups" \
  alpine sh -c '
    apk add --no-cache sqlite >/dev/null && \
    sqlite3 /var/lib/agent-marketplace/marketplace.db ".backup '\''/backups/marketplace-$(date +%F).db'\''" && \
    tar -C /var/lib/agent-marketplace -czf "/backups/blobs-$(date +%F).tar.gz" blobs && \
    chown -R 1000:1000 /var/lib/agent-marketplace
  '

docker compose run --rm --no-deps --entrypoint /app/agent-marketplace app --version > "./backups/version-$(date +%F).txt"
docker compose start app
```

Expected result: `./backups/` holds `marketplace-<date>.db`, `blobs-<date>.tar.gz`, and `version-<date>.txt`. The `sqlite3` and `tar` steps print nothing on success.

The helper mounts the volume read-write. The database uses SQLite's write-ahead log (WAL), and SQLite must be able to create its `-wal` and `-shm` files next to the database even to read it. The final `chown` gives any file the helper created back to the container user.

The volume name comes from the Compose project name (`agent-marketplace-sqlite` in the repository's `compose.yaml`). If your project uses a different name, find the volume with `docker volume ls -f name=marketplace-data` and adjust the commands.

#### Restore

```bash
docker compose stop app

docker run --rm \
  -v agent-marketplace-sqlite_marketplace-data:/var/lib/agent-marketplace \
  -v "$PWD/backups:/backups" \
  alpine sh -c '
    rm -f /var/lib/agent-marketplace/marketplace.db-wal /var/lib/agent-marketplace/marketplace.db-shm && \
    cp /backups/marketplace-<date>.db /var/lib/agent-marketplace/marketplace.db && \
    rm -rf /var/lib/agent-marketplace/blobs && \
    tar -C /var/lib/agent-marketplace -xzf /backups/blobs-<date>.tar.gz && \
    chown -R 1000:1000 /var/lib/agent-marketplace
  '

docker compose start app
```

Expected result: the restore commands print nothing on success, and `docker compose start` reports the `app` container started.

The container images run as UID and GID 1000, which is why the restore sets that owner. The restore deletes any leftover `-wal` and `-shm` files first, because SQLite would otherwise apply them to the restored database.

### Native service (systemd)

#### Back up

```bash
systemctl stop agent-marketplace
sqlite3 /var/lib/agent-marketplace/marketplace.db ".backup '/backups/marketplace-$(date +%F).db'"
tar -C /var/lib/agent-marketplace -czf "/backups/blobs-$(date +%F).tar.gz" blobs
agent-marketplace --version > "/backups/version-$(date +%F).txt"
systemctl start agent-marketplace
```

Expected result: `/backups/` holds the same three files.

Use SQLite's `.backup` command instead of `cp` on a live database file. If you snapshot the whole volume instead, capture the database and `blobs/` in the same snapshot.

#### Restore

```bash
systemctl stop agent-marketplace
rm -f /var/lib/agent-marketplace/marketplace.db-wal /var/lib/agent-marketplace/marketplace.db-shm
cp /backups/marketplace-<date>.db /var/lib/agent-marketplace/marketplace.db
rm -rf /var/lib/agent-marketplace/blobs
tar -C /var/lib/agent-marketplace -xzf /backups/blobs-<date>.tar.gz
chown -R 1000:1000 /var/lib/agent-marketplace
systemctl start agent-marketplace
```

Expected result: the restore commands print nothing on success.

Change the `chown` owner to the account your service runs as.

Every process that opens the stores (`serve` in any role, `serve reindex`, and `serve adopt`) applies pending migrations to the restored database. Restore with the same application version or a newer one, never an older one.

## PostgreSQL and S3

### Back up

- Database: run `pg_dump --format=custom` on a schedule, or use your managed provider's point-in-time recovery.
- Blobs: enable S3 versioning and a retention or object-lock policy. Do not expire objects based on an application-side listing, because the metadata store is the only record of which digests are referenced.
- Record the image tag, the operator configuration, and the exact `pg_dump` timestamp together.

### Restore

1. Scale the API and worker to zero.
2. Restore the database to the chosen point with `pg_restore` or point-in-time recovery.
3. Confirm that every blob referenced at that point still exists in the bucket. Object versioning guarantees this as long as its retention outlasts your database backups.
4. Start one API replica, wait for migrations to complete, then scale up.
5. Start the worker.

## Verify a restore

Run these checks before you open the proxy to traffic. Set `BASE` to the Marketplace's public base URL, and pick a resource from a registry that uses `acquisition: mirror` (the default). Index-only revisions have no `archive_digest`, so the fourth command fails for them.

```bash
curl -fsS http://<api-host>:9090/readyz                              # {"status":"ready"}
curl -fsS "$BASE/api/v1/registries" | python3 -m json.tool | head    # registries present
curl -fsS "$BASE/api/v1/resources/<registry>/skill/<name>" -o rev.json
hex=$(python3 -c 'import json;print(json.load(open("rev.json"))["archive_digest"].split(":")[1])')
curl -fsS "$BASE/api/v1/artifacts/sha256/${hex}" -o /dev/null        # blob readable
```

The `/readyz` check runs on the operations listener, so run it from a host that can reach that address.

If the artifact request returns `404`, the blob backup is older than the metadata backup. Restore both from the same point instead of letting the worker reindex over an inconsistent store.

## Drills

At least once a quarter, restore into a scratch environment for each topology you run. Include one drill with a missing blob to confirm that the API reports it, one upgrade after a restore, and one `serve reindex`. `serve reindex` ends with an integrity check that fails if any published artifact has no matching blob. Document your recovery point and recovery time objectives (RPO and RTO), and who may approve a destructive rollback.

## Related documentation

- [How to deploy a production Marketplace](./deploy-a-production-marketplace.md)
- [Marketplace health and metrics reference](../../reference/marketplace-health-and-metrics.md)
