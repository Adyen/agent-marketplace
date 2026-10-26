## Context

Installation counts come from one Prometheus-compatible aggregate per refresh
(`installationMetricsQueries` in `installation_adoption.go`). The aggregate is
a union of five operands with distinct `inventory_type` labels: count, fresh,
valid_until, invalid, suspicious. The single-expression union was measured at
3.3-3.6 s against a 3 s client timeout in production, because Prometheus
evaluates the ` or ` operands sequentially. The operand costs are count 2.1 s,
fresh 0.27 s, valid_until 0.27 s, invalid 0.7 s, suspicious 0.2 s.

The snapshot previously served search only until `min(evaluatedAt+max_age,
ValidUntil)`. ValidUntil is the oldest fresh observation plus 30 days, so it
is often minutes away, and one failed refresh removed every count.

## Decisions

### A. Concurrent operands with one evaluation time

- `installationMetricsQueries` returns the operands in the existing order
  (count only when a contributing set exists, then fresh, valid_until,
  invalid, suspicious). Each operand's text is unchanged.
- `fetch` computes the evaluation time once and issues one POST per operand
  concurrently through plain goroutines and a `sync.WaitGroup`. All requests
  carry the same `time` value, headers, bearer handling, redirect policy, and
  client timeout. A single `queryVector` helper performs the request and
  response read, so the request code is not duplicated.
- Every request is waited on; siblings are not cancelled on the first error.
- Error precedence is deterministic: if any operand returns
  `errInvalidInstallationMetrics`, the refresh returns it; otherwise, if any
  operand failed, it returns `errInstallationMetricsUnavailable`.
- Each response is still read through `LimitReader(installationMetricsMaxBody+1)`.
  The sum of body bytes across operands must be at most
  `installationMetricsMaxBody`, or the refresh is unavailable. This matches
  today's oversize outcome, now measured over the whole refresh.
- Samples are concatenated in operand order and passed to the unchanged
  `aggregateInstallationMetrics`.
- Refresh remains serialized. Only the sub-requests inside one attempt run
  concurrently, so at most one attempt is in flight.
- The promtool real-engine test and query-text tests use a test-only
  `installationMetricsQuery` helper, which joins the operands with ` or `. The
  union is equivalent because each operand carries a distinct `inventory_type`.

Trade-off: the metrics endpoint sees five requests per refresh instead of one.
The timeout bounds each request independently, so worst-case latency falls
from the sum of operand costs to roughly the slowest operand.

### B. Last-good counts up to max_stale

- New optional `installation_metrics.max_stale`. The default is twenty-four
  hours. It must be positive and not shorter than `installation_metrics.max_age`.
- A snapshot records `staleUntil = evaluatedAt + MaxStale`. It is not capped
  by ValidUntil. `expiresAt` keeps its meaning (freshness). It still drives
  scheduling, the hint short-circuit in `attempt`, and the post-fetch
  already-expired check in `refresh`.
- `lookup(registries)` returns `(counts, fresh)`:
  - identity-incompatible, no snapshot, or `now >= staleUntil` → `(nil, false)`;
  - compatible and `now < expiresAt` → `(counts, true)`;
  - compatible and `expiresAt <= now < staleUntil` → `(counts, false)`.
- In `handleSearch`, counts are used whenever non-nil. A non-blocking hint is
  sent whenever `!fresh`.
- Failed attempts do not clear the snapshot. A successful refresh replaces it.
- No public API, OpenAPI, or web change.

Trade-off: during an outage, counts can be up to a day old and still shown.
They are shown as current, which is acceptable for a ranking signal, and the
operator can lower `max_stale` to tighten that bound.

## Verification

- Measured timings above are from production Prometheus (operator-supplied).
- Unit tests cover config defaults, overrides, and rejections; stale serving at
  expiry, past ValidUntil, and at `staleUntil`; identity-incompatible stale
  rejection; replacement on successful refresh; concurrent request shape with
  one shared evaluation time; combined-budget enforcement; and precedence
  between invalid and unavailable operands.
- `make installation-query-check` runs the promtool real-engine gate over the
  union query.
