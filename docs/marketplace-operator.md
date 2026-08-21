# Marketplace operator guide

## Deployment shape

Run the static Go binary as two roles when possible:

- `serve --role api` serves read-only catalog, artifact, browser, well-known, and public health routes.
- `serve --role worker` performs repository acquisition, analysis, and indexing. The worker image includes `git`, CA certificates, and `/bin/sh` because acquisition runs there.
- `serve --role all` is suitable for a single-process development deployment, not the preferred production topology.

The API and worker can start concurrently. SQLite and PostgreSQL migration runners serialize schema changes and record applied versions. PostgreSQL uses a transaction-scoped advisory lock; SQLite uses an immediate transaction, WAL, busy timeout, and retry handling. A failed migration rolls back its transaction and should block startup rather than run with a partial schema.

## Network boundary

The application has no built-in user authentication or authorization. Treat every public API and browser route as unauthenticated. Put it behind a TLS-terminating, authenticated network boundary, or keep it on a private network. Restrict the proxy port with firewall/security-group policy.

`operations_listen` serves `/readyz` and `/metrics`. It defaults to loopback and must not be published through a container port or reverse proxy; the public listener returns 404 for both paths. If operators need metrics, collect them from a private sidecar/network path with its own access controls. `/healthz` is a liveness endpoint and does not prove storage readiness. Container API healthchecks use the private `/readyz` listener.

## Configuration and secrets

Use `storage.metadata.dsn_file` in production, preferably through a Compose/platform secret integration. Exactly one DSN source must be configured and the selected value must be non-empty. Literal `dsn` and the environment-based `dsn_env` Compose fixture are for local development only. DSN values are not included in configuration errors or application startup logs.

Use file permissions and a secret manager for DSN files. For S3, prefer workload identity/IAM roles through the AWS SDK's default credential chain; if roles are unavailable, use an SDK-supported credentials file from the platform secret manager rather than static environment credentials. Do not put passwords in committed YAML, Compose files, image layers, command arguments, or issue output. Source origins must not contain URL userinfo or password/token query parameters; put source authentication in a credentials file or environment reference instead. Source credential settings follow the same file/environment-reference rule. Production SQLite configuration must name a writable filesystem database; `:memory:`, `mode=memory`, `mode=ro`, and `immutable=1` URI modes are rejected (the SQLite package's direct in-memory constructor remains available to tests).

## Storage and operations

Use durable storage for SQLite plus filesystem blobs, PostgreSQL plus S3 blobs, or an equivalent managed service. Configure S3 bucket versioning, encryption, retention, and restricted IAM separately. Monitor `/healthz`, private `/readyz`, worker logs, migration failures, queue age, and blob availability.

Resource ingestion clones configured repositories and therefore requires controlled outbound network access. Limit repository size, file count, file size, artifact size, and operation durations in policy. The service is not a sandbox for arbitrary untrusted network destinations; use egress controls and review source configuration.

See [security](marketplace-security.md), [backup and restore](marketplace-backup-restore.md), and [reverse proxy](marketplace-reverse-proxy.md).
