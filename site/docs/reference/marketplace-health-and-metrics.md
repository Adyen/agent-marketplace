# Marketplace health and metrics reference

This page describes the health, readiness, and metrics endpoints of a hosted Marketplace server. Operators use it to configure load balancer checks, container probes, and Prometheus scraping.

## Endpoints

| Path | Listener | Purpose |
|---|---|---|
| `/healthz` | public | Process liveness. Does not check storage. |
| `/readyz` | operations | Readiness: configuration authority, revocations, metadata store, and blob store. |
| `/metrics` | operations | Prometheus metrics. |

All three accept `GET` and `HEAD`.

## `/healthz`

Returns `200` with this body while the process is running:

```json
{"status": "ok"}
```

`/healthz` skips the configuration authority and availability checks that other public routes run, so it answers even when the server cannot serve the catalog. Use it for load balancer checks that only need to know the process is up.

## `/readyz`

Returns `200` with this body when every readiness check passes:

```json
{"status": "ready"}
```

The checks run in this order, and the first failure returns `503`:

1. Configuration authority matches `management.mode`. On failure the body is `{"error":"configuration authority unavailable"}`.
2. The revocations stored in the metadata database match the revocations in the operator configuration.
3. The instance record exists in the metadata store.
4. The blob store answers a probe within 2 seconds.

Failures of checks 2 to 4 return `{"error":"not ready"}`. Point container startup and readiness probes at this endpoint on the operations listener.

## `/metrics`

The endpoint serves the Prometheus text format and, when the scraper asks for it, OpenMetrics. It exposes these application series:

| Metric | Type | Labels | Description |
|---|---|---|---|
| `agent_marketplace_requests_total` | counter | `listener`, `method`, `route`, `code` | HTTP requests handled. |
| `agent_marketplace_request_duration_seconds` | histogram | `listener`, `method`, `route` | HTTP request duration in seconds, with the Prometheus default buckets. |
| `agent_marketplace_active_requests` | gauge | `listener` | HTTP requests in progress. |

Label values:

- `listener`: `public` or `operations`.
- `method`: the lowercase HTTP method (for example `get` or `head`), or `unknown` for a non-standard method.
- `route`: the registered route pattern that matched, for example `/api/v1/search` or `/api/v1/resources/`. Requests that match no route use `unmatched`. Raw request paths never become label values.
- `code`: the response status code, for example `200` or `404`.

The endpoint also exposes the standard Go runtime (`go_*`) and process (`process_*`) collectors.

Public requests that fail the configuration authority check, or the availability check in `database` management mode, receive a `503` before they reach the instrumented router. Those requests do not appear in these metrics.

The server has no metrics for the worker, the job queue, or reindex runs. Monitor queue age, refresh failures, and reindex duration with external tooling, for example the logs of `serve` and `serve reindex`.

## Keep operations endpoints private

On the public listener, `/readyz` and `/metrics` return `404`. Keep the operations listener on a private network or loopback, and do not route it through the public reverse proxy.

## Related documentation

- [Marketplace HTTP API reference](./marketplace-http-api.md)
- [How to reindex and monitor a Marketplace](../how-to/operator/reindex-and-monitor.md)
