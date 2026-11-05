# SQLite/filesystem example

This example is a single-process, all-in-one deployment. The `app` service runs both the API and the refresh worker with `serve --role all`, `web` serves the Nuxt Marketplace UI, and `proxy` is the only service that publishes a port. Local documentation is optional and disabled by default. Do not split the API and worker while using SQLite: each process would open its own database connection, and concurrent refresh writes are not a supported production topology. The worker runs with low concurrency (`refresh_concurrency: 1`). Use the PostgreSQL example when you need to scale the API and worker independently.

The proxy routes the API, the well-known metadata document, and `/healthz` to `app`, and the remaining UI traffic to Nuxt. Without local docs, `/docs` and `/docs/` descendants return 404 and the UI links to `https://agent-marketplace.io/docs`. The operations endpoints (`/readyz` and `/metrics`) stay on the app's private listener, and public requests to them receive an explicit 404. The `app` container's healthcheck runs `/app/marketplace-healthcheck`, which calls `/readyz` on `127.0.0.1:9090` inside the container. The services use `restart: unless-stopped`. After a restart, check the app logs and the private readiness endpoint before you treat the deployment as healthy.

When upgrading from the combined site profile, the proxy temporarily redirects `/marketplace` and its descendants to the corresponding root routes while preserving query parameters. Keep `instance.browser_base` on the bare external origin, move any gateway protection from `/marketplace/admin/**` to `/admin/**`, and run `docker compose up --build --remove-orphans` so the old `site` container is removed. OIDC transactions started before the route switch may require a fresh sign-in.

Before starting, review `config.yaml` and replace the `.example.invalid` canonical URL with the deployment's own value. Keep `instance.browser_base` on that same origin without a path suffix. The example keeps both SQLite metadata and filesystem blobs on one named volume (`marketplace-data`, mounted at `/var/lib/agent-marketplace`); that volume is the only persistent writable path in the otherwise read-only app container. The app can also write to `/tmp`, a tmpfs that holds the worker's Git clone scratch space and is emptied when the container stops. Stop the app before copying the volume for a backup; see [the backup guide](../../../docs/marketplace-backup-restore.md).

For a registry with `ca_file: /etc/agent-marketplace/ca/internal.pem`, add a matching read-only bind mount to the `app` service, such as `/host/path/internal.pem:/etc/agent-marketplace/ca/internal.pem:ro`. The file must be readable by container UID 1000.

The Compose entry point for this example is `compose.yaml` at the repository root. Run it from there. Compose leaves the platform unset by default, so Docker selects the host architecture:

```bash
docker compose up --build
```

Open `http://localhost:8080/` for the Marketplace UI. If
`MARKETPLACE_HTTP_PORT` is set, use that port instead.

For air-gapped environments, opt in to bundled documentation:

```bash
docker compose -f compose.yaml -f compose.docs.yaml up --build
```

This adds the `docs` service, routes `http://localhost:8080/docs/` to it,
and sets `NUXT_PUBLIC_DOCS_ENABLED=true` for every UI documentation link.
Use the same `-f` arguments for subsequent Compose commands. If you also
use a local `compose.override.yaml`, list it explicitly after these files;
Compose does not load it automatically when `-f` is supplied.

Build or transfer all required images before disconnecting the network.
This docs-only switch does not disable registry acquisition, external
identity providers, or other outbound connections. To disable local docs
again, use the base configuration and stop the now-unused docs container.

If your image mirror provides only `linux/amd64` images, set the platform explicitly (Apple Silicon then uses emulation):

```bash
MARKETPLACE_PLATFORM=linux/amd64 docker compose up --build
```

If ARM64 images are available, use `MARKETPLACE_PLATFORM=linux/arm64` instead.
`NGINX_IMAGE` overrides both Nginx services for mirrors that expose one image;
set `SITE_NGINX_IMAGE` separately to keep an unprivileged documentation runtime.

`nginx-operator.local.conf` is an alternative proxy configuration for testing the admin panel locally in trusted-proxy mode. It overwrites caller-supplied identity headers with a fixed operator identity and needs a Compose override that places the app and proxy on a dedicated network. Use it only on a local machine; [Test locally with Compose](../../../site/docs/how-to/operator/test-locally-with-compose.md) describes the setup.

This deployment has no application authentication for public routes. Put the proxy behind an authenticated, TLS-terminating network boundary before exposing it to users. Mount only the deployment-owned Marketplace operator configuration at `/app/marketplace.yaml`; do not substitute configuration that consumers provide or can write.
