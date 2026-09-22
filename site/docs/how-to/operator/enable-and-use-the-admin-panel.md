# How to enable and use the Marketplace admin panel

Enable the database-managed admin panel and manage registries, lifecycle states, license policies, and audit history from a browser.

For laptop testing without an SSO provider, use [the local Docker Compose operator setup](./test-locally-with-compose.md). Its fixed development identity is not suitable for production.

## Overview

The hosted Marketplace admin panel is a private, authenticated interface integrated into the Nuxt frontend (`/marketplace/admin`) and backed by the Go API (`/api/v1/admin`).

It provides live management of:

- **Registries:** add, edit, configure, and reindex Git repositories.
- **Lifecycle states:** pause refresh, withdraw availability, restore, or retire registries without deleting history.
- **License policies:** set global allowlists and undeclared license behavior, with per-registry overrides.
- **Activity & audit trail:** inspect who made which change, review diffs, and restore historical configurations.

### Configuration authority model

A hosted Marketplace runs in one of two authority modes:

| Mode | Authority | Admin panel | Description |
|---|---|:---:|---|
| `file` | Operator YAML | Disabled | Default mode. Registries, license policy, and limits are read from the YAML configuration file on startup. Changes require editing the file and restarting. |
| `database` | Metadata database | Enabled | Registry identities, lifecycle states, and license policies are managed dynamically in the database. Requires explicit one-time maintenance adoption. |

Infrastructure settings (listeners, database DSNs, blob storage, proxy trust, and secret mounts) always remain deployment-managed in the operator YAML file. They cannot be edited from the browser.

## Prerequisites

- A running hosted Marketplace deployment (Go API, worker, and Nuxt frontend) using PostgreSQL or SQLite.
- A reverse proxy or identity-aware ingress (such as Nginx, Traefik, or an OAuth2 proxy) capable of TLS termination and SSO authentication.
- Access to run the `agent-marketplace` CLI on the server for the one-time adoption procedure.
- Coordinated metadata and blob backups prior to adoption.

---

## 1. Configure the operator YAML

Update your operator configuration file (`marketplace.yaml`) to enable database management and configure administration trust.

```yaml
apiVersion: agent-marketplace.io/marketplace/v1
instance:
  id: team-marketplace
  name: Team Agent Marketplace
  canonical_url: https://marketplace.example.com
  browser_base: https://marketplace.example.com/marketplace

# ... storage, listeners, refresh, and policy settings ...

management:
  mode: database
  allowed_origins:
    - https://github.com
    - https://gitlab.example.com
  profiles:
    internal-git:
      allowed_origins:
        - https://gitlab.example.com
      credentials:
        token:
          file: /run/secrets/gitlab-token
      ca_file: /etc/agent-marketplace/ca/internal.pem

admin:
  enabled: true
  origin: https://marketplace.example.com
  trusted_proxies:
    - 10.0.0.0/8
    - 172.16.0.0/12
  issuer: https://sso.example.com
  subject_header: X-Marketplace-Subject
  groups_header: X-Marketplace-Groups
  admin_groups:
    - marketplace-admins
```

### Key configuration fields

- `management.mode`: set to `database`. The service will refuse to start until adoption is completed.
- `management.allowed_origins`: allowed Git remote authorities for admin-created registries. Must contain protocol and host/port only (no URL paths or trailing slashes).
- `management.profiles`: optional credential and trust profiles for private registries. The browser selects profiles by opaque ID (`internal-git`); secret paths and tokens are never sent to or edited from the browser.
- `admin.enabled`: enables the administration API routes (`/api/v1/admin/**`).
- `admin.origin`: the exact HTTPS origin of the frontend in canonical serialized form — lowercase scheme, a strictly lowercase ASCII DNS host (use the Punycode A-label) or a canonical literal IP host (IPv6 in brackets; IPv4-mapped IPv6 forms such as `[::ffff:192.0.2.1]` are rejected — use the canonical IPv4 literal), no default or leading-zero port, and no path (explicit non-default ports such as a loopback `http://localhost:8080` are valid). Cross-origin mutations are strictly rejected.
- `admin.trusted_proxies`: CIDR blocks of immediate reverse proxies allowed to assert identity headers. Forwarded client IPs (`X-Forwarded-For`) do not bypass this check.
- `admin.subject_header`: header containing the authenticated user identity (e.g. username or email).
- `admin.groups_header`: header containing a JSON string array of group memberships, such as `["marketplace-admins", "developers"]`. Comma-separated strings are rejected.
- `admin.admin_groups`: list of group names authorized to access the admin panel.
- `admin.admin_users`: list of immutable provider subjects authorized to access the admin panel. At least one of `admin_groups` or `admin_users` must be configured.

### Authentication modes

Administration selects exactly one authentication mode through `admin.authentication.mode`:

| Mode | Identity source | When to use |
|---|---|---|
| `oidc` | Native Okta Authorization Code sign-in with S256 PKCE and a signed host-only session cookie (`__Host-` prefixed on HTTPS). | Recommended for production. |
| `trusted_proxy` | Identity headers asserted by an authenticated reverse proxy or SSO gateway. | Existing deployments; inferred automatically when the complete proxy field set is configured without an explicit mode. |

The modes are mutually exclusive: OIDC mode rejects `trusted_proxies`, `issuer`, `subject_header`, and `groups_header`; trusted-proxy mode rejects `authentication.oidc` fields.

### OIDC mode configuration

```yaml
admin:
  enabled: true
  origin: https://marketplace.example.com
  authentication:
    mode: oidc
    oidc:
      issuer: https://idp.example.com/oauth2/default
      client_id: marketplace-admin-client-id
      session_secret_env: MARKETPLACE_ADMIN_SESSION_SECRET
  admin_groups:
    - marketplace-admins
  admin_users:
    - 00u123exampleOktaSubject
```

The dedicated Okta application contract:

- Public Authorization Code client; client authentication disabled; S256 PKCE required.
- Sign-in redirect URI exactly `<origin>/api/v1/admin/auth/callback`, for example `https://marketplace.example.com/api/v1/admin/auth/callback`. No sign-out redirect is required; logout is a local, idempotent `POST /api/v1/admin/auth/logout` that expires the session cookie and never performs global Okta logout.
- Grant scopes `openid profile email groups`; RS256 ID-token signing; a `groups` ID-token claim as an array of strings (override with `groups_claim` only when the issuer uses a different single claim name).
- `session_secret_env` or `session_secret_file` names an environment or file reference resolving to 32 through 4096 bytes of secret key material; surrounding whitespace is trimmed before the bound is applied. The secret is never stored in YAML and signs two `HttpOnly`, `SameSite=Lax` cookies. With an HTTPS `admin.origin` — which production requires — the cookies are named `__Host-marketplace_admin_session` and `__Host-marketplace_admin_login` and carry `Secure`, `Path=/`, and no `Domain`; the `__Host-` prefix lets the browser itself enforce host-only isolation, so a sibling-domain cookie with the same logical name can never be confused with them. This deliberately replaces the narrower per-path confinement used on loopback HTTP. The explicit `allow_insecure_localhost` loopback HTTP development exception instead keeps the unprefixed names without `Secure` so browsers accept the cookie over plain HTTP: the transient login transaction cookie is confined to `/api/v1/admin/auth`, and the session cookie is confined to `/api/v1/admin` (default lifetime 12h, maximum 24h, no `Domain` attribute). Both signed cookie values are bounded to 3900 bytes so the complete `Set-Cookie` line stays below the 4096-byte browser limit; a verified, authorized identity whose claims exceed that bound is rejected with `401 authentication_rejected` and receives no session cookie.
- A verified identity is authorized when one of its groups matches `admin_groups` or its immutable `sub` matches `admin_users`. Every admin request re-evaluates authorization; a session that no longer matches receives `403 forbidden`, and a missing or expired session receives `401 unauthenticated` and is never refreshed implicitly.

With OIDC mode the reverse proxy only routes `/api/v1/admin/` and `/marketplace/admin` to the application; it must not inject identity headers. Public Marketplace routes remain anonymous and available when the identity provider is unavailable or the OIDC configuration is invalid.

Stable sign-in outcomes: `400 authentication_request_invalid` (bad destination or callback shape), `401 authentication_rejected` (state, token, signature, or claims), `503 authentication_unavailable` (discovery, JWKS, or token endpoint unavailable), `503 admin_unavailable` (unsafe configuration), `401 unauthenticated` (missing/expired session), `403 forbidden` (verified but unauthorized identity).

---

## 2. Configure the reverse proxy

With `authentication.mode: oidc` the application authenticates administrators itself; the proxy only routes traffic. The steps below apply to `trusted_proxy` mode, where the API trusts identity headers asserted by your authenticated reverse proxy or SSO gateway.

Your reverse proxy must:

1. Terminate TLS and authenticate operators against your identity provider.
2. Strip any incoming client-supplied identity headers to prevent header spoofing.
3. Inject the authenticated user identity and groups headers before proxying to the API.
4. Route both `/marketplace/admin` (Nuxt frontend) and `/api/v1/admin/` (Go API) through the authenticated path.

### Example Nginx snippet

```nginx
upstream marketplace_api {
    server app:8080;
}

upstream marketplace_web {
    server web:3000;
}

server {
    listen 443 ssl http2;
    server_name marketplace.example.com;

    # SSL configuration ...

    # 1. Strip incoming identity headers from the client
    proxy_set_header X-Marketplace-Subject "";
    proxy_set_header X-Marketplace-Groups "";

    # 2. Public endpoints (no authentication required)
    location / {
        proxy_pass http://landing_site;
        include /etc/nginx/proxy-headers.conf;
    }

    location /marketplace/ {
        proxy_pass http://marketplace_web;
        include /etc/nginx/proxy-headers.conf;
    }

    location /api/ {
        proxy_pass http://marketplace_api;
        include /etc/nginx/proxy-headers.conf;
    }

    # 3. Protected admin routes (SSO authentication enforced)
    location ^~ /marketplace/admin {
        # Enforce SSO / OAuth2 proxy authentication here
        auth_request /oauth2/auth;

        # Inject authenticated operator identity
        auth_request_set $auth_user $upstream_http_x_auth_request_user;
        auth_request_set $auth_groups $upstream_http_x_auth_request_groups;

        proxy_set_header X-Marketplace-Subject $auth_user;
        proxy_set_header X-Marketplace-Groups $auth_groups; # Must be JSON array: ["marketplace-admins"]

        proxy_pass http://marketplace_web;
        include /etc/nginx/proxy-headers.conf;
    }

    location ^~ /api/v1/admin/ {
        # Enforce SSO / OAuth2 proxy authentication here
        auth_request /oauth2/auth;

        auth_request_set $auth_user $upstream_http_x_auth_request_user;
        auth_request_set $auth_groups $upstream_http_x_auth_request_groups;

        proxy_set_header X-Marketplace-Subject $auth_user;
        proxy_set_header X-Marketplace-Groups $auth_groups; # Must be JSON array: ["marketplace-admins"]

        proxy_pass http://marketplace_api;
        include /etc/nginx/proxy-headers.conf;
    }
}
```

::: warning Groups header must be a JSON array
The `groups_header` value must be formatted as a JSON array of strings, for example `["marketplace-admins"]`. If your identity proxy forwards groups as comma-separated values (e.g. `marketplace-admins,engineers`), configure your proxy or lua filter to encode them as a JSON array.
:::

---

## 3. Adopt catalog settings during a maintenance window

Adoption is a one-time operation that transfers catalog settings from your YAML file into the metadata database and establishes database authority.

### Step 3.1: Stop processes and take backups

1. Stop **all** running API and worker processes. Do not perform adoption while instances are writing to the database.
2. Take coordinated backups of both the metadata database and the artifact blob store as described in [How to back up and restore a Marketplace](./back-up-and-restore.md).
3. Invalidate any shared reverse-proxy or CDN caches that may hold previously cached content.

### Step 3.2: Run the adoption command

Run `agent-marketplace serve adopt` using the updated configuration file:

```bash
agent-marketplace serve adopt \
  --config /app/marketplace.yaml \
  --actor "operator-username" \
  --reason "Initial adoption of database-managed catalog administration" \
  --idempotency-key "maintenance-cutover-1" \
  --confirm-maintenance
```

Expected output:

```text
Adopted marketplace configuration at revision 1. Database authority is durable; runtime safety checks still apply.
```

If the command fails:
- Resolve the reported validation error (such as an origin missing from `management.allowed_origins` or an unresolved credential profile).
- Re-run the command with the **same** `--idempotency-key`.

### Step 3.3: Start API and worker processes

Start the API and worker processes. Verify startup:

1. Check private readiness:
   ```bash
   curl -fsS http://127.0.0.1:9090/readyz
   ```
2. Verify that unauthenticated requests to `/api/v1/admin/configuration` return `401 Unauthorized` or `403 Forbidden`.
3. Verify that requests without the `trusted_proxies` IP are rejected.

::: tip YAML changes after adoption
After adoption, the database is the source of truth for registries, lifecycle, and license policy. Modifying the `registries:` block in `marketplace.yaml` will have no effect on runtime behavior. You can safely remove the `registries:` list from `marketplace.yaml` once adoption is complete.
:::

---

## 4. Work with the admin panel

Log in through your reverse proxy and navigate to `/marketplace/admin`.

The admin navigation bar provides four sections:

1. **Registries**: manage Git repositories, roots, and lifecycle.
2. **Policies**: manage global license allowlists and undeclared license handling.
3. **Activity**: view the full audit history and restore historical settings.
4. **Instance**: inspect read-only deployment parameters.

---

### Managing registries

The **Registries** page lists all managed repositories with their slug, configured Git ref, lifecycle status, generation, and publication status.

#### Adding a new registry

1. Click **Add registry**.
2. Fill in the registry details:
   - **Slug:** URL-safe identifier (e.g. `community-skills`).
   - **Origin:** Git repository URL (must match one of the authorities declared in `management.allowed_origins`).
   - **Ref:** branch name, tag, or commit SHA (e.g. `main`).
   - **Ordered roots:** relative repository paths where agent resources are located (e.g. `.`, `.agents`, `.claude`), with optional source harness selection.
   - **Acquisition mode:**
     - `mirror`: Marketplace builds and hosts immutable artifact bundles.
     - `index-only`: Marketplace records metadata only; clients acquire files directly from the Git origin.
   - **Publisher metadata:** optional display name and organization URL.
   - **Default license:** default SPDX license identifier for resources in this registry.
   - **Credential profile:** select an approved credential/trust profile configured in `management.profiles` if authentication or a private CA is required.
3. Optionally click **Preview discovery** to see what indexing would find before anything is applied (see below).
4. Review the proposal diff, provide a mandatory **Reason** for the audit log, and click **Apply change**.

#### Previewing discovery before applying

A wrong source root is the most common reason a new registry ends up empty. The **Discovery preview** panel under the source roots asks a worker to clone the origin at the configured ref and run the same discovery that indexing uses, without publishing anything:

- Each root lists the skills, commands, subagents, hooks, MCP servers and collections it exposes, with the source path of every resource. A root that exposes nothing is called out explicitly.
- For an existing registry, resources are marked **New** or **Changed** against the current publication, and resources that are currently published but would no longer be discovered are listed separately.
- A root that does not exist at that ref, or that has no recognizable layout, fails the preview with the same error indexing would report, naming the offending root. Git output is never shown because it can contain credentials.
- The result is bound to the exact draft it was requested for. Editing any field marks it stale; the Review step then says that this exact draft has not been previewed.

Previews are rate limited per operator and retained for 24 hours. In an API-only deployment they stay queued until a worker process picks them up. A preview observes one commit: a mutable ref can still move before indexing runs, so pin an exact commit in the ref field when that matters. Applying never requires a successful preview; the Review step only warns.

#### The Edit → Validate → Review → Apply workflow

All admin mutations follow an explicit validation flow:

1. **Edit:** make changes in page memory. Drafts remain in your browser session and are not autosaved to the server. Registry drafts can request a discovery preview from here.
2. **Validate:** static checks verify syntax, paths, origin allowlists, and profile bindings.
3. **Review:** the UI displays a structured field-by-field diff showing the current state versus the proposed state.
4. **Apply:** the change is submitted with an optimistic concurrency check (`If-Match: "<revision>"`). If another operator applied a change in the meantime, the submission is rejected and you are prompted to reload the latest revision.
5. **Observe:** upon acceptance, the instance configuration revision increments, an audit entry is committed, and background indexing is enqueued.

---

### Managing registry lifecycle

Click on any registry to open its detail view. Under the **Configuration** tab, open **Lifecycle and indexing actions** to perform lifecycle transitions:

| Action | Indexing / refresh | Public availability | Description |
|---|:---:|:---:|---|
| **Active** | Enabled | Available | Normal operating state. Scheduled and manual reindexing are permitted. |
| **Pause refresh** | Stopped | Available | Pauses automated refresh. Existing published resources remain discoverable and installable. |
| **Resume refresh** | Resumed | Available | Resumes indexing for a paused registry. Does not lift withdrawals. |
| **Withdraw availability** | Stopped | Blocked | Immediately blocks serving and installation of all resources from this registry across all API endpoints, including historical exact revisions. Resource anchors remain in the catalog projection marked as withdrawn for client reconciliation. |
| **Restore availability** | Paused | Re-evaluated | Removes the admin withdrawal and places the registry in **Paused** state. Serving is re-evaluated against live license policy. You must explicitly click **Resume refresh** when ready to restart indexing. |
| **Retire registry** | Disabled | Blocked | Retires an already withdrawn registry. It is hidden from the default registry list. Immutable IDs, historical origins, and audit records are permanently retained to prevent namespace hijacking. |
| **Restore retired** | Disabled | Blocked | Restores a retired registry to the active management list in **Withdrawn** state. |
| **Request reindex** | Queued | Current | Enqueues a durable background indexing job for the registry. |

::: warning Non-destructive lifecycle
Registry deletion is not supported. Retiring a registry marks it as inactive while preserving its identity and history. This guarantees that old lockfiles referencing historical revisions remain resolvable and that slugs cannot be repurposed for untrusted repositories.
:::

---

### Managing license and rejected-resource policies

Navigate to **Policies** to manage global license enforcement and rejected-resource distribution. Choose **Block publication** (default) or **Expose as operator-approved**. Global exposure requires acknowledging that rejected resources from every inheriting registry may become installable; their reject assessment and findings remain visible, and revoked resources remain blocked.

#### Global license policy

- **Allowlist:** a list of approved SPDX license identifiers (e.g. `MIT`, `Apache-2.0`, `BSD-3-Clause`). If empty, license enforcement is completely disabled.
- **Undeclared licenses:** controls the behavior when an indexed resource does not declare a license:
  - `reject`: resource is blocked from publication (default).
  - `warn`: resource remains installable, but an advisory warning is recorded.
  - `allow`: resource is accepted without warnings.

#### Immediate availability enforcement

When you update the license policy:

- **Tightening policy (more restrictive):** takes effect **immediately** for public downloads and API queries. Resources that do not meet the new policy are blocked right away, without waiting for a reindex cycle.
- **Relaxing policy (more permissive):** takes effect for newly indexed content. To make previously blocked resources available, trigger a reindex of the affected registries.

#### Per-registry overrides

In the registry editor, you can override the global allowlist, undeclared license behavior, and **Rejected resource availability**. Choose **Inherit global policy**, **Block**, or **Expose as operator-approved**. Changing only rejected-resource availability takes effect immediately and does not enqueue a reindex; the activity log records the audited change.

---

### Auditing and restoring settings (Activity)

The **Activity** page records an immutable, append-only audit trail of every configuration change and reindex request:

- **Revision progression:** e.g. `r12 → r13`.
- **Operator identity:** the subject and issuer asserted by your identity proxy.
- **Timestamp & Reason:** when the change was made and the operator-supplied rationale.
- **Linked jobs:** background indexing jobs triggered by the change.
- **Snapshot inspection:** expand any entry to inspect the complete managed JSON configuration at that point in time.

#### Restoring historical settings

To roll back a configuration change:

1. Locate the desired revision in the **Activity** log.
2. Click **Prepare policy settings** or **Prepare [registry] settings**.
3. The admin panel loads the historical values into a new draft proposal against the **current** configuration revision.
4. Review the diff, supply a reason for the rollback, and apply the change.

::: note Restores do not erase history
Restoring a historical configuration increments the revision number (e.g. creating `r14` from `r11`) and records a new audit entry. It does not overwrite or delete audit history, and it does not alter current lifecycle states or aliases.
:::

---

## 5. Troubleshooting & operational safeguards

### Common error codes

| Error code | Cause | Remediation |
|---|---|---|
| `unauthenticated` | Missing or malformed identity headers, or `groups_header` is not a valid JSON array. | Check reverse proxy logs. Ensure headers are forwarded and the groups header is formatted as `["group-name"]`. |
| `forbidden` | The client IP is not in `admin.trusted_proxies`, or the user is not in `admin.admin_groups`. | Verify the immediate proxy IP matches configured CIDRs and that the user belongs to the required admin group. |
| `origin_rejected` | The request `Origin` header does not match `admin.origin`. | Ensure requests are sent directly to the configured HTTPS origin and not via an alternative host or IP address. |
| `request_header_required` | Missing `X-Marketplace-Admin: 1` header. | Use the built-in browser UI; custom scripts must include this header on POST requests. |
| `stale_revision` / `conflict` | Another operator updated the configuration while your draft was open. | Reload the page to view the latest revision, re-apply your edits, and submit again. |

### Disabling the admin panel

If you need to disable browser administration while keeping database management active:

```yaml
admin:
  enabled: false
```

Setting `enabled: false` shuts down the `/api/v1/admin/**` endpoints immediately. It does **not** revert the marketplace to file-managed authority; database-managed registries and policies remain active.

## Related documentation

- [Marketplace operator configuration reference](../../reference/marketplace-operator-configuration.md)
- [How to configure a reverse proxy](./configure-a-reverse-proxy.md)
- [How to deploy a production Marketplace](./deploy-a-production-marketplace.md)
- [How to back up and restore a Marketplace](./back-up-and-restore.md)
- [How to manage ingestion policy](./manage-ingestion-policy.md)
- [How to reindex and monitor a Marketplace](./reindex-and-monitor.md)
