# Prometheus metrics for the Marketplace API

**Status:** Approved and implemented on 2026-09-15.

Before this change, the Marketplace API exposed `/metrics` on its private operations listener, but the endpoint manually formatted two process-local atomic counters. The implementation replaced that placeholder with Prometheus-native HTTP RED metrics and standard Go runtime and process collectors while preserving the existing network boundary.

## Background

`Server` owns separate public and operations `http.Server` instances. The public listener serves the Marketplace API and `/healthz`; the operations listener serves `/readyz` and `/metrics`. Public requests to `/readyz` and `/metrics` deliberately return `404` so deployment proxies cannot expose operational data accidentally.

Before this change, the middleware incremented `requestsTotal` and `activeRequests` for both listeners. The metrics handler rendered those values as Prometheus text, but it could not distinguish successful requests from errors, identify slow route families, or expose runtime and process health. It also implemented exposition formatting directly rather than using the Prometheus client.

## Goals

The approved change provides:

- request rate, errors, and duration for every public and operations HTTP route;
- bounded dimensions suitable for dashboards and alerts;
- current in-flight requests by listener;
- standard Go runtime and process metrics;
- Prometheus and OpenMetrics content negotiation through the standard handler;
- isolated metric state for each `Server` instance;
- continued private-only access to operational metrics.

The change does not add application-authentication policy, publish `/metrics` through the public listener or reverse proxy, instrument worker refresh and ingestion internals, define dashboards or alerts, or add operator configuration for metric selection.

## Metrics model

The Marketplace-specific metric families are:

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `agent_marketplace_requests_total` | Counter | `listener`, `method`, `route`, `code` | Completed HTTP requests partitioned by listener, method, bounded route pattern, and response status code. |
| `agent_marketplace_request_duration_seconds` | Histogram | `listener`, `method`, `route` | End-to-end handler duration using `prometheus.DefBuckets`. Errors remain derivable from the request counter rather than duplicating the `code` dimension on every histogram bucket. |
| `agent_marketplace_active_requests` | Gauge | `listener` | Requests currently executing on the public or operations listener. |

`listener` has exactly two values: `public` and `operations`. `method` and `code` follow the standard `promhttp` instrumentation labels. `route` is the registered `http.ServeMux` pattern, never `r.URL.Path`. Requests that match no declared route use the single value `unmatched`.

Using registered patterns keeps identifiers, digests, resource names, and arbitrary unknown paths out of labels. The number of time series therefore grows only when the server declares a route, not when clients introduce new URL values.

The existing aggregate metric names remain stable, but their former singleton series become labeled series. Queries that need the old aggregate view must sum the metric. This dimensional change is intentional because status and route visibility are the purpose of the feature.

A server registry also explicitly registers the standard Go and process collectors. Their `go_*` and `process_*` metric families retain the names and semantics defined by `prometheus/client_golang`.

## Registry ownership and isolation

Each `Server` owns a dedicated `prometheus.Registry`. The registry contains that server's three Marketplace collectors plus one Go collector and one process collector. The design does not use Prometheus's global default registry.

Per-server ownership matters because unit tests and embedding callers construct many servers in one process. Global registration would either panic on duplicate collectors or mix measurements from unrelated server instances. A dedicated registry makes `NewServer` deterministic and keeps tests independent.

The approved dependency is `github.com/prometheus/client_golang` v1.24.1.

## HTTP instrumentation architecture

Instrumentation belongs at route registration, outside method validation and the endpoint handler:

```text
listener request
    -> in-flight instrumentation
    -> duration instrumentation
    -> completion/status instrumentation
    -> method guard
    -> endpoint handler
```

This ordering ensures that `405`, endpoint-generated errors, readiness failures, and successful responses all contribute the status observed by Prometheus. `promhttp` wrappers retain the response-writer behavior needed by normal HTTP handlers and avoid a local status-recorder implementation.

Route registration binds the fixed `listener` and `route` values before serving requests. The remaining `method` and `code` labels are supplied by `promhttp`. Both muxes have an instrumented catch-all handler labeled `unmatched`; it preserves the standard plain-text `404` behavior for undeclared browser and API paths.

The operations `/metrics` request is itself instrumented, matching the current whole-listener counting behavior. Its in-flight gauge may therefore include the scrape being served, and repeated scrapes contribute to the operations request counter. Consumers can select `listener="public"` when they want only customer-facing traffic.

The metrics endpoint uses `promhttp.HandlerFor` against the server registry with OpenMetrics negotiation enabled. The existing GET/HEAD method contract remains in force. The public listener continues to bind explicit `/metrics` and `/readyz` rejection handlers, preserving the deliberate `404` boundary for every method.

## Code boundaries

The implementation is expected to keep metric construction and instrumentation cohesive rather than enlarging the already substantial API server file:

- `pkg/marketplace/api/metrics.go` owns the private metrics type, per-server registry, collector construction, route instrumentation, and scrape handler.
- `pkg/marketplace/api/server.go` owns route declarations and binds each route to the metrics layer. The atomic counter fields, handwritten middleware, and handwritten exposition handler disappear.
- `pkg/marketplace/api/metrics_test.go` owns focused black-box metric contracts.
- `go.mod` and `go.sum` record the Prometheus client dependency and its transitive dependencies. Workspace sums may change only as required by the Go workspace.

No operator configuration or OpenAPI change is required. `/metrics` is already documented as an operations endpoint and is intentionally absent from the public OpenAPI document.

## Failure and compatibility properties

Collector construction is local and deterministic; it has no storage or network dependency. A scrape reports the current in-memory registry and does not call the metadata or blob stores.

The network boundary is unchanged: `operations_listen` remains the sole metrics address, defaults to loopback, and must stay off public reverse-proxy routes. Runtime and process collectors can reveal operational details, which reinforces rather than relaxes that requirement.

Every label is bounded or derived from HTTP protocol values. No query string, credential, registry origin, resource name, digest, or request header enters metric labels or help text.

Metric collection must not alter endpoint responses. Existing status codes, bodies, content types, cache headers, HEAD behavior, and public operations-endpoint rejections remain observable contracts.

## Verification contract

Focused tests should establish the externally visible behavior rather than inspect collector fields. The required cases are:

- a successful public API request produces a `code="200"` request series for its fixed route;
- a method rejected by the GET/HEAD guard produces `code="405"` for the same route;
- an unknown path produces a `code="404"`, `route="unmatched"` series without embedding the requested path;
- public `/metrics` remains `404`, while operations `/metrics` returns a valid Prometheus or negotiated OpenMetrics response;
- the duration histogram exposes count and bucket series for an exercised route;
- the in-flight metric is partitioned by listener;
- representative `go_*` and `process_*` families are present;
- two independently constructed servers do not panic, share application counters, or contaminate each other's scrapes.

The existing `BenchmarkServerSearch` is the performance gate because it exercises a user-visible API route through the complete handler stack. Repository standards require a five-run, two-second baseline before implementation and the identical measurement afterward, including allocations. The change has no fixed numerical threshold, but the before/after delta must be reported and any material regression understood before merge.

The focused package test gate is `pkg/marketplace/api`; the normal repository lint and broader test gates remain required before delivery.

## Alternatives considered

### Keep handwritten exposition

Extending atomic counters and formatting every Prometheus family locally would avoid a dependency, but it would recreate status recording, histogram behavior, escaping, content negotiation, collector registration, and response-writer compatibility. That surface is easy to get subtly wrong and provides no advantage over the maintained client library.

### Use the global Prometheus registry

The default registry is convenient for one process-wide server, but this code constructs servers repeatedly in tests and may be embedded. Global state creates duplicate-registration failures and cross-test contamination, so the design uses a registry owned by each server.

### Label by raw URL path

Raw paths would make individual resources visible but create attacker-controlled cardinality from names, digests, and arbitrary unknown URLs. Fixed mux patterns preserve route-level utility without unbounded memory and storage costs.

### Export only OpenTelemetry metrics

The repository already uses OpenTelemetry elsewhere, but the requested operational contract is a Prometheus scrape endpoint. Routing HTTP measurements through an additional OpenTelemetry-to-Prometheus bridge would add configuration and lifecycle complexity without improving this bounded endpoint. Direct Prometheus collectors are the narrower design.

## Acceptance criteria

The design is delivered when the private operations listener exposes the three Marketplace HTTP metric families with the approved labels, Go and process collectors are present, public isolation and existing HTTP behavior remain unchanged, metric state is isolated per server, focused tests pass, and the measured benchmark delta is recorded.
