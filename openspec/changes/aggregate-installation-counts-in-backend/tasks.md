## 1. Failing regressions

- [x] 1.1 Add the Docker-backed `TestInstallationMetricsQueryPromtool` with generated promtool cases from design decision 5 and `// spec:` bindings for `marketplace-api/adoption-growth-does-not-exhaust-the-response-budget`, `marketplace-api/malformed-observations-retain-the-fallback-under-aggregation` and the existing adoption scenarios it now proves; verify it fails against the current query under `make installation-query-check`.
- [x] 1.2 Add Go unit tests for plan construction, deterministic query text and aggregate response parsing (count, fresh, valid_until, invalid, suspicious, unknown types, absent fresh) and convert search, refresh and fallback tests to canned aggregate responses; verify the new tests fail before implementation.

## 2. Implementation

- [x] 2.1 Add `newInstallationQueryPlan` and the plan-driven `installationMetricsQuery` exactly as design decisions 1 to 3 describe; verify the query-text and promtool tests pass.
- [x] 2.2 Replace per-installation aggregation with aggregate response parsing per design decision 4, leaving response limits, timeout, redirect, credential, caching and scheduling code unchanged; verify focused `./pkg/marketplace/api` tests pass.
- [x] 2.3 Add `PROMETHEUS_IMAGE` and `make installation-query-check` through `tools/witness-check -require`, the `installation_query` ci-plan output with its test, and the `Installation query` CI job; verify `go test ./tools/ci-plan` and the make target pass.

## 3. Verification and completion

- [ ] 3.1 Run focused API tests, `make installation-query-check`, `make validate` and `make spec-check`, and measure the generated query once against production Prometheus (read-only, at most 2 s and 1 MiB); verify each result is recorded.
- [ ] 3.2 Run `openspec archive aggregate-installation-counts-in-backend --yes` in the same pull request; verify the archive succeeds and `make spec-check` passes.
