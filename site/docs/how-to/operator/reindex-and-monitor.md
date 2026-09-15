# How to reindex and monitor a Marketplace

Refresh registry data and observe Marketplace health.

## Reindex one registry

```bash
agent-marketplace serve reindex shared-agents --config /app/marketplace.yaml
```

## Reindex every registry

```bash
agent-marketplace serve reindex --config /app/marketplace.yaml
```

The worker applies configured revocations before acquisition and performs a readiness check after processing.

## Monitor with health endpoints

- `/healthz` on the public listener returns process liveness.
- `/readyz` on the operations listener returns storage readiness.
- `/metrics` on the operations listener returns Prometheus metrics.

Do not expose `/readyz` or `/metrics` through the public reverse proxy.

## Watch the worker log

Look for:

- `marketplace.ingest.duplicate-resource`: duplicate registry-local names.
- `marketplace.policy.license-not-allowed`: a resource was rejected by the license allowlist.
- `marketplace.policy.license-undeclared`: a resource has no license.
- Migration success or failure messages on startup.

## Alert on metrics

Track:

- `agent_marketplace_requests_total`: total HTTP requests; useful for request-rate alerts.
- `agent_marketplace_active_requests`: in-flight HTTP requests; useful for concurrency alerts.

Alerts for error rate, latency, queue age, storage saturation, and reindex health need signals from the reverse proxy, application logs, or the platform running the containers; these two application metrics alone do not provide them.

## Related documentation

- [Marketplace health and metrics reference](../../reference/marketplace-health-and-metrics.md)
