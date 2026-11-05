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

`/healthz` skips the configuration authority and availability checks that other public routes run, so it answers even when the server cannot serve the catalog. It suits load balancer checks that only need to know the process is up.

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

Failures of checks 2 to 4 return `{"error":"not ready"}`. Container startup and readiness probes target this endpoint on the operations listener.

## `/metrics`

The endpoint serves the Prometheus text format and, when the scraper asks for it, OpenMetrics. It exposes these application series:

| Metric | Type | Labels | Description |
|---|---|---|---|
| `agent_marketplace_requests_total` | counter | `listener`, `method`, `route`, `code` | HTTP requests handled. |
| `agent_marketplace_request_duration_seconds` | histogram | `listener`, `method`, `route` | HTTP request duration in seconds, with the Prometheus default buckets. |
| `agent_marketplace_active_requests` | gauge | `listener` | HTTP requests in progress. |
| `agent_marketplace_public_request_duration_seconds` | histogram | `method`, `route`, `code` | Full public request stack duration, including the configuration authority and availability checks. Buckets: 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10 seconds. |
| `agent_marketplace_request_phase_duration_seconds` | histogram | `phase` | Duration of individual public request phases, with the same buckets as the public stack histogram. |
| `agent_marketplace_metadata_pool_waits_total` | counter | none | Cumulative metadata database connection pool waits since the metadata connection pool opened, from `database/sql` pool statistics. |
| `agent_marketplace_metadata_pool_wait_seconds_total` | counter | none | Cumulative metadata database connection pool wait time in seconds since the metadata connection pool opened, from `database/sql` pool statistics. |

Label values:

- `listener`: `public` or `operations`.
- `method`: the lowercase HTTP method (for example `get` or `head`), or `unknown` for a non-standard method.
- `route`: the registered route pattern that matched, for example `/api/v1/search` or `/api/v1/resources/`. Requests that match no route use `unmatched`. Raw request paths never become label values.
- `code`: the response status code, for example `200` or `404`.
- `phase`: `authority`, `availability`, `catalog_read`, `catalog_availability`, or `catalog_response`.

The two public stack histograms observe only public listener requests and carry no `listener` label. A phase sample is recorded only when that phase actually ran: `authority` runs on every public route except `/healthz`, `availability` only in `database` management mode on non-admin routes, and the `catalog_*` phases only on the catalog route, with `catalog_availability` skipped when a matching conditional request short-circuits the page read. Phase values are independent observations, not slices of a shared budget; they do not sum to the full stack duration.

The endpoint also exposes the standard Go runtime (`go_*`) and process (`process_*`) collectors.

The three `listener`-labeled series observe requests inside the configuration authority and availability middleware. Public requests that fail the configuration authority check, or the availability check in `database` management mode, receive a `503` before they reach the instrumented router, so they still do not appear in those series. `agent_marketplace_public_request_duration_seconds` wraps those checks, so its `code` label records the early `503` responses and its `route` label carries the registered pattern or `unmatched`.

The server has no metrics for the worker, the job queue, or reindex runs. Monitor queue age, refresh failures, and reindex duration with external tooling, for example the logs of `serve` and `serve reindex`.

## Keep operations endpoints private

On the public listener, `/readyz` and `/metrics` return `404`. The server exposes no profiling endpoints. Keep the operations listener on a private network or loopback, and do not route it through the public reverse proxy.

## Related documentation

- [Marketplace HTTP API reference](./marketplace-http-api.md)
- [How to reindex and monitor a Marketplace](../how-to/operator/reindex-and-monitor.md)
