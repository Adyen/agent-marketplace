# Marketplace backup and restore

Back up metadata and immutable blobs together. A metadata record can refer to an archive or manifest blob, so restoring only one side can make the service fail readiness or return incomplete history.

## SQLite/filesystem

1. Stop both the API and worker processes so no transaction or blob write is in flight. In the all-in-one Compose example, both run in the `app` service.
2. Copy the SQLite database and the filesystem blob directory from the same point in time. Preserve ownership and permissions, and keep the database's WAL and shared-memory files with it until the copy is complete.
3. Prefer a filesystem or volume snapshot of the entire durable volume. For a logical SQLite copy, use SQLite's backup facility instead of copying a live database file.
4. Record the application version and the configuration used for the backup.

To restore, stop the services, restore the database and blob tree as one snapshot, check the permissions for the non-root runtime user (UID 1000 in the bundled images), and start the API and worker. Startup applies any pending migrations. Run a private readiness check and verify a few representative artifact and manifest reads before you open the proxy boundary. The `/readyz` check covers configuration authority, reconciled revocations, the instance record, and a blob-store probe; it does not read every blob.

Do not restore a SQLite database while the worker is writing to it. The migration ledger makes startup safe to repeat, but it does nothing for backup consistency.

## PostgreSQL/S3

- Use `pg_dump`/`pg_restore` or managed PostgreSQL point-in-time backups for metadata.
- Set S3 versioning, replication, retention, and object-lock policy to match the required recovery point and recovery time objectives.
- Preserve every referenced artifact, manifest, and mirrored-registry object. Do not expire objects based only on an application metadata listing unless an independently verified retention policy allows it.
- Back up the secret references and configuration separately from the secret values, and restore access through the secret manager.

To restore, bring PostgreSQL and the S3 object set back to a consistent recovery point, start the service with the restored DSN and bucket, and let migrations complete. Run readiness and sampled digest checks before you enable traffic. A missing blob is an operational restore failure; the API does not repair it.

## Verification and drills

Automate alerts for backup age, object retention, database backup success, and restore verification. Exercise both topologies periodically, including an incomplete-blob failure, a migration after restore, and a worker reindex (`serve reindex` ends with an integrity check of metadata against blobs). Document the RPO and RTO, and who can approve a destructive rollback. The application provides no point-in-time recovery and no transactions that span the metadata and blob stores.
