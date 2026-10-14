# Marketplace HTTP API reference

This page lists the HTTP routes that a hosted Marketplace serves, with their parameters, responses, and status codes. It is for operators who put a proxy in front of the server and for developers who call the API directly. The consumer CLI uses the same public routes.

The machine-readable contract is the [OpenAPI document](https://github.com/Adyen/agent-marketplace/blob/main/api/openapi.yaml). It has the full response schemas.

## Listeners

- Public listener: the `listen` address in the [operator configuration](marketplace-operator-configuration.md). It serves the public API, the well-known document, `/healthz`, and the admin routes.
- Operations listener: the `operations_listen` address. It serves only `/readyz` and `/metrics`. Keep it on a private network or loopback.

## Common behavior

Public and operations routes accept only `GET` and `HEAD`. Any other method returns `405` with `{"error":"method not allowed"}`. The admin routes have their own method rules, described in [Administration routes](#administration-routes).

Errors that public and operations route handlers return are JSON objects with one field. A path that matches no route gets the Go router's plain-text `404` instead.

```json
{"error": "registry not found"}
```

| Status | When |
|---|---|
| `400` | A malformed parameter: an unknown `kind` or `sort`, a digest or commit in the wrong form, a bad `limit` or `offset`, or a search query longer than 256 characters. |
| `403` | The content is withheld because its current assessment rejects it. |
| `404` | The registry, resource, revision, or object does not exist or is not available. |
| `405` | The method is not `GET` or `HEAD`. |
| `410` | The content is revoked. |
| `413` | The stored object is larger than the configured limit. |
| `415` | The stored object is not a supported manifest or content type. |
| `500` | Stored data failed a validation or digest check, or a store read failed. |
| `503` | Configuration authority or availability data cannot be read (see below). |

Every public route except `/healthz` first checks configuration authority (see `management.mode`). If that check fails, the route returns `503` with `{"error":"configuration authority unavailable"}` and `Cache-Control: private, no-store`. In `database` management mode, public routes other than `/healthz` and the admin routes also load current availability; if that fails they return `503` with `{"error":"availability unavailable"}`.

Digest-addressed routes (assessments, inspection manifests, inspection content, manifests, and artifacts) return `Cache-Control: public, max-age=31536000, immutable` and `X-Content-Type-Options: nosniff`.

## Public routes

### Well-known

- `GET /.well-known/agent-marketplace.json`: instance identity and capabilities.

The response has `instance_id`, `version`, `canonical_url`, `api_base` (always `/api/v1`), and the capability fields listed under `/api/v1/capabilities`. When `instance.browser_base` is set, it also has `browser_base`, `browser_latest_route_template` (`<browser_base>/r/{registry}/{kind}/{name}`), and `browser_exact_route_template` (`<browser_base>/r/{registry}/{kind}/{name}/revisions/{revision}`).

### Capabilities and instance

- `GET /api/v1/capabilities`: server capabilities. Fields: `api_versions` (`["v1"]`), `artifact_schema_versions` (`["v1"]`), `resource_kinds` (`skill`, `command`, `subagent`, `hook`, `mcpserver`, `collection`), `acquisition_modes` (`index-only`, `mirror`), and `features`. `features` contains `inspection`, plus `catalog-projection-v1alpha1` when `catalog_projection.enabled` is `true`.
- `GET /api/v1/instance`: stored instance metadata plus the server `version`. Returns `404` if the instance row does not exist yet.

### Search and registries

- `GET /api/v1/search`: search the latest non-revoked revision of each resource. Query parameters:
  - `q`: search text, at most 256 characters.
  - `kind`: one of the resource kinds.
  - `registry`: registry filter; repeat it to filter by several registries.
  - `limit`: default `100`. Values above `1000` are reduced to `1000`. Negative or non-numeric values return `400`.
  - `offset`: default `0`. Negative or non-numeric values return `400`.
  - `sort`: optional; the only accepted value is `installations`. Any other value returns `400`. Requests that omit `sort` keep the existing relevance and coordinate ordering.

  The response is `{"items": [...], "total": <n>, "offset": <n>, "limit": <n>}`. Each item has `registry_id`, `registry_slug`, `kind`, `name`, `revision`, `content_digest`, and, when present, `description`, `availability`, `metadata` (for example `license`), and `score`.

  With `sort=installations` and a configured `installation_metrics` endpoint, text searches keep relevance ordering first and then rank ties by reported adoption; browse (no `q`) orders by adoption. Items then carry an optional `installation_count`: the number of distinct installations whose latest enabled configuration within the last 30 days reports the resource. Counts cover only telemetry-reporting installations matched by Git identity or explicit coordinates for this Marketplace instance; hosted observations use canonical resource names rather than installed-name aliases, and non-reporting installations or older clients can cause undercount; the aggregate is refreshed in the background and bounded by the configured `installation_metrics.max_age` (five minutes by default), with an earlier registry-origin change or 30-day inventory horizon expiring it sooner. Searches only read the published aggregate and never perform or wait for telemetry; a miss may leave a non-blocking hint that asks the background worker to refresh sooner without bypassing failure backoff. Metrics requests are POSTs to `/api/v1/query` with a bounded timeout (two seconds by default), an 8 MiB response bound, and no redirect following, so credentials never leave the configured endpoint. A failed refresh keeps serving a still-fresh, identity-compatible last-good aggregate until its original expiry. When metrics are disabled or no usable aggregate is published — before the first successful refresh, after expiry, or after a registry identity change — ordering falls back to the existing behavior and `installation_count` is omitted rather than reported as zero.
- `GET /api/v1/registries`: registry identifiers as an array of `{"id": ..., "slug": ...}`, sorted by slug, at most 1000 entries.
- `GET /api/v1/registries/{registry}`: public registry metadata. `{registry}` is a slug or an ID. Credentials and refresh diagnostics are never included.
- `GET /api/v1/registries/{registry}/history`: registry occurrence history, newest first, at most 1000 entries. Returns `[]` when there is no history.
- `GET /api/v1/registries/{registry}/occurrences/{commit}`: one registry occurrence at an exact commit. `{commit}` is a full 40-character lowercase hexadecimal Git commit; any other form returns `400`. The response includes the current approved `acquisition_origin`, which index-only consumers clone.

### Resources

- `GET /api/v1/resources/{registry}/{kind}/{name}`: latest revision of a resource.
- `GET /api/v1/resources/{registry}/{kind}/{name}/revisions`: all revisions of a resource.
- `GET /api/v1/resources/{registry}/{kind}/{name}/revisions/{revision}`: metadata for one exact revision.
- `GET /api/v1/resources/{registry}/{kind}/{name}/revisions/{revision}/inspection`: immutable inspection manifest for the revision.
- `GET /api/v1/resources/{registry}/{kind}/{name}/revisions/{revision}/inspection/content`: the revision's primary inspection file.
- `GET /api/v1/resources/{registry}/{kind}/{name}/revisions/{revision}/inspection/content/{digest}`: one allowlisted inspection file.

`{kind}` is one of `skill`, `command`, `subagent`, `hook`, `mcpserver`, or `collection`. MCP servers use `mcpserver` here, while the CLI `--kind` flag uses `mcp`. `{revision}` and the inspection file `{digest}` use the form `sha256:<64 lowercase hex>`.

Resource and revision responses include a `current_availability` object. Its `availability` value is one of:

| Value | Meaning |
|---|---|
| `available` | The revision can be installed from this instance. |
| `operator-exposed` | The assessment rejected the revision, but the operator set `rejected_resources: expose`. |
| `policy-blocked` | The current assessment rejected the revision. |
| `no-artifact` | A mirrored revision has no artifact this instance can serve. |
| `revoked` | The operator revoked the revision. |
| `blocked` | The revision is listed but not installable for another reason. |

In `database` management mode, a resource route for a revision that is not `available` or `operator-exposed` returns `403` (or `410` when revoked) with an `error` field and the `current_availability` object.

The inspection routes return `404` when no inspection exists and `413` for inspection files larger than `policy.limits.file_bytes`. In `database` management mode they also return `410` for revoked content and `403` for blocked content.

The latest-revision resource response also carries prerequisite relations recorded at publication:

- `requires`: the revision's `metadata.requires` declarations in declaration order, each `{"kind", "name", "registry"?, "path"?, "resolved"?}` with `registry` and `path` credential-redacted before storage. `resolved` names the visible current-head resource (`{"registry", "kind", "name"}`, registry by slug) that satisfies the declaration within the same registry or the hinted registry, including the `path` source-root rule; it is omitted when no candidate qualifies. Revisions published before prerequisite indexing omit `requires`; an indexed revision with no prerequisites reports `[]`.
- `required_by`: the in-scope dependents, meaning the visible current-head resources of the same Marketplace, counting a dependent in another registry only when that registry is listed, whose indexed current-head revisions declare this resource as a prerequisite, ordered by (`registry`, `kind`, `name`); omitted when a visible in-scope unindexed revision leaves the dependent set unknowable, and `[]` when there are no dependents.

### Catalog projection

This route exists only when `catalog_projection.enabled` is `true`. Otherwise it returns `404`.

- `GET /api/v1/catalog`: one page of the current catalog, ordered by resource coordinate.
  - `limit`: `1` to `500`, default `500`. Other values return `400`.
  - `after`: the opaque `next_cursor` from the previous page. Do not build or inspect it.
  - `If-None-Match` header: accepted only on the first page (without `after`). A match returns `304` with no body.
  - Any other query parameter returns `400`.

The response has `api_version` (`agent-marketplace.io/catalog/v1alpha1`), `marketplace_id`, `catalog_version`, `complete`, `next_cursor` (absent on the last page), and `resources`. Keep requesting pages until `complete` is `true`. Every page carries a strong `ETag` for the whole catalog and `Cache-Control: no-cache`. A scan is not a consistent snapshot: compare the `ETag` of the first and last pages and scan again if it changed.

### Manifests and digest-addressed objects

- `GET /api/v1/manifests/{registry-revision}`: registry revision manifest or collection manifest. `{registry-revision}` is `sha256:<64 lowercase hex>`. Returns `410` when revoked, `403` when blocked by assessment, and `415` if the stored object is not a manifest.
- `GET /api/v1/assessments/sha256/{digest}`: immutable resource assessment.
- `GET /api/v1/inspection/sha256/{digest}`: immutable inspection manifest.
- `GET /api/v1/content/sha256/{digest}`: one allowlisted inspection file, returned as `text/plain`, `application/json`, or a PNG, JPEG, GIF, or WebP image. Other content returns `415`.
- `GET /api/v1/artifacts/sha256/{digest}`: artifact bytes with content type `application/vnd.agent-marketplace.artifact.v1.tar+gzip`. A digest that no published revision owns returns `404`, even if a blob exists at that address. Returns `410` when revoked, `403` when blocked by assessment, and `413` above `policy.limits.artifact_bytes`.

In these `sha256/{digest}` routes, `{digest}` is exactly 64 lowercase hexadecimal characters; the `sha256` part is a path segment. In `database` management mode, the inspection and content routes also refuse objects that no available revision owns (`404`, `403`, or `410`).

### Health

- `GET /healthz`: returns `{"status":"ok"}` while the process runs. It skips the authority and availability checks. See the [health and metrics reference](marketplace-health-and-metrics.md).

## Browser routes

The separate Nuxt frontend serves these HTML pages behind the reverse proxy. The API listener does not serve them and returns `404` for these paths.

- `GET /r/{registry}/{kind}/{name}`: page for the latest revision.
- `GET /r/{registry}/{kind}/{name}/revisions/{revision}`: page for an exact revision.

## Administration routes

These routes exist on the public listener under `/api/v1/admin/`. When `admin.enabled` is `false`, every one of them returns `404`. When the admin configuration is invalid, they return `503` with code `admin_unavailable`. Except for the sign-in routes, they also require `management.mode: database` with adopted configuration and return `503` with code `managed_configuration_required` otherwise. Admin errors are JSON objects with `code` and `message`.

Sign-in routes (OIDC mode only):

- `GET /api/v1/admin/auth/login`: starts sign-in and redirects (`303`) to the identity provider. Optional `next` parameter; only `/marketplace/admin` and paths below it are accepted.
- `GET /api/v1/admin/auth/callback`: OIDC callback. Sets the session cookie and redirects (`303`).
- `POST /api/v1/admin/auth/logout`: expires the admin cookies. Safe to repeat.

Read routes (`GET` or `HEAD`, responses use `Cache-Control: no-store`):

- `/api/v1/admin/session`: the authenticated actor, authentication mode, and capabilities.
- `/api/v1/admin/configuration`: the managed configuration. The `ETag` header carries the current revision.
- `/api/v1/admin/profiles`: credential profile keys with their allowed origins, plus the accepted source harnesses.
- `/api/v1/admin/instance`: instance name, version, management mode, and revision.
- `/api/v1/admin/attempts`: refresh attempt history. Parameters `before`, `limit` (1 to 100), `registry_id`, and `job_id`. Failed and superseded attempts may carry `failure_code`, a stable machine-readable classification, and `failure_cause`, a sanitized cause that never contains raw command output or credentials; both are omitted on started and succeeded attempts, and historical records may lack them.
- `/api/v1/admin/activity`: audited changes, newest first. Parameters `before` and `limit` (1 to 100, default 50).
- `/api/v1/admin/jobs/{id}`: status of one managed job.
- `/api/v1/admin/previews/{id}`: state and result of one discovery preview.

Write routes (`POST` only; any other method returns `405` with code `method_not_allowed`):

- `/api/v1/admin/validate`: static validation of a proposed change. Returns `200` with `{"valid": true, "warnings": []}`.
- `/api/v1/admin/previews`: queues an acquisition and discovery preview of a registry proposal. Returns `202`. Previews never publish.
- `/api/v1/admin/changes`: applies an audited change. Returns `202` and an `ETag` with the new revision.

Write routes require an `Origin` header equal to `admin.origin`, the header `X-Marketplace-Admin: 1`, `Content-Type: application/json`, and an `If-Match` header with the quoted revision from `/api/v1/admin/configuration`. Request bodies are limited to 1 MiB. `changes` also requires an `Idempotency-Key` header. Any request that carries an `Origin` header, including a read, must match `admin.origin`. See [How to enable and use the Marketplace admin panel](../how-to/operator/enable-and-use-the-admin-panel.md) for authentication setup and the full workflow.

## Operations routes

- `GET /readyz`: returns `{"status":"ready"}` with `200` when the server is ready, and `503` otherwise.
- `GET /metrics`: Prometheus metrics.

On the public listener, `/readyz` and `/metrics` return `404`. The [health and metrics reference](marketplace-health-and-metrics.md) describes the readiness checks and every metric.

## Timeouts

| Listener | Read | Write | Idle |
|---|---|---|---|
| Public | 5s | 30s | 120s |
| Operations | 5s | 10s | 120s |

## Related documentation

- [Marketplace health and metrics reference](./marketplace-health-and-metrics.md)
- [Marketplace resource locators reference](./marketplace-resource-locators.md)
- [Marketplace operator configuration reference](./marketplace-operator-configuration.md)
