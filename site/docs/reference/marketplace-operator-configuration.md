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

## Server roles and endpoints

`agent-marketplace serve --config <path> --role <role>` accepts:

| Role | Behavior |
|---|---|
| `api` | Runs the public and operations HTTP listeners. |
| `worker` | Runs background indexing without the HTTP API. |
| `all` | Runs API and worker in one process. This is the default. |

The public listener exposes the well-known document, capability and instance metadata, search, registry/resource metadata, manifests, assessments, inspection content, artifacts, and `/healthz`. The operations listener exposes `/readyz` and `/metrics`. Public requests to `/readyz` and `/metrics` return 404.

`agent-marketplace serve reindex [registry-slug-or-id] --config <path>` processes one registry when an argument is present and every configured registry when omitted. It validates and applies revocations before acquisition and performs a readiness check after processing.
