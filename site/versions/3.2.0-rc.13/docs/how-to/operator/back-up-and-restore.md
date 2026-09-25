# How to back up and restore a Marketplace

Back up a hosted Marketplace and restore it to a consistent point.

## Prerequisites

- Operator access to the database and blob store.
- The ability to stop the API and worker during the backup.

## SQLite and filesystem

1. Stop the API and worker.
2. Copy the SQLite database and the filesystem blob directory at the same point in time.
3. Preserve ownership, permissions, and journal files until the copy is complete.
4. Record the application version and configuration used for the backup.

To restore:

1. Stop the services.
2. Restore the database and blob tree.
3. Verify permissions for the non-root runtime user.
4. Start the API and worker. Startup applies any pending migrations.
5. Run a private readiness check and verify representative artifact reads.

## PostgreSQL and S3

- Back up PostgreSQL with `pg_dump`, `pg_restore`, or managed point-in-time recovery.
- Back up S3 objects with versioning, replication, retention, and object-lock policy.
- Preserve all referenced artifact, manifest, and mirrored-registry objects.
- Back up secret references and configuration separately from secret values.

To restore:

1. Restore PostgreSQL and the S3 object set to a consistent point.
2. Start the service with the restored DSN and bucket.
3. Allow migrations to complete.
4. Run readiness and sampled digest checks.

## Verification

Automate backup age, object retention, and restore drills. Exercise both topologies periodically, including an incomplete-blob failure and a worker reindex.

## Related documentation

- [Marketplace health and metrics reference](../../reference/marketplace-health-and-metrics.md)
