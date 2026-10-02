# How to enable and use the Marketplace admin panel

Turn on the database-managed admin panel, then manage registries, lifecycle states, license policies, and audit history from a browser.

To try it on a laptop without an SSO provider, use [the local Docker Compose operator setup](./test-locally-with-compose.md). Its fixed development identity is not suitable for production.

## Overview

The admin panel is a private, authenticated part of the Nuxt frontend at `/marketplace/admin`, backed by the Go API under `/api/v1/admin/`. From it you can:

- add, edit, and reindex Git registries;
- pause refresh, withdraw availability, restore, or retire a registry without deleting its history;
- set the global license allowlist, the handling of undeclared licenses, and per-registry overrides;
- review who changed what and why, and restore settings from an earlier revision.

### Configuration authority model

A hosted Marketplace runs in one of two authority modes, set by `management.mode`:

| Mode | Authority | Admin panel | Description |
|---|---|:---:|---|
| `file` | Operator YAML | Disabled | Default mode. The Marketplace reads registries, license policy, and limits from the YAML configuration at startup. To change them, edit the file and restart. |
| `database` | Metadata database | Enabled | Registry identities, lifecycle states, and license policies live in the database and change through the admin panel. Requires a one-time adoption during maintenance. |

Infrastructure settings (listeners, database DSNs, blob storage, proxy trust, and secret mounts) always stay in the operator YAML file and cannot be edited from the browser.

## Prerequisites

- A running hosted Marketplace (API, worker, and Nuxt frontend) using PostgreSQL or SQLite.
- A reverse proxy or ingress that terminates TLS and routes `/marketplace/admin` and `/api/v1/admin/` to the application. In `trusted_proxy` mode it must also authenticate operators against your identity provider.
- Shell access to run the `agent-marketplace` CLI against the deployment's configuration for the one-time adoption.
- Coordinated metadata and blob backups taken before adoption.

## 1. Configure the operator YAML

Update your operator configuration file (`marketplace.yaml`) to turn on database management and configure who may administer the Marketplace. This example uses `trusted_proxy` authentication:

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

### Configuration fields

| Field | Purpose |
|---|---|
| `management.mode` | Set to `database`. In this mode the service refuses to start until adoption has completed. |
| `management.allowed_origins` | Git remote origins that administrators may use for registries. Each entry is an `https` or `ssh` scheme plus a host and optional port, with no path or trailing slash. |
| `management.profiles` | Optional credential and trust profiles for private registries. The browser selects a profile by its ID (`internal-git` above). Secret paths and tokens are never sent to or edited from the browser. |
| `admin.enabled` | Turns on the administration API routes under `/api/v1/admin/`. |
| `admin.origin` | The exact HTTPS origin of the frontend, in canonical form (see below). The API rejects admin requests whose `Origin` header differs. |
| `admin.trusted_proxies` | CIDR blocks of the immediate reverse proxies allowed to assert identity headers. The API checks the connecting peer address, so `X-Forwarded-For` cannot bypass this check. |
| `admin.issuer` | Label recorded as the issuer of every proxy-asserted identity in the audit log. |
| `admin.subject_header` | Header that carries the authenticated user's stable identity-provider subject. Use a permanent ID rather than an email address or username that can change or be reassigned, because `admin_users` and the audit log match on this value. |
| `admin.groups_header` | Header that carries group memberships as a JSON array of strings, such as `["marketplace-admins", "developers"]`. Comma-separated values are rejected. |
| `admin.admin_groups` | Group names allowed to use the admin panel. |
| `admin.admin_users` | Immutable provider subjects allowed to use the admin panel. Configure at least one of `admin_groups` or `admin_users`. |

`admin.origin` must be serialized exactly as a browser sends it:

- a lowercase scheme;
- a lowercase ASCII DNS host (use the Punycode A-label for internationalized names) or a canonical IP literal, with IPv6 in brackets;
- no IPv4-mapped IPv6 form such as `[::ffff:192.0.2.1]` (use the plain IPv4 literal instead);
- no default port, no leading zeros in the port, and no path.

An explicit non-default port is valid, for example `http://localhost:8080` for loopback development with `allow_insecure_localhost`.

### Authentication modes

`admin.authentication.mode` selects exactly one authentication mode:

| Mode | Identity source | When to use |
|---|---|---|
| `oidc` | Native Okta Authorization Code sign-in with S256 PKCE and a signed host-only session cookie (`__Host-` prefixed on HTTPS). | Recommended for production. |
| `trusted_proxy` | Identity headers asserted by an authenticated reverse proxy or SSO gateway. | Existing deployments. Selected automatically when `trusted_proxies`, `issuer`, `subject_header`, and `groups_header` are all set and no mode is given. |

The modes exclude each other. OIDC mode rejects `trusted_proxies`, `issuer`, `subject_header`, and `groups_header`, and trusted-proxy mode rejects `authentication.oidc` fields.

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

Create a dedicated Okta application with these settings:

- A public Authorization Code client with client authentication turned off and S256 PKCE required.
- The sign-in redirect URI set exactly to `<origin>/api/v1/admin/auth/callback`, for example `https://marketplace.example.com/api/v1/admin/auth/callback`. You do not need a sign-out redirect: logout is a local `POST /api/v1/admin/auth/logout` that expires the session cookie, is safe to repeat, and never signs the user out of Okta globally.
- The scopes `openid profile email groups`, RS256 ID-token signing, and a `groups` ID-token claim that holds an array of strings. Set `groups_claim` only when your issuer uses a different claim name.

Provide the session secret with exactly one of `session_secret_env` (an environment variable name) or `session_secret_file` (a file path). After surrounding whitespace is trimmed, the secret must be 32 to 4096 bytes. It never appears in the YAML file. It signs two cookies, both `HttpOnly` and `SameSite=Lax`:

- With an HTTPS `admin.origin`, which production requires, the cookies are named `__Host-marketplace_admin_session` and `__Host-marketplace_admin_login` and carry `Secure`, `Path=/`, and no `Domain`. The `__Host-` prefix makes the browser enforce host-only isolation, so a cookie with the same name set by a sibling domain cannot be mistaken for them.
- With `allow_insecure_localhost` on loopback HTTP, the cookies keep their unprefixed names and omit `Secure`, so browsers accept them over plain HTTP. The sign-in transaction cookie is limited to `/api/v1/admin/auth` and the session cookie to `/api/v1/admin`, and neither sets `Domain`.

The session lasts 12 hours by default; set `session_ttl` to change it, up to 24 hours. Each signed cookie value is limited to 3900 bytes so that the complete `Set-Cookie` line stays below the browser's 4096-byte limit. A verified, authorized identity whose claims exceed that limit is rejected with `401 authentication_rejected` and receives no session cookie.

A verified identity is authorized when one of its groups matches `admin_groups` or its immutable `sub` claim matches `admin_users`. Authorization is checked again on every admin request. A session that no longer matches receives `403 forbidden`. A missing or expired session receives `401 unauthenticated` and is never refreshed automatically.

In OIDC mode the reverse proxy only routes `/api/v1/admin/`, `/marketplace/admin`, and the `/marketplace/auth/error` sign-in error page to the application and must not inject identity headers. Public Marketplace routes stay anonymous and keep working when the identity provider is down or the OIDC configuration is invalid.

Sign-in failures use these stable codes. When a browser navigates to the login or callback endpoint (an `Accept` header that prefers `text/html` over JSON), failures redirect to the fixed same-origin `/marketplace/auth/error` page, which explains the allowlisted code, offers a fresh sign-in where retry can help, and links back to the Marketplace. API clients, missing `Accept`, wildcard-only, and tied preferences keep the JSON error bodies and statuses below. The error page never reflects provider error descriptions, state, authorization codes, tokens, or identity claims, and an unknown or malformed code shows a generic message.

Manual recovery: denied or rejected sign-ins can retry from the page; `forbidden`, `admin_unavailable`, and `origin_rejected` require operator action (authorization, configuration, or the configured origin) rather than a retry.

| Status and code | Meaning |
|---|---|
| `400 authentication_request_invalid` | Bad sign-in destination or callback shape. |
| `401 authentication_rejected` | State, token, signature, or claims failed verification. |
| `503 authentication_unavailable` | The issuer's discovery, JWKS, or token endpoint is unreachable. |
| `503 admin_unavailable` | The administration configuration is unsafe or invalid. |
| `401 unauthenticated` | The session is missing or expired. |
| `403 forbidden` | The identity is verified but not authorized. |
| `403 origin_rejected` | The request Origin header does not match the configured administration origin. |

## 2. Configure the reverse proxy

This step applies to `trusted_proxy` mode, where the API trusts identity headers from your authenticated reverse proxy or SSO gateway. With `authentication.mode: oidc`, the application authenticates administrators itself and the proxy only routes traffic.

Your reverse proxy must:

1. Terminate TLS and authenticate operators against your identity provider.
2. Remove any identity headers the client sent, so a client cannot spoof them.
3. Set the authenticated user's identity and groups headers before proxying to the API.
4. Route both `/marketplace/admin` (Nuxt frontend) and `/api/v1/admin/` (Go API) through the authenticated path.

### Example Nginx snippet

```nginx
upstream marketplace_api {
    server app:8080;
}

upstream marketplace_web {
    server web:3000;
}

upstream landing_site {
    server site:8080;
}

server {
    listen 443 ssl http2;
    server_name marketplace.example.com;

    # SSL configuration ...

    # Public endpoints (no authentication required)
    location / {
        proxy_pass http://landing_site;
        include /etc/nginx/proxy-headers.conf;
    }

    location /marketplace/ {
        proxy_pass http://marketplace_web;
        include /etc/nginx/proxy-headers.conf;
    }

    location /_fonts/ {
        proxy_pass http://marketplace_web;
        include /etc/nginx/proxy-headers.conf;
    }

    location = /.well-known/agent-marketplace.json {
        proxy_pass http://marketplace_api;
        include /etc/nginx/proxy-headers.conf;
    }

    location = /healthz {
        proxy_pass http://marketplace_api;
        include /etc/nginx/proxy-headers.conf;
    }

    location /api/ {
        proxy_pass http://marketplace_api;
        include /etc/nginx/proxy-headers.conf;
        # A location that sets any proxy_set_header does not inherit
        # server-level ones, so strip client identity headers here.
        proxy_set_header X-Marketplace-Subject "";
        proxy_set_header X-Marketplace-Groups "";
    }

    # Protected admin routes (SSO authentication enforced)
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

The `/oauth2/auth` subrequest location belongs to your SSO proxy and is not shown. The API reads identity headers only on `/api/v1/admin/` routes and only from peers in `admin.trusted_proxies`.

::: warning Groups header must be a JSON array
The `groups_header` value must be a JSON array of strings, for example `["marketplace-admins"]`. If your identity proxy forwards groups as comma-separated values (such as `marketplace-admins,engineers`), configure the proxy or a Lua filter to encode them as a JSON array.
:::

## 3. Adopt catalog settings during a maintenance window

Adoption is a one-time operation. It copies the registries and license policy from your YAML file into the metadata database and makes the database the authority for them.

### Step 3.1: Stop processes and take backups

1. Stop all running API and worker processes. Do not adopt while any instance is writing to the database.
2. Take coordinated backups of the metadata database and the artifact blob store as described in [How to back up and restore a Marketplace](./back-up-and-restore.md).
3. Invalidate any shared reverse-proxy or CDN caches that may hold cached content.

The adoption command does not check that processes are stopped or caches invalidated. It refuses to run without `--confirm-maintenance`, which is your confirmation that you did.

### Step 3.2: Run the adoption command

Run `agent-marketplace serve adopt` with the updated configuration file:

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

1. Fix the reported validation error, such as an origin missing from `management.allowed_origins` or an unresolved credential profile.
2. Run the command again with the same `--idempotency-key`. A retry with the same key and the same configuration and reason returns the original result; a different configuration or reason under that key is rejected as an idempotency conflict.

Adoption also fails if the database already stores a registry that the YAML `registries` list leaves out.

### Step 3.3: Start API and worker processes

Start the API and worker processes, then verify:

1. Private readiness responds on the operations listener:
   ```bash
   curl -fsS http://127.0.0.1:9090/readyz
   ```
2. An unauthenticated request to `/api/v1/admin/configuration` returns `401` or `403`.
3. In `trusted_proxy` mode, a request that does not come from an address in `trusted_proxies` returns `403`.

::: tip YAML changes after adoption
After adoption, the database is the source of truth for registries, lifecycle, and license policy. Changes to the `registries:` block in `marketplace.yaml` have no effect at runtime, and you can remove the `registries:` list once adoption is complete.
:::

## 4. Work with the admin panel

Open `/marketplace/admin`. In `trusted_proxy` mode your reverse proxy signs you in. In `oidc` mode the panel shows **Sign in with Okta** when you have no session.

The navigation bar has four sections:

1. **Registries**: add registries, edit their roots and settings, and change their lifecycle.
2. **Policies**: manage the global license allowlist, undeclared-license handling, and rejected-resource distribution.
3. **Activity**: view the audit history and restore earlier settings.
4. **Instance**: inspect read-only deployment details such as the version, management mode, and configuration revision.

### Managing registries

The **Registries** page lists managed registries with their slug and ID, origin, ref, and lifecycle state. By default it shows current registries; choose **All, including retired** in **Show** to include retired ones. The lifecycle shown is the desired state, not indexing health. Open a registry's **Indexing history** tab for the recorded indexing attempts.

#### Adding a new registry

1. Click **Add registry**.
2. Fill in the registry details:
   - **Immutable registry ID**: the stable machine identity. It cannot change after creation.
   - **Slug**: the name shown in the catalog and in install commands (lowercase letters, digits, and hyphens). You can change it later; the old slug stays reserved as an alias.
   - **Git origin**: an HTTPS or SSH repository URL whose origin is listed in `management.allowed_origins`.
   - **Ref or exact commit**: a branch, tag, or commit SHA (for example `main`).
   - **Acquisition**: **Mirror** stores immutable artifact bundles on the Marketplace; **Index only** records metadata, and clients fetch files directly from the Git origin. New drafts start as **Index only**.
   - **Source roots**: directories inside the repository to scan, in priority order (for example `.`, `.agents`, `.claude`), each with an optional source harness (**Autodetect** by default).
   - **Credential / trust profile**: a profile from `management.profiles`, when the origin needs authentication or a private CA.
   - **Publisher name** and **Publisher URL**: optional display metadata.
   - **Default license**: the SPDX identifier applied to resources that declare no license.
3. Optionally click **Preview discovery** to see what indexing would find before anything is applied (see below).
4. Enter a **Reason for change**, which the audit log records, and click **Continue to review**.
5. Review the field-by-field diff, confirm that you reviewed it, and click **Apply change**.

#### Previewing discovery before applying

A wrong source root is the most common reason a new registry ends up empty. The **Discovery preview** panel below the source roots asks a worker to clone the origin at the configured ref and run the same discovery that indexing uses, without publishing anything:

- Each root lists the skills, commands, subagents, hooks, MCP servers, and collections it exposes, with the source path of every resource. The panel warns about any root that exposes nothing.
- For an existing registry, resources are marked **New** or **Changed** against the current publication, and resources that are published now but would no longer be discovered are listed separately.
- If a root does not exist at that ref or has no recognizable layout, the preview fails with the same error indexing would report and names the root. Git output is never shown because it can contain credentials.
- The result belongs to the exact draft it was requested for. Editing any field marks it out of date, and the Review step then says that this exact draft has not been previewed.

Each operator can request five previews per minute. Previews are kept for 24 hours. In an API-only deployment they stay queued until a worker process picks them up. A preview observes one commit, and a branch or tag can move before indexing runs; enter an exact commit in the ref field when that matters. Applying never requires a successful preview: the Review step only warns.

#### The edit, review, and result workflow

Every admin change goes through the same three steps, shown at the top of the editor:

1. **Edit**: change the draft in the page. Drafts stay in the current browser tab and are never saved to the server. Registry drafts can request a discovery preview here. **Continue to review** runs static validation on the server: syntax, paths, origin allowlists, and profile bindings. Problems are highlighted on the affected fields.
2. **Review**: the editor shows a field-by-field diff of the current and proposed values, the reason, and the preview outcome. You confirm that you reviewed the diff, then click **Apply change**. The browser sends the change with the revision you started from (`If-Match: "<revision>"`). If another operator applied a change in the meantime, the API rejects yours with `revision_conflict`, and **Load latest revision and keep my draft** reloads the configuration so you can review a fresh diff.
3. **Result**: the configuration revision increases, the audit entry is recorded, and any indexing work is queued. The job status refreshes for about a minute; click **Refresh job status** to check again. An accepted change does not mean indexing succeeded.

---

### Managing registry lifecycle

Open a registry to see its detail view. On the **Configuration** tab, the **Actions** panel lists the lifecycle actions that apply to the registry's current state. Each action opens a draft that you review and apply like any other change.

| Action | Available when | Result |
|---|---|---|
| **Pause refresh** | Active | Stops refresh and publication work. Published resources stay discoverable and installable, subject to policy. |
| **Resume refresh** | Paused, not withdrawn | Returns the registry to active and queues indexing. Does not lift a withdrawal. |
| **Withdraw availability** | Active or paused | Blocks server-side downloads and installation of every resource from the registry, including retained exact revisions, and pauses refresh. Copies already installed on user machines are not removed. |
| **Restore availability** | Withdrawn | Removes only the admin withdrawal and leaves the registry paused. Deployment revocations and license policy still apply. Click **Resume refresh** when you want indexing to restart. |
| **Retire registry** | Withdrawn | Retires the registry: no refresh and no downloads. It disappears from the default registry list. Its ID, slugs, historical origins, and audit records are kept permanently so they cannot be reused for another repository. |
| **Restore retired registry** | Retired | Returns the registry to management in the withdrawn state. Restore availability and resume refresh separately afterward. |
| **Request reindex** | Active | Queues a durable indexing job. Acceptance does not mean publication succeeded. |

::: warning Registries cannot be deleted
The admin panel has no delete action. Retiring a registry keeps its identity and history, so old lockfiles that reference historical revisions still resolve and its slugs cannot be taken over by an untrusted repository.
:::

---

### Managing license and rejected-resource policies

Open **Policies** and click **Edit policies** to change global license enforcement and the distribution of rejected resources.

For **Rejected resources**, choose **Block publication** (the default) or **Expose as operator-approved**. Switching to exposure requires confirming that rejected resources from every registry that inherits the global policy may become downloadable and installable. Their reject assessment and findings stay visible, and revoked resources stay blocked.

#### Global license policy

- **License allowlist**: approved SPDX license identifiers, one per line (for example `MIT`, `Apache-2.0`, `BSD-3-Clause`). An empty allowlist turns off license enforcement, and the editor asks you to confirm that before you can continue.
- **Undeclared license**: what happens when a resource declares no license:
  - `reject`: the resource is blocked from publication (default).
  - `warn`: the resource stays installable and a warning finding is recorded.
  - `allow`: the resource is accepted without a warning.

#### When policy changes take effect

The Marketplace evaluates license policy when it serves a resource, so a stricter policy blocks non-compliant resources right away, without waiting for a reindex.

A license policy change also queues a reindex when at least one registry is active. A looser policy makes previously blocked resources available once that indexing has published them. Changing only the rejected-resource setting takes effect immediately and queues no indexing.

#### Per-registry overrides

In the registry editor, select **Override the inherited license policy for this registry** to set a registry-specific allowlist and undeclared-license outcome. A nonempty override allowlist replaces the global one, and an empty override is rejected.

**Rejected resource availability** offers **Inherit global policy**, **Block**, or **Expose as operator-approved**. Changing only this setting takes effect immediately and does not queue a reindex. The Activity log records the change.

---

### Auditing and restoring settings (Activity)

The **Activity** page lists every configuration change and operational request, newest first. The history is append-only. Each entry shows:

- the operation and the revision change, for example `Revision 12 → 13`;
- the subject of the operator who made it and when;
- the reason the operator gave;
- the indexing jobs the change queued, with their status;
- under **Settings at this revision**, the complete managed configuration as JSON.

#### Restoring historical settings

To roll back a configuration change:

1. Find the revision you want in **Activity**.
2. Under **Restore**, click **Policy settings**, or the button named after a registry's slug (for example **team-skills settings**). A registry's **Indexing history** tab also offers **Restore settings from revision N**.
3. The admin panel loads the historical values into a new draft against the current configuration revision.
4. Review the diff, enter a reason for the rollback, and apply the change.

::: info Restores do not erase history
Restoring earlier settings creates a new revision (for example `r14` with the settings of `r11`) and a new audit entry. It never overwrites or deletes history, and it keeps each registry's current lifecycle state, aliases, historical origins, and non-license overrides.
:::

---

## 5. Troubleshooting

### Common error codes

| Error code | Cause | Fix |
|---|---|---|
| `unauthenticated` (401) | Trusted-proxy mode: identity headers are missing or malformed, or `groups_header` is not a JSON array. OIDC mode: the session is missing or expired. | Check the reverse proxy logs and that the groups header looks like `["group-name"]`. In OIDC mode, sign in again. |
| `forbidden` (403) | The request did not come from an address in `admin.trusted_proxies`, or the user matches neither `admin.admin_groups` nor `admin.admin_users`. | Check that the immediate proxy address matches a configured CIDR and that the user belongs to an admin group or is listed as an admin user. |
| `origin_rejected` (403) | The request's `Origin` header does not match `admin.origin`. | Send requests from the configured origin, not an alternative host name or IP address. |
| `request_header_required` (403) | A mutating request (any method other than `GET` or `HEAD`) lacks the `X-Marketplace-Admin: 1` header. | Use the admin panel. Custom scripts must send this header, `Content-Type: application/json`, and a matching `Origin` on every mutating request. |
| `precondition_required` (428) | A change was submitted without an `If-Match` revision. | Read the current configuration first and send its revision in `If-Match`. |
| `revision_conflict` (409) | Another operator changed the configuration while your draft was open. | Click **Load latest revision and keep my draft**, review the new diff, and apply again. |
| `rate_limited` (429) | Too many admin requests or previews in one minute. | Wait for the time in the `Retry-After` header and try again. |
| `managed_configuration_required` (503) | `admin.enabled` is `true` but `management.mode` is not `database`. | Set `management.mode: database` and complete adoption. |
| `admin_unavailable` (503) | The `admin` block is incomplete or unsafe, for example an HTTP origin without `allow_insecure_localhost` or a missing identity header. | Fix the `admin` block and restart the Marketplace. Public routes keep working meanwhile. |

### Disabling the admin panel

To turn off browser administration while keeping database management:

```yaml
admin:
  enabled: false
```

The Marketplace reads this setting at startup. After a restart, requests under `/api/v1/admin/` return `404`. The Marketplace stays in database authority mode: the registries and policies managed in the database remain in effect, and it does not fall back to file-managed configuration.

## Related documentation

- [Marketplace operator configuration reference](../../reference/marketplace-operator-configuration.md)
- [How to configure a reverse proxy](./configure-a-reverse-proxy.md)
- [How to deploy a production Marketplace](./deploy-a-production-marketplace.md)
- [How to back up and restore a Marketplace](./back-up-and-restore.md)
- [How to manage ingestion policy](./manage-ingestion-policy.md)
- [How to reindex and monitor a Marketplace](./reindex-and-monitor.md)
