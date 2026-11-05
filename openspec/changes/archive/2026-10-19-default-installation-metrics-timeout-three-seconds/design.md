## Context

See proposal.md. `config.ResolveInstallationMetricsTiming` applies `defaultInstallationMetricsTimeout` when `installation_metrics.timeout` is absent. The aggregated query measured 2.03–2.38 s end to end (2.19 s Prometheus execution) against production; production configures `timeout: 3s`.

## Decisions

Change only the constant to `3 * time.Second`. Validation already rejects `interval + jitter + timeout >= max_age`; the defaults give 4m15s3s, below 5m. No new configuration field: the existing optional `installation_metrics.timeout` is the operator knob.

## Risks / Trade-offs

A slower default lets a stuck backend hold the single refresh attempt one second longer; searches never wait for refresh, so user-facing latency is unaffected.
