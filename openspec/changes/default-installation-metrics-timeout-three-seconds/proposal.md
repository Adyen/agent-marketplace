## Why

The backend-aggregated installation query takes about 2.0–2.4 s against production Prometheus, above the two-second default `installation_metrics.timeout`. Operators can already override the timeout, but a default that the shipped query routinely exceeds forces every deployment to discover and tune it. Production already runs with three seconds.

## What Changes

- Raise the default `installation_metrics.timeout` from two to three seconds. Explicit overrides, validation (positive, and interval plus jitter plus timeout below max_age) and the other timing defaults are unchanged; the defaults still satisfy validation (4m + 15s + 3s < 5m).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-api`: the default installation metrics timeout is three seconds.

## Impact

One configuration default, its timing test, the Marketplace API spec, and the operator configuration and HTTP API reference pages. No schema, API or telemetry change.
