# Marketplace health and metrics reference

This page describes the health, readiness, and Prometheus metrics endpoints exposed by the hosted Marketplace.

## Endpoints

| Path | Listener | Purpose |
|---|---|---|
| `/healthz` | public | Process liveness. Does not prove storage readiness. |
| `/readyz` | operations | Storage and dependency readiness. |
| `/metrics` | operations | Prometheus metrics. |

## `/healthz`

Returns a JSON body when the process is running:

```json
{"status": "ok"}
```

Use this for load balancer health checks that do not need storage proof.

## `/readyz`

Returns a JSON body when the database and blob store are reachable:

```json
{"status": "ready"}
```

Container startup probes should target this on the operations listener.

## `/metrics`

The current application metrics expose only these series:

```text
# HELP agent_marketplace_requests_total Total HTTP requests
# TYPE agent_marketplace_requests_total counter
agent_marketplace_requests_total <n>

# HELP agent_marketplace_active_requests Active HTTP requests
# TYPE agent_marketplace_active_requests gauge
agent_marketplace_active_requests <n>
```

These counters do not include route, status, latency, queue, or reindex labels. Broader monitoring such as route error rate, p95 latency, queue age, and reindex duration must be provided by external infrastructure.

## Do not expose operations endpoints publicly

`/readyz` and `/metrics` are rejected with `404` on the public listener. Keep the operations listener on a private network or loopback.

## Related documentation

- [Marketplace HTTP API reference](./marketplace-http-api.md)
- [How to reindex and monitor a Marketplace](../how-to/operator/reindex-and-monitor.md)
