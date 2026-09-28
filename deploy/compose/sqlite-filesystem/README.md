# SQLite/filesystem example

This example is a single-process, all-in-one deployment. The `app` service runs both the API and the refresh worker with `serve --role all`, `site` serves the static VitePress landing page, `web` serves the Nuxt Marketplace UI, and `proxy` is the only service that publishes a port. Do not split the API and worker while using SQLite: each process would open its own database connection, and concurrent refresh writes are not a supported production topology. The worker runs with low concurrency (`refresh_concurrency: 1`). Use the PostgreSQL example when you need to scale the API and worker independently.

The proxy routes the API, the well-known metadata document, and `/healthz` to `app`, `/marketplace/` (and `/_fonts/`) to Nuxt, and `/` to VitePress. The operations endpoints (`/readyz` and `/metrics`) stay on the app's private listener, and public requests to them receive an explicit 404. The `app` container's healthcheck runs `/app/marketplace-healthcheck`, which calls `/readyz` on `127.0.0.1:9090` inside the container. The app, site, web, and proxy services use `restart: unless-stopped`. After a restart, check the app logs and the private readiness endpoint before you treat the deployment as healthy.

Before starting, review `config.yaml` and replace the `.example.invalid` canonical URL with the deployment's own value. Keep `instance.browser_base` on that same origin with the `/marketplace` suffix. The example keeps both SQLite metadata and filesystem blobs on one named volume (`marketplace-data`, mounted at `/var/lib/agent-marketplace`); that volume is the only persistent writable path in the otherwise read-only app container. The app can also write to `/tmp`, a tmpfs that holds the worker's Git clone scratch space and is emptied when the container stops. Stop the app before copying the volume for a backup; see [the backup guide](../../../docs/marketplace-backup-restore.md).

For a registry with `ca_file: /etc/agent-marketplace/ca/internal.pem`, add a matching read-only bind mount to the `app` service, such as `/host/path/internal.pem:/etc/agent-marketplace/ca/internal.pem:ro`. The file must be readable by container UID 1000.

The Compose entry point for this example is `compose.yaml` at the repository root. Run it from there. Compose leaves the platform unset by default, so Docker selects the host architecture:

```bash
docker compose up --build
```

Open `http://localhost:8080/` for the landing site and
`http://localhost:8080/marketplace/` for the Marketplace UI. If
`MARKETPLACE_HTTP_PORT` is set, use that port instead.

If your image mirror provides only `linux/amd64` images, set the platform explicitly (Apple Silicon then uses emulation):

```bash
MARKETPLACE_PLATFORM=linux/amd64 docker compose up --build
```

If ARM64 images are available, use `MARKETPLACE_PLATFORM=linux/arm64` instead.
`NGINX_IMAGE` overrides both Nginx services for mirrors that expose one image;
set `SITE_NGINX_IMAGE` separately to keep an unprivileged site runtime.

`nginx-operator.local.conf` is an alternative proxy configuration for testing the admin panel locally in trusted-proxy mode. It overwrites caller-supplied identity headers with a fixed operator identity and needs a Compose override that places the app and proxy on a dedicated network. Use it only on a local machine; [Test locally with Compose](../../../site/docs/how-to/operator/test-locally-with-compose.md) describes the setup.

This deployment has no application authentication for public routes. Put the proxy behind an authenticated, TLS-terminating network boundary before exposing it to users. Mount only the deployment-owned Marketplace operator configuration at `/app/marketplace.yaml`; do not substitute configuration that consumers provide or can write.
