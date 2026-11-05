## Why

Reported installation counts disappeared from production search on 2026-10-16. Every background refresh now fails because the latest-inventory projection returns one row per installation and resource: 9.3 MB against the specified 8 MiB response budget. That projection grows with every new installation, so any fixed budget is eventually exhausted.

## What Changes

- Evaluate freshness, ambiguity, deduplication, registry identity matching and hosted isolation in the metrics backend, and return one aggregate count per counted resource coordinate plus the fresh-installation count and the oldest fresh observation.
- Build the Git registry-identity mapping and registry filters into the query from current registry metadata, so local and unknown registries never reach the response.
- Keep the malformed-observation fallback: the backend returns exactly checkable violations as invalid markers and a small superset of suspicious hosted identities that the server validates with the existing rules.
- Prove the query's semantics by evaluating it in a real Prometheus engine through a Docker-backed gate.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-api`: current reported installation counts are aggregated by the metrics backend, so the response grows with counted resource coordinates instead of reporting installations, and no Installation ID crosses the metrics boundary for well-formed data.

## Impact

Installation metrics query construction, response parsing and validation, their tests, a Docker-backed Make target, one CI job, and the Marketplace API spec. No public API field, operator configuration, telemetry emission, frontend or deployment change. The 8 MiB budget and timeouts are unchanged.
