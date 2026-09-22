# Marketplace health and metrics reference

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

Application metrics include:

- `agent_marketplace_requests_total{listener,method,route,code}`: requests by registered route pattern, never raw paths or digests.
- `agent_marketplace_request_duration_seconds{listener,method,route}`: request latency histogram.
- `agent_marketplace_active_requests{listener}`: active requests.
- `agent_marketplace_artifact_revisions{schema="v1"|"v2"}`: mirrored revisions with legacy archives and v2 associations, respectively. The difference measures remaining backfill coverage.
- `agent_marketplace_artifact_coverage_available`: 1 when the bounded-time metadata query succeeds, otherwise 0; coverage samples are omitted on failure.

Go runtime and process collectors are also registered. Compare `/api/v1/artifacts/` and `/api/v1/blobs/` route counters to observe schema selection and errors without high-cardinality labels. Queue age and worker reindex duration still require worker logs or external monitoring.

## Do not expose operations endpoints publicly

`/readyz` and `/metrics` are rejected with `404` on the public listener. Keep the operations listener on a private network or loopback.

## Related documentation

- [Marketplace HTTP API reference](./marketplace-http-api.md)
- [How to reindex and monitor a Marketplace](../how-to/operator/reindex-and-monitor.md)
