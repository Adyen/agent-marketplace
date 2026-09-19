# How to test Marketplace administration locally with Docker Compose

Run the real API, worker, SQLite database, frontend, and proxy on your laptop without SSO or local TLS certificates. This guide uses `http://localhost:8080` and a fixed `local-operator` identity supplied by Nginx.

::: danger Local development only
Everyone who can reach this proxy is an administrator. Bind it only to `127.0.0.1`; never publish it on your LAN, tunnel it, or use this configuration in production. Other local processes can also act as the operator. Use test data and narrowly scoped registry credentials. Keep production HTTPS and SSO enabled for real deployments.
:::

## Prerequisites

- A checkout containing the `admin.allow_insecure_localhost` option and Docker Compose 2.24.4 or later (`!override` support).
- Port 8080 available and Docker subnet `172.30.89.0/24` unused. If that subnet conflicts, change the subnet and both static addresses below, the Nginx upstream, and the trusted-proxy CIDR together.
- Run commands from the repository root.

## 1. Prepare the operator configuration

For a new environment, copy `deploy/compose/sqlite-filesystem/config.yaml` to `deploy/compose/sqlite-filesystem/config-mounted.local.yaml`. For an existing environment, edit your already-mounted config instead. Do not replace existing storage paths, instance IDs, policies, or credential mounts.

Set these fields, retaining your existing instance ID when reusing a database:

```yaml
instance:
  id: example-marketplace
  name: Local Marketplace
  canonical_url: http://localhost:8080
  browser_base: http://localhost:8080/marketplace
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

The issuer is an audit identity label, not an SSO discovery endpoint. No identity-provider registration or client secret is needed. Retain the base config's storage, listeners, policy, and refresh settings. Start with `registries: []` only for a fresh catalog; adoption imports the registries listed in YAML and refuses to omit stored registry identities.

`allow_insecure_localhost` defaults to false. When explicitly enabled, it permits HTTP only for `localhost` or a literal loopback IP, not LAN addresses, arbitrary DNS names, or wildcard hosts. It does not change listener bindings or bypass authentication, proxy trust, or exact-origin checks.

## 2. Configure the local proxy and Compose override

Use the repository's `deploy/compose/sqlite-filesystem/nginx-operator.local.conf`. It listens on plain HTTP, overwrites caller-supplied identity headers, and sends API traffic over a dedicated network. Only the proxy's exact address is trusted by the API.

Create or merge the following into `compose.override.yaml`. Preserve existing private CA mounts and `env_file` entries under `app` if you use private Git registries:

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

The `!override` tag replaces the base file's wildcard port publication rather than adding another mapping. Do not publish the app's port directly. Do not enable this fixed-identity proxy in the shared production proxy configuration.

Validate and build the updated application:

```bash
docker compose config --quiet
docker compose build
```

Expected: both commands exit successfully. Rebuilding the app is required when upgrading from a version that does not support the HTTP opt-in. `compose.override.yaml` and the local operator YAML are gitignored. If your override is a symlink shared across worktrees, changes affect that shared override too; relative mount paths are resolved from the Compose project directory.

### Replace an earlier local HTTPS setup

Set all three local URLs (`canonical_url`, `browser_base`, and `admin.origin`) to HTTP and enable `allow_insecure_localhost`. Use the plain-HTTP proxy configuration above and remove the `operator-cert.local.pem` and `operator-key.local.pem` mounts from your override. Existing certificate files can remain unused; private Git CA mounts are unrelated and should remain.

Rebuild the app and recreate app/proxy containers after these changes. No database reset or re-adoption is needed. Open the HTTP URL explicitly; update old HTTPS bookmarks and disable browser HTTPS-only upgrading for this local site if necessary.

## 3. Adopt a fresh database once

Skip adoption if this instance has already been adopted. Restarting, rebuilding, or switching the local listener from HTTPS to HTTP does not require re-adoption.

Stop the app (the SQLite deployment runs API and worker together):

```bash
docker compose stop app
```

Before adopting an existing catalog, take coordinated [database and blob backups](./back-up-and-restore.md), stop any other processes using the stores, and invalidate shared caches if present. Do not use volume deletion as an authority-mode repair.

Once those maintenance prerequisites are satisfied, run:

```bash
docker compose run --rm --no-deps app serve adopt \
  --config /app/marketplace.yaml \
  --actor local-operator \
  --reason "Initial local operator environment" \
  --idempotency-key local-operator-adoption-1 \
  --confirm-maintenance
```

Expected: `Adopted marketplace configuration at revision 1.` If a retry is needed, reuse the same key and request inputs. Afterward, edit registries in the admin UI, not the YAML registry list.

## 4. Start and verify administration

```bash
docker compose up -d --force-recreate app proxy
docker compose ps
docker compose logs --tail=50 app proxy
curl -fsS http://localhost:8080/api/v1/admin/session
```

Expected: app, web, site, and proxy run; app, web, and site become healthy. The proxy's published address must be `127.0.0.1:8080`, not `0.0.0.0:8080`. The session response contains the `local-operator` actor and administration capabilities.

Open `http://localhost:8080/marketplace/admin`. There is no SSO login or certificate prompt. Use exactly `http://localhost:8080`; `http://127.0.0.1:8080` is a different origin and admin mutations will be rejected unless you also change the configured origin.

## 5. Exercise registry discovery and lifecycle

1. Open Registries and add a Git registry whose authority is in `management.allowed_origins`.
2. Enter a ref and repository-relative source root. Use a small repository you control and a root containing a supported resource layout. For GitHub, select the `github` profile above: it explicitly selects the image's CA bundle without adding credentials. Acquisition sanitizes inherited Git environment settings, so do not rely on `GIT_SSL_CAINFO` alone when your Git build needs a non-default CA path.
3. Click **Preview discovery**. Wait for the worker result; inspect resource counts, source paths, diagnostics, and differences from the current publication.
4. Change the root and verify that the preview is marked stale. Preview again before applying.
5. Apply the change and inspect job activity. A successful configuration change does not itself mean indexing succeeded.
6. Test pause, withdrawal, and restoration on your test registry. Pausing stops refresh; withdrawal removes public availability. Restoration does not automatically unpause refresh.

Preview runs discovery without publishing. Apply remains permitted with preview warnings. Git acquisition still needs network access and any required credentials; localhost in a Git URL refers to the worker container, not your laptop. The HTTP admin opt-in does not disable TLS verification for Git origins.

### Use a private registry

Add its exact protocol/host authority to `management.allowed_origins`, then add an origin-bound profile alongside your existing profiles:

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

Pass `LOCAL_GIT_TOKEN` through a gitignored Compose `env_file`. Mount the private CA read-only at the named path, or omit `ca_file` when the standard trust store is sufficient. Use a read-only repository token. Select `private-git` in the registry editor. Restart the app after changing profiles; never enter a token or a server filesystem path into browser registry fields. Existing per-registry YAML credentials do not replace deployment profiles in database mode.

## Troubleshoot startup and access

- **Plain HTTP request sent to HTTPS port:** the old TLS proxy is still running or the wrong configuration is mounted. Use the plain-HTTP config, rebuild the app, and recreate app/proxy. Do not rewrite the browser's `Origin` header to work around this.
- **`configuration authority mode conflict`:** an adopted database is being opened in file mode. Set `management.mode: database` in the file actually mounted at `/app/marketplace.yaml`. Preserve the volume.
- **`managed configuration adoption required`:** no adopted configuration exists for the current instance ID in the current database. Confirm the instance ID and volume first; adopt only if this is the intended unadopted database.
- **Admin requires HTTPS/proxy/issuer fields:** populate the complete `admin` block above and rebuild the image with HTTP opt-in support. Database management and admin authentication are separate settings.
- **403 trusted identity proxy:** check that Nginx targets `172.30.89.3` and that its operator-network address matches the exact `/32` trust entry.
- **403 origin rejected:** use the exact configured HTTP origin, including port. Keep origin validation enabled.
- **Preview reports `profile_unavailable`:** allow the Git authority and select a valid deployment profile. Preserve credential environment variables and CA mounts when editing overrides.
- **Mounted config changes seem ignored:** inspect `docker compose config` locally (it can expose secrets from environment files; do not share its full output). Recreate app/proxy after changing bind-mounted files or symlink targets: `docker compose up -d --force-recreate app proxy`.

## Stop without losing data

```bash
docker compose stop
```

The named volume retains the database, managed configuration, history, and blobs. Do not use `docker compose down -v` unless you deliberately want to destroy that data. For deployment with real users, follow [the production admin-panel guide](./enable-and-use-the-admin-panel.md) instead of this fixed-identity setup.
