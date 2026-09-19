# How to enable and use the Marketplace admin panel

Enable the database-managed admin panel and manage registries, lifecycle states, license policies, and audit history from a browser.

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
- `admin.origin`: the exact HTTPS origin of the frontend. Cross-origin mutations are strictly rejected.
- `admin.trusted_proxies`: CIDR blocks of immediate reverse proxies allowed to assert identity headers. Forwarded client IPs (`X-Forwarded-For`) do not bypass this check.
- `admin.subject_header`: header containing the authenticated user identity (e.g. username or email).
- `admin.groups_header`: header containing a JSON string array of group memberships, such as `["marketplace-admins", "developers"]`. Comma-separated strings are rejected.
- `admin.admin_groups`: list of group names authorized to access the admin panel.

---

## 2. Configure the reverse proxy

The Marketplace API does not handle user login, passwords, or session cookies directly. Instead, it trusts identity headers asserted by your authenticated reverse proxy or SSO gateway.

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
3. Review the proposal diff, provide a mandatory **Reason** for the audit log, and click **Apply change**.

#### The Edit → Validate → Review → Apply workflow

All admin mutations follow an explicit validation flow:

1. **Edit:** make changes in page memory. Drafts remain in your browser session and are not autosaved to the server.
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

### Managing license policies

Navigate to the **Policies** section to manage global and per-registry license enforcement.

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

In the registry editor, you can override the global allowlist and undeclared license behavior for specific registries. A registry without overrides inherits the global policy.

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
