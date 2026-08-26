# PostgreSQL/S3 example

This example runs PostgreSQL, separate API and worker images, the Nuxt web UI, and an internal Nginx reverse proxy. Only the proxy publishes a port. PostgreSQL, the API, and the worker are internal services; operations endpoints are intentionally not routed or published. Public `/readyz` and `/metrics` are explicit 404s.

The default `compose.yaml` is production-safe by construction. It uses a PostgreSQL password secret file, mounts the metadata DSN as a Compose secret at `/run/secrets/marketplace-postgres-dsn`, and declares no AWS credential environment variables. S3 credentials therefore come from the AWS SDK default credential chain (prefer workload identity/IAM roles). If roles are unavailable, use the platform's secret manager and an SDK-supported credentials file rather than adding static AWS secrets to Compose.

The mounted `config.dsn-file.example.yaml` is authoritative for the S3 bucket, region, endpoint, canonical URL, and API configuration. Copy it to a deployment-specific config file or edit the checked-in example before starting. Do not put passwords in committed YAML, Compose files, image layers, command arguments, or logs.

## Production usage

1. Copy both secret examples to ignored files and replace every placeholder. Keep the DSN password equal to the PostgreSQL password:

   ```bash
   cp secrets/marketplace-postgres-password.example secrets/marketplace-postgres-password
   cp secrets/marketplace-postgres-dsn.example secrets/marketplace-postgres-dsn
   $EDITOR secrets/marketplace-postgres-password secrets/marketplace-postgres-dsn
   chmod 0400 secrets/marketplace-postgres-password secrets/marketplace-postgres-dsn
   ```

2. Replace the non-secret placeholders in `config.dsn-file.example.yaml` (S3 bucket, region, endpoint, and canonical URL), or mount your own equivalent file at `/etc/agent-marketplace/config.yaml`.
3. Provide S3 access through workload identity/IAM or the platform's AWS credentials-file integration. Do not add `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`, or other static credential declarations to this Compose path.
4. Require TLS and authentication at the reverse proxy or a private network boundary, and restrict access to the proxy port. The application has no built-in authentication.
5. Validate the merged file and start it:

   ```bash
   make compose-config
   docker compose up --build
   ```

The application containers remain non-root and read-only apart from `/tmp`. Compose mounts secrets as files; the PostgreSQL password is consumed through `POSTGRES_PASSWORD_FILE`, and the application reads its DSN through `storage.metadata.dsn_file`. S3 durability, versioning, retention, and encryption remain operator responsibilities.

For a source with `ca_file: /etc/agent-marketplace/ca/internal.pem`, mount the same readable PEM bundle at that absolute path in both `api` and `worker`; both roles validate the complete operator config during startup, although only the worker uses it for Git acquisition. Keep the mount read-only and readable by container UID 1000.

## Local development usage

The environment-based DSN and static AWS credential path is retained only in the explicit development override. Copy `.env.example` to an untracked `.env`, replace all placeholders, and run:

```bash
cp .env.example .env
$EDITOR .env
# .env must remain untracked
docker compose --env-file .env -f compose.yaml -f compose.development.yaml up --build
```

If your registry mirror provides only `linux/amd64` images, add `MARKETPLACE_PLATFORM=linux/amd64` to `.env` or export it before starting. Apple Silicon will then use emulation. Use `MARKETPLACE_PLATFORM=linux/arm64` when ARM64 images are available.

The development override switches both application services to `config.yaml`, which uses `storage.metadata.dsn_env`, and supplies `MARKETPLACE_POSTGRES_DSN` from `.env`. It also supplies AWS credentials from `.env` for local testing only. Never use `compose.development.yaml` or static AWS environment credentials for production.

The separate `compose.dsn-file.yaml` remains available for deployments that start from an older base file; it clears `MARKETPLACE_POSTGRES_DSN` and mounts the DSN secret at the exact `storage.metadata.dsn_file` path. The file and environment DSN modes are mutually exclusive by configuration validation. Local Docker Compose bind mounts may require the secret files to be readable by the application's non-root UID/GID `1000:1000`; never make them world-readable.
