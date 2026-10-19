# PostgreSQL/S3 example

This example runs PostgreSQL, separate API and worker containers, the Nuxt Marketplace UI, and an internal Nginx reverse proxy. Local documentation is optional and disabled by default; UI documentation links use `https://agent-marketplace.io/docs`. Only the proxy publishes a port. It serves Nuxt at `/` and routes `/.well-known/agent-marketplace.json`, `/api/`, and `/healthz` to the API. Without local docs, `/docs` and `/docs/` descendants return 404. PostgreSQL, the API, and the worker are internal services. The operations endpoints are not routed or published, and public requests to `/readyz` and `/metrics` get an explicit 404. The API container's healthcheck runs `/app/marketplace-healthcheck`, which calls `/readyz` on `127.0.0.1:9090` inside the container.

The API and worker are built from different targets of `Dockerfile.marketplace`. Only the `worker` image contains Git, because acquisition runs there.

When upgrading from the combined site profile, the proxy temporarily redirects `/marketplace` and its descendants to the corresponding root routes while preserving query parameters. Set `instance.browser_base` to the bare external origin, move any gateway protection from `/marketplace/admin/**` to `/admin/**`, and start Compose with `--remove-orphans` so the old `site` container is removed. OIDC transactions started before the route switch may require a fresh sign-in.

The default `compose.yaml` is safe for production as written. It reads the PostgreSQL password from a secret file, mounts the metadata DSN as a Compose secret at `/run/secrets/marketplace-postgres-dsn`, and declares no AWS credential environment variables. S3 credentials therefore come from the AWS SDK default credential chain; prefer workload identity or IAM roles. If roles are unavailable, use the platform's secret manager and an SDK-supported credentials file instead of adding static AWS secrets to Compose.

The mounted `config.dsn-file.example.yaml` is the source of truth for the S3 bucket, region, endpoint, canonical URL, browser base, and API configuration. Copy it to a deployment-specific config file or edit the checked-in example before starting. Do not put passwords in committed YAML, Compose files, image layers, command arguments, or logs.

## Production usage

1. From this directory (`deploy/compose/postgres-s3`), copy both secret examples to ignored files and replace every placeholder. The password inside the DSN must match the PostgreSQL password:

   ```bash
   cp secrets/marketplace-postgres-password.example secrets/marketplace-postgres-password
   cp secrets/marketplace-postgres-dsn.example secrets/marketplace-postgres-dsn
   $EDITOR secrets/marketplace-postgres-password secrets/marketplace-postgres-dsn
   sudo chown 1000:1000 secrets/marketplace-postgres-dsn
   sudo chmod 0400 secrets/marketplace-postgres-password secrets/marketplace-postgres-dsn
   ```

   Compose mounts these files with their host owner and mode. The API and worker run as UID/GID `1000:1000` and read the DSN file directly, so change its owner before you restrict it to `0400`. Otherwise the containers cannot read it on Linux hosts.

2. Replace the non-secret placeholders in `config.dsn-file.example.yaml` (S3 bucket, region, endpoint, canonical URL, and browser base), or mount your own equivalent Marketplace operator configuration at `/app/marketplace.yaml`.
3. Provide S3 access through workload identity or IAM, or through the platform's AWS credentials-file integration. Do not add `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`, or other static credential declarations to this Compose path.
4. Require TLS and authentication at the reverse proxy or a private network boundary, and restrict access to the proxy port. The public application routes have no built-in authentication.
5. From the repository root, validate the Compose files and start this example:

   ```bash
   make compose-config
   docker compose -f deploy/compose/postgres-s3/compose.yaml up --build
   ```

   `make compose-config` checks every Compose file in the repository using temporary placeholder secrets, so it does not validate your secret values. It prints `SKIP` and exits successfully when Docker or Docker Compose is unavailable. Plain `docker compose up` in the repository root starts the SQLite example instead of this one.

The application containers run as non-root users and are read-only apart from `/tmp`. Compose mounts secrets as files: PostgreSQL reads its password through `POSTGRES_PASSWORD_FILE`, and the application reads its DSN through `storage.metadata.dsn_file`. S3 durability, versioning, retention, and encryption are the operator's responsibility.

For a registry with `ca_file: /etc/agent-marketplace/ca/internal.pem`, mount the same readable PEM bundle at that absolute path in both `api` and `worker`. Both roles validate the complete operator config at startup, although only the worker uses the bundle for Git acquisition. Keep the mount read-only and readable by container UID 1000.

## Bundled documentation

For air-gapped environments, append the docs override. From the repository root:

```bash
docker compose -f deploy/compose/postgres-s3/compose.yaml \
  -f deploy/compose/postgres-s3/compose.docs.yaml up --build
```

This adds the `docs` service at origin-root `/docs/`, enables its proxy
route and healthy-service dependency, and sets `NUXT_PUBLIC_DOCS_ENABLED=true`
for the web container. Use the same files for subsequent Compose commands.
The override also composes with `compose.development.yaml`.

Build or transfer the images before disconnecting the network. This
docs-only switch does not disable other outbound connections. To disable
local docs again, use the base configuration and stop the unused docs container.

## Local development usage

The environment-based DSN and static AWS credentials exist only in the explicit development override. From this directory, copy `.env.example` to an untracked `.env`, replace every placeholder, and run:

```bash
cp .env.example .env
$EDITOR .env
# .env must remain untracked
docker compose --env-file .env -f compose.yaml -f compose.development.yaml up --build
```

If your image mirror provides only `linux/amd64` images, add `MARKETPLACE_PLATFORM=linux/amd64` to `.env` or export it before starting. Apple Silicon then uses emulation. Use `MARKETPLACE_PLATFORM=linux/arm64` when ARM64 images are available.
`NGINX_IMAGE` overrides both Nginx services for mirrors that expose one image;
set `SITE_NGINX_IMAGE` separately to keep an unprivileged documentation runtime.

The development override switches both application services to `config.yaml`, which uses `storage.metadata.dsn_env`, and supplies `MARKETPLACE_POSTGRES_DSN` from `.env`. It also supplies AWS credentials from `.env` for local testing only. Never use `compose.development.yaml` or static AWS environment credentials for production.

`compose.dsn-file.yaml` is for deployments that start from an older base file. It clears `MARKETPLACE_POSTGRES_DSN` and mounts the DSN secret at the exact `storage.metadata.dsn_file` path, owned by UID/GID 1000 with mode `0400`. Configuration validation makes the file and environment DSN modes mutually exclusive. Local Docker Compose bind mounts may need the secret files to be readable by the application's non-root UID/GID `1000:1000`; never make them world-readable.
