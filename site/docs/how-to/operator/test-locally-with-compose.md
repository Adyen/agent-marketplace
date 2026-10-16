# How to test Marketplace administration locally with Docker Compose

Run the API, worker, SQLite database, frontend, and proxy on your laptop without SSO or local TLS certificates. Nginx supplies a fixed `local-operator` identity on every request, at `http://localhost:8080`.

::: danger Local development only
Everyone who can reach this proxy is an administrator. Bind it only to `127.0.0.1`; never publish it on your LAN, tunnel it, or use this configuration in production. Other local processes can also act as the operator. Use test data and narrowly scoped registry credentials. Keep production HTTPS and SSO enabled for real deployments.
:::

## Prerequisites

- A checkout of this repository and Docker Compose 2.24.4 or later (for the `!override` tag).
- Port 8080 available and the Docker subnet `172.30.89.0/24` unused. If that subnet conflicts, change the subnet, both static addresses below, the upstream in `nginx-operator.local.conf`, and the trusted-proxy CIDR together.
- A shell in the repository root for every command.

## 1. Prepare the operator configuration

For a new environment, copy `deploy/compose/sqlite-filesystem/config.yaml` to `deploy/compose/sqlite-filesystem/config-mounted.local.yaml`. For an existing environment, edit the configuration you already mount instead, and keep its storage paths, instance ID, policies, and credential mounts.

Set these fields. Keep your existing instance ID when you reuse a database:

```yaml
instance:
  id: example-marketplace
  name: Local Marketplace
  canonical_url: http://localhost:8080
  browser_base: http://localhost:8080
management:
  mode: database
  allowed_origins:
    - https://github.com
  profiles:
    github:
      allowed_origins: [https://github.com]
      ca_file: /etc/ssl/certs/ca-certificates.crt
admin:
  enabled: true
  allow_insecure_localhost: true
  origin: http://localhost:8080
  trusted_proxies: [172.30.89.2/32]
  issuer: local-development-proxy
  subject_header: X-Marketplace-Subject
  groups_header: X-Marketplace-Groups
  admin_groups: [marketplace-admins]
```

In trusted-proxy mode, `issuer` is a label recorded with each audited change. It is not an SSO discovery endpoint, so you do not register anything with an identity provider or create a client secret. Keep the base configuration's storage, listener, policy, and refresh settings. Use `registries: []` only for a fresh catalog: adoption imports the registries listed in the YAML file and refuses to run if the list leaves out a registry the database already stores.

`allow_insecure_localhost` defaults to `false`. When you enable it, the admin origin may use HTTP, but only for `localhost` or a literal loopback IP address. LAN addresses, other DNS names, and wildcard hosts still require HTTPS. The option does not change listener bindings, and it does not bypass authentication, proxy trust, or the exact-origin check.

## 2. Configure the local proxy and Compose override

Use the repository's `deploy/compose/sqlite-filesystem/nginx-operator.local.conf`. It listens on plain HTTP, overwrites any identity headers a caller sends, and sends API traffic to the app's address on a dedicated network. The API trusts only the proxy's exact address on that network.

Create `compose.override.yaml` with the following content, or merge it into your existing override. If you use private Git registries, keep your existing private CA mounts and `env_file` entries under `app`:

```yaml
services:
  app:
    volumes:
      - ./deploy/compose/sqlite-filesystem/config-mounted.local.yaml:/app/marketplace.yaml:ro
    networks:
      default: {}
      operator:
        ipv4_address: 172.30.89.3
  proxy:
    ports: !override
      - "127.0.0.1:8080:8080"
    volumes:
      - ./deploy/compose/sqlite-filesystem/nginx-operator.local.conf:/etc/nginx/conf.d/default.conf:ro
    networks:
      default: {}
      operator:
        ipv4_address: 172.30.89.2
networks:
  operator:
    ipam:
      config:
        - subnet: 172.30.89.0/24
```

The `!override` tag replaces the base file's port publication on all interfaces instead of adding a second mapping. Do not publish the app's port directly, and do not add this fixed-identity proxy to a shared production proxy configuration.

Validate the configuration and build the images:

```bash
docker compose config --quiet
docker compose build
```

Both commands exit successfully. Git ignores `compose.override.yaml` and the `*.local.yaml` operator configuration. If your override is a symlink shared across worktrees, changes affect every worktree that uses it. Compose resolves relative mount paths from the project directory.

### Replace an earlier local HTTPS setup

If you previously ran this environment behind a local HTTPS proxy, set all three local URLs (`canonical_url`, `browser_base`, and `admin.origin`) to HTTP and enable `allow_insecure_localhost`. Use the plain-HTTP proxy configuration above and remove the `operator-cert.local.pem` and `operator-key.local.pem` mounts from your override. The certificate files can stay on disk unused. Private Git CA mounts are unrelated; keep them.

Rebuild the app image and recreate the `app` and `proxy` containers after these changes. You do not need to reset the database or adopt again. Open the HTTP URL explicitly, update old HTTPS bookmarks, and turn off your browser's HTTPS-only upgrade for this site if it redirects you.

## 3. Adopt a fresh database once

Skip this step if the database has already been adopted. Restarting, rebuilding, or switching the local listener from HTTPS to HTTP does not require another adoption.

Stop the app. In the SQLite deployment, one `app` container runs both the API and the worker:

```bash
docker compose stop app
```

Before you adopt an existing catalog, take coordinated [database and blob backups](./back-up-and-restore.md), stop any other process that uses the stores, and invalidate shared caches if you have any. Deleting the volume does not repair an authority-mode problem; it destroys the data.

Then run:

```bash
docker compose run --rm --no-deps app serve adopt \
  --config /app/marketplace.yaml \
  --actor local-operator \
  --reason "Initial local operator environment" \
  --idempotency-key local-operator-adoption-1 \
  --confirm-maintenance
```

The output includes `Adopted marketplace configuration at revision 1.` If you need to retry, reuse the same idempotency key with the same configuration and reason. After adoption, edit registries in the admin panel; the YAML `registries` list does not control them.

## 4. Start and verify administration

```bash
docker compose up -d --force-recreate app proxy
docker compose ps
docker compose logs --tail=50 app proxy
curl -fsS http://localhost:8080/api/v1/admin/session
```

`app`, `web`, `docs`, and `proxy` are running, and `app`, `web`, and `docs` report healthy (the proxy has no health check). The proxy's published address is `127.0.0.1:8080`, not `0.0.0.0:8080`. Typical session response:

```json
{"actor":{"issuer":"local-development-proxy","subject":"local-operator"},"authentication_mode":"trusted_proxy","capabilities":["configuration","lifecycle","activity"]}
```

Open `http://localhost:8080/admin`. There is no SSO sign-in and no certificate prompt. Use exactly `http://localhost:8080`: `http://127.0.0.1:8080` is a different origin, and the API rejects admin changes from it unless you also change `admin.origin`.

## 5. Exercise registry discovery and lifecycle

1. Open **Registries**, click **Add registry**, and enter a Git origin whose protocol and host are listed in `management.allowed_origins`.
2. Enter a ref and a source root relative to the repository. Use a small repository you control, with a root that contains a supported resource layout. For GitHub, select the `github` profile from step 1: it points Git at the image's CA bundle without adding credentials. Acquisition removes inherited Git environment settings, so do not rely on `GIT_SSL_CAINFO` alone when your Git build needs a non-default CA path.
3. Click **Preview discovery** and wait for the worker's result. Inspect the resource counts, source paths, diagnostics, and, for an existing registry, the differences from the current publication.
4. Change the root and check that the panel marks the preview as out of date. Preview again before you apply.
5. Enter a reason, click **Continue to review**, and apply the change. Then watch the job status. An accepted configuration change does not mean indexing succeeded; the registry's **Indexing history** tab shows the outcome.
6. On your test registry, try the actions in the **Actions** panel of the **Configuration** tab: **Pause refresh**, **Withdraw availability**, and **Restore availability**. Pausing stops refresh. Withdrawing blocks downloads and installation. Restoring availability leaves the registry paused; use **Resume refresh** to restart indexing.

A preview runs discovery without publishing anything, and you can apply a change even when its preview shows warnings. Git acquisition still needs network access and any required credentials. `localhost` in a Git URL refers to the app container, not your laptop. The HTTP admin option does not turn off TLS verification for Git origins.

### Use a private registry

Add the registry's exact protocol and host to `management.allowed_origins`, then add a profile bound to that origin next to your existing profiles:

```yaml
management:
  mode: database
  allowed_origins:
    - https://github.com
    - https://git.example.com
  profiles:
    github:
      allowed_origins: [https://github.com]
      ca_file: /etc/ssl/certs/ca-certificates.crt
    private-git:
      allowed_origins: [https://git.example.com]
      credentials:
        token:
          env: LOCAL_GIT_TOKEN
      ca_file: /etc/agent-marketplace/ca/private.pem
```

Pass `LOCAL_GIT_TOKEN` to the app through a Compose `env_file` that Git ignores, and use a read-only repository token. Mount the private CA read-only at the path in `ca_file`, or leave out `ca_file` when the standard trust store is enough. Restart the app after you change profiles, then select `private-git` in the registry editor. Never type a token or a server file path into a registry field in the browser. In database mode, per-registry YAML credentials do not replace deployment profiles.

## Troubleshoot startup and access

| Symptom | Cause and fix |
|---|---|
| Nginx reports that a plain HTTP request was sent to an HTTPS port | The old TLS proxy is still running, or the wrong proxy configuration is mounted. Mount the plain-HTTP configuration, rebuild the app, and recreate `app` and `proxy`. Do not rewrite the browser's `Origin` header to work around it. |
| `configuration authority mode conflict` | The app opened an adopted database in file mode. Set `management.mode: database` in the file that is actually mounted at `/app/marketplace.yaml`. Keep the volume. |
| `managed configuration adoption required` | The current database has no adopted configuration for the current instance ID. Check the instance ID and the volume first, and adopt only if this is the unadopted database you intend to use. |
| Admin API returns `503` with `admin_unavailable` | The `admin` block is incomplete or invalid, for example an HTTP origin without `allow_insecure_localhost`, a missing header or issuer, or no admin group or user. Fill in the complete `admin` block from step 1 and recreate the app. Database management and admin authentication are separate settings. |
| `403` with the message "Administration requires a trusted identity proxy." | The request did not come from an address in `admin.trusted_proxies`. Check that the Nginx upstream targets `172.30.89.3` and that the proxy's operator-network address matches the `/32` entry. |
| `403` with `origin_rejected` | The browser origin differs from `admin.origin`. Use the exact configured HTTP origin, including the port. Keep origin validation on. |
| The preview fails with `profile_unavailable` | The Git origin is not allowed, or the selected profile does not cover it. Allow the origin and select a matching deployment profile. Keep credential environment variables and CA mounts when you edit the override. |
| Changes to a mounted configuration seem ignored | Inspect `docker compose config` locally. Its output can contain secrets from environment files, so do not share it. Recreate the containers after you change bind-mounted files or symlink targets: `docker compose up -d --force-recreate app proxy`. |

## Stop without losing data

```bash
docker compose stop
```

The named volume keeps the database, managed configuration, history, and blobs. Run `docker compose down -v` only when you want to destroy that data. For a deployment with real users, follow [the production admin-panel guide](./enable-and-use-the-admin-panel.md) instead of this fixed-identity setup.
