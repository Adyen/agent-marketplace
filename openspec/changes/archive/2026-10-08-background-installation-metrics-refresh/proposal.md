## Why

Installation-ranked search synchronously waits for a telemetry fetch when its aggregate expires, producing recurring search latency spikes. Refreshing independently of requests lets discovery remain responsive while preserving count freshness and identity checks.

## What Changes

- Refresh installation counts asynchronously in each API process and serve searches from complete published aggregates.
- Preserve deterministic discovery fallback for unavailable, expired or identity-incompatible counts; keep a still-fresh compatible last-good aggregate after a failed proactive refresh.
- Add optional installation metrics interval, retry interval, jitter, maximum age and timeout settings with defaults independent of registry reindexing.
- Expose bounded refresh health metrics and log failures once per attempt.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-api`: background installation refresh, non-blocking search, configurable freshness and scheduling, safe failure behavior and refresh observability.

## Impact

The API installation metrics reader, API lifecycle, search lookup, operations metrics and Marketplace operator configuration loader change. Existing operator/API reference guidance and regression tests are updated. No public response schema, telemetry query, inventory identity rules, registry scheduler, storage or deployment changes are required.
