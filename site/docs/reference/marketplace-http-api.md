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
- `GET /api/v1/artifacts/sha256/{digest}`: exact legacy v1 artifact bytes. `{digest}` is 64 lowercase hex characters.
- `GET /api/v1/blobs/sha256/{digest}`: exact v2 artifact blob bytes, addressed by the SHA-256 of the compressed bytes. `{digest}` is 64 lowercase hex characters.

### Artifact identities and compatibility

Capabilities advertise `artifact_schema_versions: [v2, v1]`. Exact resource responses retain `archive_digest` and add an `artifacts` array containing `schema_version`, `artifact_digest`, `blob_digest`, and `media_type`. An absent v2 reference means that revision has not been backfilled; global capabilities alone never authorize selection.

V2 has three identities: `content_digest` authenticates the unchanged canonical resource payload; `artifact_digest` authenticates all decoded entries; `blob_digest` authenticates the transported compressed bytes. The response media type is `application/vnd.agent-marketplace.artifact.v2.tar+gzip`.

The artifact frame starts with `agent-marketplace-artifact-v2` followed by a NUL and a big-endian u32 entry count. Each entry has a u8 type, u32 UTF-8 path length, path bytes, u64 content length, and content bytes. Types are manifest (1), payload (2), asset (3), and license (4), ordered by type and then bytewise path. Paths are `manifest.json`, `resource/<kind>.json`, `assets/<name>`, and `licenses/<name>`. Directories are implied, not hashed. Different gzip encodings can share an artifact digest while having different blob digests.

The v2 manifest contains, in order: `schema_version`, `artifact_version`, `kind`, `name`, `content_digest`, optional nonempty `license`, required ordered `capabilities`, `provenance` containing only the fixed `generator`, and `supported_schema_versions: [v2]`. Both version fields are `v2`; the generator is `agent-marketplace canonical artifact v2`. Digest and registry provenance fields are detached. Canonical JSON is compact UTF-8 without a trailing newline; duplicate, unknown, missing required, null, and noncanonical fields are rejected. Quotes, backslashes, controls, U+2028 and U+2029 are escaped; other Unicode, `/`, `<`, `>`, and `&` are literal, without Unicode normalization.

Before blob storage is read, every owning resource is checked against current restrictions. Unowned addresses return 404, blocked owners 403, and revoked owners 410. Stored integrity failures return 500. Verification checks blob bytes before bounded decompression, then structure, manifest, framed identity, payload digest, and coordinate binding. No failure permits downgrade or materialization.

V1 encoding remains frozen to Go 1.26.6 tar, gzip/DEFLATE, and manifest byte behavior. Existing v1 routes, blobs, and exact locks remain supported. Registry and collection manifests retain their existing formats; their legacy references are not rewritten during backfill.

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
