# Marketplace backup and restore

Backups must cover metadata and immutable blobs together. A metadata record can refer to an archive or manifest blob, so restoring only one side can make the service fail readiness or return incomplete history.

## SQLite/filesystem

1. Stop both API and worker processes so no transaction or blob write is in flight.
2. Copy the SQLite database and the filesystem blob directory from the same point in time. Preserve ownership, permissions, and the database journal files until the copy is complete.
3. Prefer a filesystem/snapshot backup of the entire durable volume. If using a logical SQLite copy, use SQLite's backup facility rather than copying a live database file.
4. Record the application version and configuration used for the backup.

To restore, stop services, restore the database and blob tree as one snapshot, verify permissions for the non-root runtime user, and start the API/worker. Startup applies any pending migrations. Run a private readiness check and verify representative artifact and manifest reads before opening the proxy boundary.

Do not restore a SQLite database while the worker is writing it. The migration ledger makes startup re-runnable but is not a backup consistency mechanism.

## PostgreSQL/S3

- Use `pg_dump`/`pg_restore` or managed PostgreSQL point-in-time backups for metadata.
- Use S3 versioning, replication, retention, and object-lock policy according to the required recovery point and recovery time objectives.
- Preserve all referenced artifact, manifest, and mirrored-source objects. Do not expire objects solely from an application metadata listing without an independently verified retention policy.
- Back up the secret references/configuration separately from secret values; restore access through the secret manager.

For restore, restore PostgreSQL and the S3 object set to a consistent recovery point, start the service with the restored DSN/bucket, and allow migrations to complete. Run readiness and sampled digest checks before traffic is enabled. A missing blob is an operational restore failure, not something the API should silently repair.

## Verification and drills

Automate backup age, object retention, database backup success, and restore verification alerts. Exercise both topologies periodically, including an incomplete-blob failure, a migration after restore, and a worker reindex. Document RPO/RTO and who can approve a destructive rollback. The application does not provide point-in-time recovery or cross-store transactionality.
