# Marketplace operator configuration reference

The Marketplace operator configuration defines one hosted Marketplace instance, its public and operations listeners, persistence backends, indexing policy, approved registries, and revocations. It is separate from the layered consumer configuration described in [Repository configuration reference](configuration.md).

## Scope and location

- **File location:** supplied to `agent-marketplace serve --config <path>`
- **Format:** YAML
- **Schema version:** `agent-marketplace.io/marketplace/v1`
- **Unknown fields:** rejected

## Complete example

```yaml
apiVersion: agent-marketplace.io/marketplace/v1
instance:
  id: team-marketplace
  name: Team Agent Marketplace
  canonical_url: https://marketplace.example.com
  browser_base: https://marketplace.example.com
listen: 0.0.0.0:8080
operations_listen: 127.0.0.1:9090
storage:
  metadata:
    driver: sqlite
    dsn: /var/lib/agent-marketplace/marketplace.db
  blobs:
    driver: filesystem
    path: /var/lib/agent-marketplace/blobs
acquisition_default: mirror
refresh:
  interval: 30m
refresh_concurrency: 1
policy:
  hidden_content: warn
  license_allowlist: [MIT, Apache-2.0, BSD-2-Clause, BSD-3-Clause]
  license_undeclared: reject
  limits:
    repository_bytes: 104857600
    file_count: 1024
    file_bytes: 2097152
    artifact_bytes: 16777216
    clone_duration: 5m
    ingestion_duration: 2m
registries:
  - id: shared-agents
    slug: shared-agents
    aliases: [agents]
    origin: https://github.com/example/shared-agents.git
    ref: main
    acquisition: mirror
    publisher:
      name: Example Engineering
      url: https://engineering.example.com
    license: Apache-2.0
    roots:
      - path: .
        source_harness: claude
    credentials: {}
    policy_overrides: {}
revocations:
  registry_ids: []
  coordinates: []
  revisions: []
management:
  mode: file
  allowed_origins:
    - https://github.com
  profiles: {}
admin:
  enabled: false
  origin: https://marketplace.example.com
  authentication:
    mode: oidc
    oidc:
      issuer: https://idp.example.com/oauth2/default
      client_id: marketplace-admin-client-id
      session_secret_env: MARKETPLACE_ADMIN_SESSION_SECRET
      groups_claim: groups
      session_ttl: 12h
  admin_groups: []
  admin_users: []
```

## Top-level fields

| Field | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `apiVersion` | string | yes | none | Must equal `agent-marketplace.io/marketplace/v1`. |
| `instance` | mapping | yes | none | Public identity and URLs for the deployment. |
| `listen` | host:port | no | `127.0.0.1:8080` | Public API listener. |
| `operations_listen` | host:port | no | `127.0.0.1:9090` | Private readiness and metrics listener. |
| `storage` | mapping | yes | none | Metadata and artifact persistence backends. |
| `acquisition_default` | string | no | `mirror` | Default acquisition mode for registries that omit `acquisition`. |
| `refresh` | mapping | no | interval `30m` | Background registry refresh schedule. |
| `refresh_concurrency` | integer | no | `1` | Maximum concurrent refresh work. Must not be negative; `0` resolves to `1`. |
| `policy` | mapping | no | conservative defaults | Global ingestion policy and resource limits. |
| `registries` | list | no | empty | Approved registry repositories. IDs and slugs must each be unique. |
| `revocations` | mapping | no | empty lists | Registry-, coordinate-, and exact-revision revocations. |
| `management` | mapping | no | mode `file` | Authority mode, allowed Git origins, and credential profiles. |
| `admin` | mapping | no | disabled | Browser administration settings (native OIDC or trusted-proxy identity). |

## `instance`

| Field | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `id` | string | yes | none | Stable Marketplace identity returned by the well-known endpoint. |
| `name` | string | yes | none | Human-readable deployment name. |
| `canonical_url` | HTTP(S) URL | yes | none | Public API base URL. User information, queries, fragments, and path traversal are rejected. |
| `browser_base` | HTTP(S) URL | no | none | Public browser base used to construct resource links. Subject to the same public-URL validation. |

## `storage`

### Metadata

| Field | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `driver` | string | yes | none | `sqlite` or `postgres`. |
| `dsn` | string | exactly one DSN source | none | Literal DSN. Avoids indirection but can expose credentials in configuration. |
| `dsn_env` | environment variable name | exactly one DSN source | none | Reads the DSN from the named non-empty environment variable. |
| `dsn_file` | path | exactly one DSN source | none | Reads the DSN from a non-empty file. |

Exactly one of `dsn`, `dsn_env`, and `dsn_file` must resolve to a usable value. SQLite requires a writable filesystem database; in-memory, read-only, and immutable modes are rejected. PostgreSQL accepts PostgreSQL URLs and key-value DSNs.

### Blobs

| Field | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `driver` | string | yes | none | `filesystem` or `s3`. |
| `path` | path | for `filesystem` | none | Root for digest-addressed artifact blobs. |
| `bucket` | string | for `s3` | none | S3 bucket name. |
| `region` | string | for `s3` | none | S3 region. |
| `endpoint` | URL | no | SDK default | Optional S3-compatible endpoint. |

## `refresh`

| Field | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `interval` | Go duration or integer nanoseconds | no | `30m` | Positive delay between background refresh cycles. |

## `policy`

| Field | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `hidden_content` | string | no | `warn` | `reject` or `warn`. Controls the outcome for hidden repository content. |
| `license_allowlist` | list of SPDX-like strings | no | `MIT`, `Apache-2.0`, `BSD-2-Clause`, `BSD-3-Clause` | Allowed declared licenses. An empty list disables license enforcement. |
| `license_undeclared` | string | no | `reject` | `reject`, `warn`, or `allow` when the allowlist is non-empty and a resource declares no license. |
| `limits` | mapping | no | values below | Positive acquisition and ingestion bounds. |

### `policy.limits`

| Field | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `repository_bytes` | integer bytes | no | `104857600` | Maximum acquired repository size. |
| `file_count` | integer | no | `1024` | Maximum files considered during ingestion. |
| `file_bytes` | integer bytes | no | `2097152` | Maximum size of one ingested file. |
| `artifact_bytes` | integer bytes | no | `16777216` | Maximum published artifact and digest-addressed manifest size. |
| `clone_duration` | duration | no | `5m` | Maximum registry clone duration. |
| `ingestion_duration` | duration | no | `2m` | Maximum ingestion duration. |

## `registries[]`

| Field | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `id` | string | yes | none | Immutable registry identity. Must be unique. |
| `slug` | lowercase slug | yes | none | Route-facing identifier matching `^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`. Must be unique. |
| `aliases` | list of strings | no | empty | Alternate registry names retained as metadata. |
| `origin` | public Git URL | yes | none | Repository acquisition URL. |
| `ref` | string | yes | none | Branch, tag, or commit to refresh. |
| `ca_file` | absolute path | no | system trust | PEM CA bundle for this registry. It must be a readable regular file and not a symlink. |
| `acquisition` | string | no | `acquisition_default` | `mirror` publishes Marketplace-hosted artifacts; `index-only` publishes verified source occurrences acquired by consumers from the registry origin. |
| `publisher` | mapping | no | empty | Publisher name and validated public URL. |
| `license` | string | no | empty | Registry-level declared license. A configured value must be in the global allowlist when that allowlist is non-empty. |
| `roots` | list | yes | none | One or more relative ingestion roots. |
| `credentials` | mapping | no | empty | Server-side credential references. Inline secret values are rejected. |
| `policy_overrides` | mapping | no | empty | Registry-specific policy values interpreted by the ingestion worker. |

### `registries[].roots[]`

| Field | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `path` | relative path | no | `.` | Ingestion root. Absolute paths and `..` are rejected. Roots are recursively ingested. |
| `source_harness` | string | no | autodetect | `factory`, `claude`, `cursor`, `windsurf`, `gemini`, or `devin`. |

### `registries[].publisher`

| Field | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `name` | string | no | empty | Publisher display name. |
| `url` | HTTP(S) URL | no | empty | Public publisher URL. |

### `registries[].credentials`

Only references are accepted; inline `value` fields are rejected.

```yaml
credentials:
  ssh_key:
    file: /run/secrets/registry-ssh-key
```

```yaml
credentials:
  token:
    file: /run/secrets/registry-token
```

A token requires a non-empty `file` or `env` reference. `ssh_key` and `token` are mutually exclusive.

### `registries[].policy_overrides`

Registry overrides accept the same `hidden_content`, `license_allowlist`, `license_undeclared`, and nested `limits` shapes as the global policy. Recognized values replace the corresponding global value for that registry. Missing, malformed, and unrecognized values retain the global value.

```yaml
policy_overrides:
  hidden_content: reject
  license_undeclared: warn
  limits:
    repository_bytes: 52428800
```

## `revocations`

| Field | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `registry_ids` | list of strings | no | empty | Revokes every resource from each registry ID. Values cannot contain `/` or `@`. |
| `coordinates` | list of coordinates | no | empty | Revokes all revisions of `marketplace/registry/kind/name`. The Marketplace component must equal `instance.id`. |
| `revisions` | list of exact targets | no | empty | Revokes `marketplace/registry/kind/name@sha256:<64 lowercase hex>`. The Marketplace component must equal `instance.id`. |

Supported resource kinds are `skill`, `command`, `subagent`, `hook`, `mcp`, and `collection`.

## `management`

Controls whether catalog settings (registries, lifecycle states, and license policies) are managed by operator YAML or dynamically through the metadata database.

| Field | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `mode` | string | no | `file` | Authority mode: `file` or `database`. In `database` mode, startup requires previous maintenance adoption. |
| `allowed_origins` | list of URLs | for `database` mode | empty | Allowed Git origin authorities for admin-managed registries. Must contain protocol (`https` or `ssh`) and host/port only (no path or query). |
| `profiles` | mapping | no | empty | Map of opaque credential and trust profile definitions referenced by registries. |

### `management.profiles`

Profiles bind credentials or custom CA bundles to allowed origin authorities. The admin panel references profiles by their key (e.g. `internal-git`) without exposing sensitive server paths or credentials to the browser.

```yaml
management:
  mode: database
  allowed_origins:
    - https://github.com
    - https://gitlab.example.com
  profiles:
    gitlab-ci:
      allowed_origins:
        - https://gitlab.example.com
      credentials:
        token:
          file: /run/secrets/gitlab-token
      ca_file: /etc/agent-marketplace/ca/internal.pem
```

| Field | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `allowed_origins` | list of URLs | yes | none | Git origin authorities permitted to use this profile. |
| `credentials` | mapping | no | empty | SSH key or token credential reference (`file` or `env`). |
| `ca_file` | absolute path | no | none | Path to a PEM CA certificate bundle for registries using this profile. |

## `admin`

Configures the browser-based administration API and its authentication mode. Exactly one mode is selected through `authentication.mode`; an enabled configuration with no explicit mode and a complete trusted-proxy field set infers `trusted_proxy` for backward compatibility. The modes are mutually exclusive: OIDC mode rejects trusted-proxy identity fields and trusted-proxy mode rejects `authentication.oidc` fields.

| Field | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `enabled` | boolean | no | `false` | Enables the `/api/v1/admin/**` API endpoints. |
| `origin` | URL | if enabled | none | Expected HTTPS origin for admin requests (e.g. `https://marketplace.example.com`). Must equal the exact browser origin serialization: lowercase scheme, ASCII DNS host (use the Punycode A-label), or literal IP host; no explicit default port (`:443` for HTTPS or `:80` for HTTP), no leading-zero port, no trailing-dot DNS host, no non-canonical IPv4 spelling (decimal, hex, dotted octal/hex/leading-zero forms, or IPv4-mapped IPv6 such as `[::ffff:192.0.2.1]`), and no path, query, or fragment. Non-default ports are valid. HTTP is permitted only by the localhost opt-in below. |
| `allow_insecure_localhost` | boolean | no | `false` | Allows HTTP origins only for `localhost` or literal loopback IP addresses, for local development. Does not relax identity, proxy, or exact-origin checks. The operator must bind the public proxy to loopback; this flag does not change listener bindings. |
| `admin_groups` | list of strings | one of `admin_groups`/`admin_users` required | empty | Group names authorized to access administration endpoints. |
| `admin_users` | list of strings | one of `admin_groups`/`admin_users` required | empty | Immutable provider subjects authorized to access administration endpoints. Never an email or display name. |
| `authentication.mode` | string | yes for new configurations | inferred | `oidc` for native sign-in or `trusted_proxy` for identity asserted by a reverse proxy. |
| `trusted_proxies` | list of CIDRs | trusted-proxy mode | empty | Immediate reverse proxy CIDRs allowed to assert identity headers. Forwarded client IPs do not bypass this check. |
| `issuer` | string | trusted-proxy mode | none | Identity provider or SSO issuer identifier recorded as the audit issuer. |
| `subject_header` | string | trusted-proxy mode | none | HTTP header containing the authenticated subject identity (e.g. `X-Marketplace-Subject`). |
| `groups_header` | string | trusted-proxy mode | none | HTTP header containing a JSON string array of group names (e.g. `X-Marketplace-Groups`). |

Identity header names must be distinct (case-insensitively) and match `[A-Za-z][A-Za-z0-9-]*`. `X-Forwarded-*` names and reserved HTTP headers such as `Authorization`, `Cookie`, and `Origin` are rejected at startup. Use dedicated headers that the trusted proxy overwrites with authenticated claims.

```yaml
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

### `admin.authentication.oidc`

Native OIDC sign-in uses a dedicated public Authorization Code client with required S256 PKCE. The API performs lazy issuer discovery, validates RS256 ID tokens against the issuer JWKS with exact issuer and audience checks, requires a present `azp` claim to equal `client_id` and requires it whenever the token carries multiple audiences, and issues a signed `HttpOnly` `SameSite=Lax` session cookie. For HTTPS origins both cookies use the browser-enforced `__Host-` names `__Host-marketplace_admin_session` and `__Host-marketplace_admin_login` with `Secure`, `Path=/`, and no `Domain`, so a same-named sibling-domain cookie can never collide with them; this deliberately trades per-path confinement for host isolation. Only the explicit `allow_insecure_localhost` loopback HTTP mode keeps the unprefixed names `marketplace_admin_session` and `marketplace_admin_login` confined to `/api/v1/admin` and `/api/v1/admin/auth` respectively without `Secure`. Request lookup and expiration always use the effective mode-specific name and path. The callback URI is fixed to `<origin>/api/v1/admin/auth/callback` and is not separately configurable.

| Field | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `issuer` | HTTPS URL | yes | none | Exact OIDC issuer (e.g. `https://idp.example.com/oauth2/default`). No query, fragment, or trailing slash. HTTP is permitted only for a loopback issuer with `allow_insecure_localhost`. |
| `client_id` | string | yes | none | Public OIDC client ID. Not a secret; may be committed to deployment configuration. |
| `session_secret_env` | environment variable name | exactly one secret source | none | Environment variable holding the session signing secret. The resolved value is trimmed of surrounding whitespace and must be 32 through 4096 bytes. Inline secrets are not supported. |
| `session_secret_file` | path | exactly one secret source | none | File holding the session signing secret. The resolved value is trimmed of surrounding whitespace and must be 32 through 4096 bytes. |
| `groups_claim` | string | no | `groups` | ID-token claim carrying group membership. Must be a single safe claim name. |
| `session_ttl` | duration | no | `12h` | Session lifetime; must be non-negative and no greater than `24h`. A zero or omitted value applies the `12h` default. The transient login transaction lasts 10 minutes and is not configurable. |

Requested scopes are fixed to `openid profile email groups`; arbitrary scopes and offline access are not configurable. Provider tokens, authorization codes, and PKCE material are never stored in sessions, logs, or error responses. Invalid authentication configuration fails the admin routes closed without affecting public Marketplace routes.

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

## Server roles and commands

`agent-marketplace serve --config <path> --role <role>` accepts:

| Role | Behavior |
|---|---|
| `api` | Runs the public and operations HTTP listeners. |
| `worker` | Runs background indexing without the HTTP API. |
| `all` | Runs API and worker in one process. This is the default. |

The public listener exposes the well-known document, capability and instance metadata, search, registry/resource metadata, manifests, assessments, inspection content, artifacts, and `/healthz`. The operations listener exposes `/readyz` and `/metrics`. Public requests to `/readyz` and `/metrics` return 404.

### Maintenance adoption

`agent-marketplace serve adopt --config <path> --actor <id> --reason <text> --idempotency-key <key> --confirm-maintenance`

Transfers catalog configuration from the operator YAML file into the metadata database and establishes database authority.

- All API and worker processes must be stopped before running adoption.
- Full database and blob backups must be taken before adoption.
- Requires `management.mode: database` in the configuration file.
- See [How to enable and use the Marketplace admin panel](../how-to/operator/enable-and-use-the-admin-panel.md) for the complete procedure.

### Reindexing

`agent-marketplace serve reindex [registry-slug-or-id] --config <path>` processes one registry when an argument is present and every configured registry when omitted. In database management mode, it queries active managed registries from the database rather than the YAML file. It validates and applies revocations before acquisition and performs a readiness check after processing.
