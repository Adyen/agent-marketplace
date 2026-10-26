## 1. Concurrent metrics operands

- [x] 1.1 Return the query operands as a slice from `installationMetricsQueries`; keep a test-only union helper.
- [x] 1.2 Add `queryVector` and issue operands concurrently in `fetch` with one shared evaluation time.
- [x] 1.3 Enforce deterministic error precedence and the combined eight-MiB response budget.

## 2. Last-good counts up to max_stale

- [x] 2.1 Add `installation_metrics.max_stale` with default, validation, and resolved timing.
- [x] 2.2 Record `staleUntil` on snapshots and change `lookup` to return `(counts, fresh)`.
- [x] 2.3 Serve non-nil counts in `handleSearch` and hint whenever the snapshot is not fresh.

## 3. Tests

- [x] 3.1 Config: default, override, and rejection cases for `max_stale`.
- [x] 3.2 Reader: stale serving, expiry boundaries, identity rejection, successful replacement.
- [x] 3.3 Search: stale counts with a hint, then omitted at `max_stale`.
- [x] 3.4 Fetch: concurrent request shape, combined budget, error precedence, failed operand keeps snapshot.

## 4. Specs and docs

- [x] 4.1 Spec delta for `marketplace-api` (modified requirements and scenarios).
- [x] 4.2 Bind every changed scenario with `// spec:` annotations.
- [x] 4.3 Update operator configuration and HTTP API reference pages.

## 5. Verification

- [ ] 5.1 `CGO_ENABLED=0 go test ./pkg/marketplace/api ./pkg/marketplace/config -count=1`
- [ ] 5.2 `make installation-query-check`
- [ ] 5.3 `make spec-check`
- [ ] 5.4 `make validate` once at the end
