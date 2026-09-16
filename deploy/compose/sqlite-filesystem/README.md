# SQLite/filesystem example

This is an explicitly all-in-one, single-process deployment: the `app` service runs both the API and refresh worker with `serve --role all`, `site` serves the static VitePress landing page, `web` serves the Nuxt Marketplace UI, and `proxy` is the only published service. Do not split the API and worker while using SQLite: each process would have its own database connection and concurrent refresh writes are not the supported production topology. The worker is deliberately configured for low concurrency (`refresh_concurrency: 1`); use the PostgreSQL example for independently scaled API and worker services.

The proxy routes API, well-known metadata, and `healthz` to `app`; `/marketplace/` goes to Nuxt; and `/` goes to VitePress. Operations endpoints (`/readyz` and `/metrics`) stay on the app's private listener and public requests receive an explicit 404. The app, site, web, and proxy use `restart: unless-stopped`; inspect the app logs and private readiness endpoint after a restart before treating the deployment as healthy.

Before starting, review `config.yaml` and replace the `.example.invalid` canonical URL with the deployment's controlled value. Keep `instance.browser_base` on that same origin with the `/marketplace` suffix. The example uses a named volume for both SQLite metadata and filesystem blobs; this is the required writable exception to the read-only app container. Worker git clone scratch space is `/tmp` (a tmpfs). Stop the app before copying that volume for backup; see `docs/marketplace-backup-restore.md`.

For a registry with `ca_file: /etc/agent-marketplace/ca/internal.pem`, add a matching read-only bind mount to the `app` service, such as `/host/path/internal.pem:/etc/agent-marketplace/ca/internal.pem:ro`. The file must be readable by container UID 1000.

The Compose entrypoint is at the repository root. From there, Compose leaves the platform unset by default so Docker can select the host architecture:

```bash
docker compose up --build
```

Open `http://localhost:8080/` for the landing site and
`http://localhost:8080/marketplace/` for the Marketplace UI. If
`MARKETPLACE_HTTP_PORT` is set, use that port instead.

If your registry mirror provides only `linux/amd64` images, set the platform explicitly (Apple Silicon will use emulation):

```bash
MARKETPLACE_PLATFORM=linux/amd64 docker compose up --build
```

If ARM64 images are available, use `MARKETPLACE_PLATFORM=linux/arm64` instead.
`NGINX_IMAGE` overrides both Nginx services for mirrors that expose one image;
set `SITE_NGINX_IMAGE` separately to retain an unprivileged site runtime.

This deployment has no application authentication. Put the proxy behind an authenticated, TLS-terminating network boundary before exposing it to users. Mount only the deployment-owned Marketplace operator configuration at `/app/marketplace.yaml`; do not substitute consumer-provided or writable configuration.
