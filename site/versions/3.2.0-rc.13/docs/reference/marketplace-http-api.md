# Marketplace HTTP API reference

All routes are `GET` or `HEAD` only; other methods return `405 Method Not Allowed`.

## Listeners

- **Public listener**: `listen` address in the operator configuration. Serves the API, well-known, and health routes.
- **Operations listener**: `operations_listen` address. Serves `/readyz` and `/metrics`. Keep this on a private network or loopback.

## Public routes

### Well-known

- `GET /.well-known/agent-marketplace.json`: instance metadata and browser route templates.

### Capabilities and instance

- `GET /api/v1/capabilities`: server capabilities.
- `GET /api/v1/instance`: instance metadata.

### Search and registries

- `GET /api/v1/search`: search resources. Query parameters: `q`, `kind`, `registry` (repeatable), `limit` (default `100`), `offset` (default `0`).
- `GET /api/v1/registries`: list registry identifiers.
- `GET /api/v1/registries/{registry}`: registry metadata.
- `GET /api/v1/registries/{registry}/history`: registry revision history, newest first, at most 1000 entries.
- `GET /api/v1/registries/{registry}/occurrences/{commit}`: one exact 40-hex commit occurrence.

### Resources

- `GET /api/v1/resources/{registry}/{kind}/{name}`: latest resource revision.
- `GET /api/v1/resources/{registry}/{kind}/{name}/revisions`: list all resource revisions.
- `GET /api/v1/resources/{registry}/{kind}/{name}/revisions/{revision}`: exact revision metadata.
- `GET /api/v1/resources/{registry}/{kind}/{name}/revisions/{revision}/inspection`: immutable inspection manifest.
- `GET /api/v1/resources/{registry}/{kind}/{name}/revisions/{revision}/inspection/content`: primary canonical inspection content.
- `GET /api/v1/resources/{registry}/{kind}/{name}/revisions/{revision}/inspection/content/{digest}`: one allowlisted inspection file.

`{kind}` is one of `skill`, `command`, `subagent`, `hook`, `mcpserver`, or `collection`. `{revision}` and the inspection file `{digest}` use the form `sha256:<64-lowercase-hex>`.

### Manifests and content-addressed objects

- `GET /api/v1/manifests/{registry-revision}`: registry or collection manifest. `{registry-revision}` is `sha256:<64-lowercase-hex>`.
- `GET /api/v1/assessments/sha256/{digest}`: immutable resource assessment. `{digest}` is 64 lowercase hex characters.
- `GET /api/v1/inspection/sha256/{digest}`: immutable inspection manifest. `{digest}` is 64 lowercase hex characters.
- `GET /api/v1/content/sha256/{digest}`: allowlisted inspection file. `{digest}` is 64 lowercase hex characters.
- `GET /api/v1/artifacts/sha256/{digest}`: canonical artifact bytes. `{digest}` is 64 lowercase hex characters.

## Browser routes

These browser-only routes are served by the Nuxt UI behind the reverse proxy. The API listener does not serve them and returns `404` for any matching path.

- `GET /r/{registry}/{kind}/{name}`: browser route for the latest revision.
- `GET /r/{registry}/{kind}/{name}/revisions/{revision}`: browser route for an exact revision.

## Operations routes

- `GET /readyz`: storage readiness. Returns `{"status":"ready"}` on `200`, `503` otherwise.
- `GET /metrics`: Prometheus metrics.

`/readyz` and `/metrics` return `404` on the public listener.

## Timeouts

| Listener | Read | Write | Idle |
|---|---|---|---|
| Public | 5s | 30s | 120s |
| Operations | 5s | 10s | 120s |

## Related documentation

- [Marketplace health and metrics reference](./marketplace-health-and-metrics.md)
- [Marketplace resource locators reference](./marketplace-resource-locators.md)
