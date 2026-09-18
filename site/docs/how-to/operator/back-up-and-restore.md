# How to back up and restore a Marketplace

Take a consistent backup of a hosted Marketplace and restore it to that point.

Metadata and blobs must be captured together: a metadata row points at an archive or manifest blob by digest, so restoring one side without the other leaves the API failing readiness or serving incomplete history.

## Prerequisites

- Operator access to the database and blob store.
- The ability to stop the API and worker (SQLite topology) or to take a database snapshot (PostgreSQL topology).
- The application version and the operator configuration in use, recorded alongside every backup.

## SQLite and filesystem

This topology is single-process, so the only consistent backup is one taken while nothing is writing.

In Docker Compose deployments, state is kept in the `marketplace-data` named volume (`/var/lib/agent-marketplace` inside the container), so backup and restore commands run via a temporary helper container mounting that volume. In native host deployments, commands run directly against the host path.

### Docker Compose

#### Back up

```bash
docker compose stop app

mkdir -p ./backups
docker run --rm \
  -v agent-marketplace-sqlite_marketplace-data:/var/lib/agent-marketplace:ro \
  -v "$PWD/backups:/backups" \
  alpine sh -c '
    apk add --no-cache sqlite >/dev/null && \
    sqlite3 /var/lib/agent-marketplace/marketplace.db ".backup '\''/backups/marketplace-$(date +%F).db'\''" && \
    tar -C /var/lib/agent-marketplace -czf "/backups/blobs-$(date +%F).tar.gz" blobs
  '

docker compose run --rm --no-deps --entrypoint /app/agent-marketplace app --version > "./backups/version-$(date +%F).txt"
docker compose start app
```

If your Compose project uses a custom name, adjust the volume name accordingly (check with `docker volume ls -f name=marketplace-data`).

#### Restore

```bash
docker compose stop app

docker run --rm \
  -v agent-marketplace-sqlite_marketplace-data:/var/lib/agent-marketplace \
  -v "$PWD/backups:/backups" \
  alpine sh -c '
    cp /backups/marketplace-<date>.db /var/lib/agent-marketplace/marketplace.db && \
    rm -rf /var/lib/agent-marketplace/blobs && \
    tar -C /var/lib/agent-marketplace -xzf /backups/blobs-<date>.tar.gz && \
    chown -R 1000:1000 /var/lib/agent-marketplace
  '

docker compose start app
```

### Native service (systemd)

#### Back up

```bash
systemctl stop agent-marketplace
sqlite3 /var/lib/agent-marketplace/marketplace.db ".backup '/backups/marketplace-$(date +%F).db'"
tar -C /var/lib/agent-marketplace -czf "/backups/blobs-$(date +%F).tar.gz" blobs
agent-marketplace --version > "/backups/version-$(date +%F).txt"
systemctl start agent-marketplace
```

Use SQLite's `.backup` command rather than `cp` on a live database file; if you snapshot the whole volume instead, snapshot the database and `blobs/` in the same snapshot.

#### Restore

```bash
systemctl stop agent-marketplace
cp /backups/marketplace-<date>.db /var/lib/agent-marketplace/marketplace.db
rm -rf /var/lib/agent-marketplace/blobs
tar -C /var/lib/agent-marketplace -xzf /backups/blobs-<date>.tar.gz
chown -R 1000:1000 /var/lib/agent-marketplace
systemctl start agent-marketplace
```

Startup applies any pending migrations to the restored database, so restore with the same or a newer application version, never an older one.

## PostgreSQL and S3

### Back up

- Database: `pg_dump --format=custom` on a schedule, or the managed provider's point-in-time recovery.
- Blobs: enable S3 versioning and a retention or object-lock policy. Do not expire objects from an application-side listing; the metadata store is the only index of which digests are referenced.
- Record the image tag, operator configuration, and the exact `pg_dump` timestamp together.

### Restore

1. Scale the API and worker to zero.
2. Restore the database (`pg_restore` or PITR) to the chosen point.
3. Confirm every blob referenced at that point still exists in the bucket. Object versioning makes this true if retention outlasts your database backups.
4. Start one API replica, let migrations complete, then scale.
5. Start the worker.

## Verify a restore

Run these before opening the proxy to traffic:

```bash
curl -fsS http://<api-host>:9090/readyz                              # {"status":"ready"}
curl -fsS "$BASE/api/v1/registries" | python3 -m json.tool | head    # registries present
curl -fsS "$BASE/api/v1/resources/<registry>/skill/<name>" -o rev.json
hex=$(python3 -c 'import json;print(json.load(open("rev.json"))["archive_digest"].split(":")[1])')
curl -fsS "$BASE/api/v1/artifacts/sha256/${hex}" -o /dev/null        # blob readable
```

A `404` on the artifact fetch means the blob backup is older than the metadata backup. Restore both from the same point rather than letting the worker reindex over an inconsistent store.

## Drills

Schedule a restore into a scratch environment at least quarterly, for both topologies you run. Include one deliberately missing blob to confirm the API reports it rather than silently repairing, one upgrade-after-restore, and one `serve reindex`. Document RPO/RTO and who may approve a destructive rollback.

## Related documentation

- [How to deploy a production Marketplace](./deploy-a-production-marketplace.md)
- [Marketplace health and metrics reference](../../reference/marketplace-health-and-metrics.md)
