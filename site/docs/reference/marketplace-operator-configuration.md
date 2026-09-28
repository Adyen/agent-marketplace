# Marketplace operator configuration reference

This page lists every field of the file that configures one hosted Marketplace server. You need it if you run `agent-marketplace serve`. The file sets the instance identity, the public and operations listeners, where metadata and artifacts are stored, the ingestion policy, the approved registries, revocations, and the optional admin panel.

This file is separate from the consumer configuration that `install` and `sync` read. For that, see the [Repository configuration reference](configuration.md).

## Scope and location

- File location: the path you pass to `agent-marketplace serve --config <path>`
- Format: YAML
- Schema version: `agent-marketplace.io/marketplace/v1`
- Unknown fields: rejected at load time

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
  rejected_resources: block
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
catalog_projection:
  enabled: false
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
| `storage` | mapping | yes | none | Metadata and artifact storage backends. |
| `acquisition_default` | string | no | `mirror` | Acquisition mode for registries that omit `acquisition`. `mirror` or `index-only`. |
| `refresh` | mapping | no | interval `30m` | Background registry refresh schedule. |
| `refresh_concurrency` | integer | no | `1` | Maximum number of refreshes that run at the same time. Must not be negative; `0` means `1`. |
| `policy` | mapping | no | defaults below | Global ingestion policy and resource limits. |
| `registries` | list | no | empty | Approved registry repositories. IDs and slugs must each be unique. |
| `revocations` | mapping | no | empty lists | Registry, coordinate, and exact-revision revocations. |
| `catalog_projection` | mapping | no | disabled | Turns on the optional `GET /api/v1/catalog` route. |
| `management` | mapping | no | mode `file` | Where catalog settings live, allowed Git origins, and credential profiles. |
| `admin` | mapping | no | disabled | Browser administration settings (native OIDC or trusted-proxy identity). |

## `instance`

| Field | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `id` | string | yes | none | Stable Marketplace identity. The well-known endpoint returns it as `instance_id`, and consumers compare it on every use. |
| `name` | string | yes | none | Human-readable deployment name. |
| `canonical_url` | HTTP(S) URL | yes | none | Public API base URL. Must have a host. User information, fragments, query parameters that carry credentials, and `..` are rejected. |
| `browser_base` | HTTP(S) URL | no | none | Public browser base used to build resource links. The same URL checks apply. When set, the well-known document also publishes browser route templates. |

## `storage`

### Metadata

| Field | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `driver` | string | yes | none | `sqlite` or `postgres`. |
| `dsn` | string | exactly one DSN source | none | Literal DSN. Simple, but it puts any credentials in the file. |
| `dsn_env` | environment variable name | exactly one DSN source | none | Reads the DSN from the named environment variable, which must be set and non-empty. |
| `dsn_file` | path | exactly one DSN source | none | Reads the DSN from a file, which must be non-empty. |

Exactly one of `dsn`, `dsn_env`, and `dsn_file` must be set and resolve to a usable value. Surrounding whitespace is trimmed. Error messages never include the DSN.

SQLite needs a writable database file on disk. The server rejects `:memory:`, `file::memory:`, `mode=memory`, `mode=ro`, and `immutable=1`. PostgreSQL accepts `postgres://` or `postgresql://` URLs with a host, and key-value DSNs.

### Blobs

| Field | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `driver` | string | yes | none | `filesystem` or `s3`. |
| `path` | path | for `filesystem` | none | Root directory for artifact blobs, stored by digest. |
| `bucket` | string | for `s3` | none | S3 bucket name. |
| `region` | string | for `s3` | none | S3 region. |
| `endpoint` | URL | no | SDK default | Endpoint for an S3-compatible service. |

## `refresh`

| Field | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `interval` | Go duration string or integer nanoseconds | no | `30m` | Delay between background refresh cycles. Must be positive. |

## `policy`

| Field | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `hidden_content` | string | no | `warn` | `reject` or `warn`. Outcome for hidden repository content. |
| `license_allowlist` | list of SPDX-like strings | no | `MIT`, `Apache-2.0`, `BSD-2-Clause`, `BSD-3-Clause` | Allowed declared licenses. An explicit empty list turns license enforcement off. |
| `license_undeclared` | string | no | `reject` | `reject`, `warn`, or `allow`. Outcome for a resource that declares no license while the allowlist is non-empty. A value outside this set fails validation. |
| `rejected_resources` | string | no | `block` | `block` withholds rejected revisions. `expose` serves them with availability `operator-exposed` and keeps their reject assessment. Revocations and integrity failures always block. A value outside this set fails validation. |
| `limits` | mapping | no | values below | Acquisition and ingestion bounds. Every value must be positive. |

### `policy.limits`

| Field | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `repository_bytes` | integer bytes | no | `104857600` | Maximum acquired repository size. |
| `file_count` | integer | no | `1024` | Maximum number of files considered during ingestion. |
| `file_bytes` | integer bytes | no | `2097152` | Maximum size of one ingested file. The API also refuses to serve inspection content larger than this. |
| `artifact_bytes` | integer bytes | no | `16777216` | Maximum size of a published artifact and of a digest-addressed manifest. |
| `clone_duration` | duration | no | `5m` | Maximum time for one registry clone. |
| `ingestion_duration` | duration | no | `2m` | Maximum time for one ingestion. |

## `registries[]`

| Field | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `id` | string | yes | none | Permanent registry identity. Must be unique. |
| `slug` | lowercase slug | yes | none | Identifier used in routes and locators. Must match `^[a-z0-9]([a-z0-9-]*[a-z0-9])?$` and be unique. |
| `aliases` | list of strings | no | empty | Alternate registry names kept as metadata. |
| `origin` | HTTP(S) Git URL | yes | none | Repository URL to clone. Must have a host. User information, fragments, and query parameters that carry credentials are rejected. |
| `ref` | string | yes | none | Branch, tag, or commit to refresh. |
| `ca_file` | absolute path | no | system trust | PEM CA bundle for this registry. Must be a readable regular file (not a symlink) that contains at least one valid certificate. |
| `acquisition` | string | no | `acquisition_default` | `mirror`: the Marketplace stores and serves the artifacts. `index-only`: the Marketplace publishes verified metadata and the exact commit, and consumers fetch the content from the registry origin. |
| `publisher` | mapping | no | empty | Publisher name and public URL. |
| `license` | string | no | empty | Registry-level declared license. When the global allowlist is non-empty, a configured value must be on it. |
| `roots` | list | yes | none | One or more relative ingestion roots. |
| `credentials` | mapping | no | empty | References to server-side credentials. Inline secret values are rejected. |
| `policy_overrides` | mapping | no | empty | Policy values for this registry only. |

### `registries[].roots[]`

| Field | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `path` | relative path | no | `.` | Ingestion root. Absolute paths and paths containing `..` are rejected. The worker ingests each root recursively. |
| `source_harness` | string | no | autodetect | `factory`, `claude`, `cursor`, `windsurf`, `gemini`, or `devin`. |

### `registries[].publisher`

| Field | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `name` | string | no | empty | Publisher display name. |
| `url` | HTTP(S) URL | no | empty | Public publisher URL. The same URL checks as `instance.canonical_url` apply. |

### `registries[].credentials`

The file accepts only references to secrets. An inline `value` field is rejected.

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

`ssh_key` requires a non-empty `file`. `token` requires a non-empty `file` or `env` reference. A registry can set `ssh_key` or `token`, not both.

### `registries[].policy_overrides`

Registry overrides accept `hidden_content`, `license_allowlist`, `license_undeclared`, `rejected_resources`, and a nested `limits` mapping with the same keys as `policy.limits`. Any key you leave out inherits the global policy.

`rejected_resources` must be `block` or `expose`; any other value makes the whole configuration invalid, so a typo can never turn on exposure. For the other keys, the worker ignores a malformed or unrecognized value and keeps the global value. An empty `license_allowlist` override also keeps the global list.

```yaml
policy_overrides:
  hidden_content: reject
  license_undeclared: warn
  rejected_resources: expose
  limits:
    repository_bytes: 52428800
```

## `revocations`

| Field | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `registry_ids` | list of strings | no | empty | Revokes every resource from each registry ID. Values cannot be empty or contain `/` or `@`. |
| `coordinates` | list of coordinates | no | empty | Revokes all revisions of `marketplace/registry/kind/name`. The Marketplace component must equal `instance.id`. |
| `revisions` | list of exact targets | no | empty | Revokes one revision, written `marketplace/registry/kind/name@sha256:<64 lowercase hex>`. The Marketplace component must equal `instance.id`. |

Valid resource kinds in coordinates are `skill`, `command`, `subagent`, `hook`, `mcpserver`, and `collection`.

`serve` and `serve reindex` write the configured revocations to the metadata store before any role starts or any registry is acquired. The API also applies them when it answers requests, and `/readyz` reports not ready until the stored set matches the file.

## `catalog_projection`

| Field | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `enabled` | boolean | no | `false` | Registers `GET /api/v1/catalog` and advertises the `catalog-projection-v1alpha1` capability feature. The projection includes source repository URLs, paths, and commits, so enable it only where that information may be exposed. |

## `management`

Controls whether catalog settings (registries, lifecycle states, and license policies) come from this YAML file or from the metadata database.

| Field | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `mode` | string | no | `file` | `file` or `database`. In `database` mode the API refuses to start, and public routes return `503`, until `serve adopt` has recorded database authority. A database that already has that authority also refuses a server started in `file` mode. |
| `allowed_origins` | list of URLs | in practice, for `database` mode | empty | Git origin authorities that admin-managed registries may use. Each entry is `https://` or `ssh://` plus a host and optional port, with no user information, path, query, or fragment. Adoption and the admin panel reject any registry origin outside this list. |
| `profiles` | mapping | no | empty | Named credential and trust profiles that registries reference. |

### `management.profiles`

A profile binds credentials or a custom CA bundle to a set of allowed origin authorities. The admin panel refers to a profile by its key (for example `gitlab-ci`), so the browser never sees server paths or secrets. A profile key must be non-empty, at most 128 characters, and must not contain `/`, `\`, spaces, line breaks, or NUL.

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
| `allowed_origins` | list of URLs | yes | none | Non-empty list of Git origin authorities that may use this profile. Same format as `management.allowed_origins`. |
| `credentials` | mapping | no | empty | SSH key or token reference, with the same rules as `registries[].credentials`. |
| `ca_file` | absolute path | no | none | PEM CA bundle for registries that use this profile. |

## `admin`

Configures the browser administration API and how it authenticates users. `authentication.mode` selects exactly one mode. For backward compatibility, an enabled configuration with no mode but a complete set of trusted-proxy fields is treated as `trusted_proxy`. The two modes cannot be mixed: OIDC mode rejects the trusted-proxy identity fields, and trusted-proxy mode rejects `authentication.oidc` fields.

The server loads the rest of the configuration even when the `admin` block is invalid. In that case the admin routes return `503` with code `admin_unavailable` and the public routes keep working. When `enabled` is `false`, every `/api/v1/admin/` route returns `404`.

| Field | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `enabled` | boolean | no | `false` | Turns on the `/api/v1/admin/` routes. |
| `origin` | URL | if enabled | none | Browser origin allowed to send admin requests, for example `https://marketplace.example.com`. It must be written exactly as a browser serializes it: lowercase scheme; an ASCII DNS host (use the Punycode A-label) or a literal IP address; no explicit default port (`:443` for HTTPS, `:80` for HTTP) and no leading zeros in the port; no trailing dot on the host; no non-canonical IPv4 spelling (decimal, hexadecimal, dotted octal or hex, leading zeros, or IPv4-mapped IPv6 such as `[::ffff:192.0.2.1]`); and no path, query, or fragment. Non-default ports are allowed. HTTP is accepted only with `allow_insecure_localhost`. |
| `allow_insecure_localhost` | boolean | no | `false` | Accepts HTTP origins for `localhost` or a literal loopback IP only, for local development. It does not relax identity, proxy, or exact-origin checks, and it does not change listener bindings; bind the public proxy to loopback yourself. |
| `admin_groups` | list of strings | at least one of `admin_groups` or `admin_users` | empty | Group names allowed to use the admin routes. |
| `admin_users` | list of strings | at least one of `admin_groups` or `admin_users` | empty | Permanent identity-provider subjects allowed to use the admin routes. Use the subject, never an email address or display name. |
| `authentication.mode` | string | yes for new configurations | inferred | `oidc` for native sign-in, or `trusted_proxy` for identity asserted by a reverse proxy. |
| `trusted_proxies` | list of CIDRs | trusted-proxy mode | empty | CIDRs of the reverse proxies that connect directly to the server and may send identity headers. A `/0` prefix or an IPv4-mapped IPv6 prefix is rejected. Forwarded client IP headers cannot bypass this check. |
| `issuer` | string | trusted-proxy mode | none | Identity provider or SSO issuer, recorded as the audit issuer. |
| `subject_header` | string | trusted-proxy mode | none | HTTP header that carries the authenticated subject (for example `X-Marketplace-Subject`). |
| `groups_header` | string | trusted-proxy mode | none | HTTP header that carries a JSON array of group names (for example `X-Marketplace-Groups`). |

The two identity header names must differ (ignoring case) and match `[A-Za-z][A-Za-z0-9-]*`. The server rejects `X-Forwarded-*` names and reserved headers such as `Authorization`, `Cookie`, and `Origin`. Use dedicated headers that the trusted proxy always overwrites with authenticated claims.

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

Native OIDC sign-in uses a dedicated public client with the Authorization Code flow and required S256 PKCE. The server discovers the issuer on first use and accepts only RS256 ID tokens signed by keys from the issuer's JWKS, with exact issuer and audience checks. If the token has an `azp` claim, it must equal `client_id`; a token with more than one audience must have one.

After sign-in the server sets a signed session cookie that is `HttpOnly` and `SameSite=Lax`. On HTTPS origins, the session and login cookies are named `__Host-marketplace_admin_session` and `__Host-marketplace_admin_login` and are set with `Secure`, `Path=/`, and no `Domain`. Browsers enforce the `__Host-` prefix, so a cookie with the same name from a sibling domain cannot collide with them. The cost is that the cookies are not limited to the admin paths. Only in `allow_insecure_localhost` loopback HTTP mode do the cookies use the unprefixed names `marketplace_admin_session` and `marketplace_admin_login`, limited to `/api/v1/admin` and `/api/v1/admin/auth` respectively, without `Secure`. Reading and expiring a cookie always uses the name and path for the active mode.

The callback URI is always `<origin>/api/v1/admin/auth/callback` and cannot be configured.

| Field | Type | Required | Default | Description |
|---|---|:---:|---|---|
| `issuer` | HTTPS URL | yes | none | Exact OIDC issuer (for example `https://idp.example.com/oauth2/default`). No query, fragment, or trailing slash. HTTP is accepted only for a loopback issuer with `allow_insecure_localhost`. |
| `client_id` | string | yes | none | Public OIDC client ID. It is not a secret and may be committed with the deployment configuration. |
| `session_secret_env` | environment variable name | exactly one secret source | none | Environment variable that holds the session signing secret. The value is trimmed of surrounding whitespace and must be 32 to 4096 bytes. Inline secrets are not supported. |
| `session_secret_file` | path | exactly one secret source | none | Regular file that holds the session signing secret. The value is trimmed of surrounding whitespace and must be 32 to 4096 bytes. |
| `groups_claim` | string | no | `groups` | ID-token claim that lists group membership. Must be a single claim name matching `^[A-Za-z_][A-Za-z0-9_-]{0,127}$`. |
| `session_ttl` | duration | no | `12h` | Session lifetime. Must be between zero and `24h`; zero or omitted means `12h`. The sign-in transaction itself lasts 10 minutes, which is not configurable. |

The requested scopes are always `openid profile email groups`; you cannot add scopes or request offline access. Sessions, logs, and error responses never contain provider tokens, authorization codes, or PKCE material.

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

`agent-marketplace serve --config <path> --role <role>` accepts these roles:

| Role | Behavior |
|---|---|
| `api` | Runs the public and operations HTTP listeners. |
| `worker` | Runs background indexing without the HTTP API. |
| `all` | Runs the API and the worker in one process. This is the default. |

The public listener serves the well-known document, capability and instance metadata, search, registry and resource metadata, manifests, assessments, inspection content, artifacts, the optional catalog projection, the admin routes, and `/healthz`. The operations listener serves `/readyz` and `/metrics`. On the public listener, `/readyz` and `/metrics` return `404`. See the [Marketplace HTTP API reference](marketplace-http-api.md) for every route.

### Maintenance adoption

```bash
agent-marketplace serve adopt --config <path> --actor <id> --reason <text> --idempotency-key <key> --confirm-maintenance
```

Adoption validates the catalog settings in the YAML file, completes the blob ownership backfill, copies the settings into the metadata database, and records database authority.

- All five flags are required. Without `--confirm-maintenance` the command refuses to run.
- The configuration must set `management.mode: database`.
- Stop every API and worker process first. The command does not check this itself.
- Take full backups of the database and blobs first, and clear any shared caches you control.
- Every registry origin must be covered by `management.allowed_origins`. A registry that sets `credentials` or `ca_file` must match exactly one profile with identical credentials and CA file whose `allowed_origins` also covers the origin.
- Reuse the same `--idempotency-key` if you retry an adoption.

See [How to enable and use the Marketplace admin panel](../how-to/operator/enable-and-use-the-admin-panel.md) for the full procedure.

### Reindexing

```bash
agent-marketplace serve reindex [registry-slug-or-id] --config <path>
```

With an argument, the command processes that one registry; the argument can be a slug or an ID. Without one, it processes every configured registry. In `database` management mode it reads the registries from the database instead of the YAML file, and without an argument it processes only active registries.

The command writes the configured revocations before it acquires anything. After processing, it checks that every published artifact has a matching blob in the blob store. It stops after one hour and exits non-zero if any registry or the integrity check failed.
