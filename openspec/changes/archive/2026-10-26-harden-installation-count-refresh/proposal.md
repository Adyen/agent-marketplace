## Why

Production Marketplace search stopped reporting installation counts. The
background installation-metrics refresh sends one PromQL query whose top-level
` or ` operands Prometheus evaluates sequentially. Measured against production
Prometheus, the whole query takes 3.3-3.6 seconds against a 3-second client
timeout. Per operand the cost is: count 2.1 s, fresh 0.27 s, valid_until
0.27 s, invalid 0.7 s, suspicious 0.2 s.

Any refresh failure also hides every count once the snapshot passes
`min(evaluatedAt+max_age, ValidUntil)`. ValidUntil (oldest fresh observation
plus 30 days) is routinely only minutes away, so a single slow or failed
refresh empties the public ranking even when the last good aggregate is
still a faithful picture of the fleet.

## What Changes

- Installation-metrics refresh evaluates its operands as concurrent backend
  requests that share one evaluation time. The counting rules, error
  precedence for malformed responses, and the eight-MiB response limit are
  unchanged; the limit now applies to the combined responses of one refresh.
- Search serves a complete, identity-compatible aggregate for up to
  `installation_metrics.max_stale` after its evaluation time (default
  twenty-four hours), even past its freshness expiry or inventory validity
  horizon, while a non-blocking refresh hint is sent. Refresh scheduling and
  expiry are unchanged. A failed refresh does not discard the last-good
  aggregate.
- New optional configuration `installation_metrics.max_stale`, which must be
  positive and not shorter than `installation_metrics.max_age`.
- Public API, OpenAPI, and web output are unchanged; stale counts look the same
  as fresh ones.

## Release note

Breaking: the new `installation_metrics.max_stale` setting (default 24 hours) must not be shorter than `installation_metrics.max_age`, so operators running with `max_age` above 24 hours must set `max_stale` to at least `max_age` before upgrading. Installation counts now keep appearing in search during a metrics outage, serving the last good counts for up to `max_stale` while refresh is retried.

## Impact

- Code: `pkg/marketplace/api/installation_metrics.go`,
  `installation_adoption.go`, `server.go` (search lookup call site), and
  `pkg/marketplace/config/config.go`.
- Specs: `marketplace-api` requirements for optional bounded installation
  metrics and installation metrics timing.
- Docs: operator configuration reference (`max_stale` field, stale serving)
  and the HTTP API reference (count availability and expiry).
- Operations: the metrics endpoint receives one request per query operand
  (five today) per refresh, issued concurrently and bounded by the configured
  timeout. Refresh remains serialized, so there is still at most one refresh
  attempt in flight.
- Prod timeout is raised separately from this change.
