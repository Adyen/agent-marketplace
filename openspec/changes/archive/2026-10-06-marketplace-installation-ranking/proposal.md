## Why

Marketplace browsing does not distinguish resources by adoption, despite existing inventory telemetry. Ranking by recent reported adoption helps users find resources already enabled by other reporting installations.

## What Changes

- Add opt-in, operator-configured access to Prometheus-compatible inventory metrics with a five-minute server-side cache.
- Count distinct reporting installations using their latest inventory within 30 days, matching existing telemetry identities to unambiguous registry origins.
- Add an explicit popularity search order and optional aggregate installation counts without changing legacy search requests.
- Request popularity ordering in the browser, preserve relevance for text searches, and display reported installation counts.
- Omit unavailable counts and preserve existing ordering when metrics are disabled, empty, failed, or expired.
- Document that these counts describe enabled configuration, exclude non-reporting installations, and may undercount hosted Marketplace resources or installed-name aliases. Telemetry correction and production deployment are out of scope.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-api`: optional installation ranking and counts backed by operator-configured existing metrics.
- `marketplace-browser`: popularity browsing with count presentation and unchanged filters and incremental loading.

## Impact

Marketplace operator configuration, HTTP search API and schema, Nuxt API normalization and resource rows, tests, and operator/API documentation. No store migration, telemetry schema change, new runtime dependency, or consumer configuration change is planned.
