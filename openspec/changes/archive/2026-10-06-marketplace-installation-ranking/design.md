## Context

The search handler selects latest effective revisions, ranks by text relevance and resource coordinate, then pages. Nuxt preserves the API order while appending pages. Inventory telemetry exports a snapshot gauge containing the observation time in Unix microseconds and resource gauges containing one. The resource labels include Installation ID, configuration fingerprint, sanitized Git-location registry identity, resource kind, and resource name. These registry identities differ from immutable Marketplace registry IDs.

## Goals / Non-Goals

Goals are accurate current reported adoption, pagination-aware ordering, bounded optional metrics access, optional aggregate count presentation, and legacy API compatibility.

Non-goals are changes to telemetry emission/schema, lifetime installation events, production deployment, database migrations, installed-name reconciliation, and automatic merging.

## Decisions

### Read existing inventory without changing emission

Add `installation_metrics` to operator configuration with `url`, optional `bearer_token_env`, and an exact-match `labels` map. A blank URL disables the reader. Accept only HTTP(S) URLs without userinfo, query, or fragment; bearer authentication requires HTTPS except loopback test endpoints. Deployment selector keys are restricted to namespace, endpoint, service, job, and cluster. Use the existing standard HTTP client and JSON libraries, a two-second request deadline, an eight-MiB response limit, and no redirect following.

Issue one Prometheus instant query combining snapshot and resource vectors. Explicit `inventory_type` labels distinguish the families. Group snapshots by Installation ID/configuration fingerprint and resource membership by Installation ID/fingerprint/telemetry registry identity/kind/name to remove scrape/CLI-version/ref/scope duplicates. Use 30-day range selectors. Independently check the snapshot gauge's observation timestamp against the 30-day cutoff; a collector can keep scraping an old gauge, so sample timestamps alone do not prove freshness. Parent-authored query and aggregation logic are authoritative.

Select the greatest fresh observation timestamp for each Installation ID, then only resource memberships with that fingerprint. If two fingerprints tie for the latest timestamp, omit that installation rather than pick an arbitrary generation. A response without fresh snapshot observations is unavailable, not zero adoption. No raw Installation IDs survive the aggregate cache or public response.

### Reconcile identities before counting

Use the existing telemetry registry sanitizer to canonicalize every non-local registry origin. Map an identity only when exactly one immutable Marketplace registry owns it. Do not count `local` or ambiguous identities. Deduplicate by Installation ID plus mapped resource coordinate, including across historical origins of one registry. Only registries with unambiguous usable origins receive optional counts. Unknown registry mapping remains absent rather than zero. Existing hosted Marketplace telemetry and installed-name aliases can undercount and are documented limitations.

Cache aggregate counts, including zero for known registry/resource coordinates, for five minutes. Include a stable registry-origin index signature in the cache key so administration changes cannot reuse obsolete identity attribution. Serialize refreshes; cache failed refresh state briefly to avoid a request storm, but never serve expired successful counts after a failure.

### Preserve legacy search while changing browser requests

Add optional `sort=installations`; invalid values return HTTP 400. Without it, preserve existing API order and avoid metrics calls. With it, compare existing text relevance first, then count descending when available, then existing registry/kind/name ties. Sort before `slicePage`. Add optional nonnegative safe-integer `installation_count` to resource list items only when counts are available for the mapped registry.

The browser always sends `sort=installations`, including filtered and incremental pages. It does not sort locally. Add a compact count beside existing row metadata with accessible explanatory text; omit it when absent, and render a valid zero. Do not add a new sorting control or unrelated visual changes.

## Risks / Trade-offs

Counts represent reporting enabled configurations and can undercount non-reporting installations, hosted Marketplace installs, and aliases. Single-process caches can change between offset pages; existing offset pagination remains a moving result set rather than a pinned ranking snapshot. A cold popularity request may wait up to the bounded metrics timeout; subsequent requests reuse the cache. Empty, invalid, stale, or failed telemetry falls back to discovery without counts. Deployment labels must isolate the intended telemetry corpus; no production endpoint or credential is embedded in code or fixtures.
