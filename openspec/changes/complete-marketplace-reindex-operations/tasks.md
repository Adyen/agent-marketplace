# Tasks

## 1. Benchmarks and baseline

- [x] 1.1 Add 200-registry benchmarks for the observation cycle (all unchanged, injected resolver, SQLite on disk), configuration classification, and due-observation selection, using only APIs present on the base commit; capture `before.txt` on the base commit with `-benchtime 2s -count 5 -benchmem` and keep it as evidence outside the repository

## 2. Worker metrics and logs

- [x] 2.1 Add tests bound to every `marketplace-worker-telemetry` "Worker signals distinguish operational conditions" and "Worker telemetry is low-cardinality and secret-free" scenario; verify they fail on the base commit for missing metrics
- [x] 2.2 Implement the worker metrics set, refresh outcome classification with the `refresh finished` log, host-budget delay counting, runtime-source candidate outcome and restart-required reporting, and per-registry gauge removal; verify the 2.1 tests and `make test TEST_PACKAGES="./pkg/marketplace/..." TEST_FLAGS="-count=1"` pass

## 3. Metrics exposure and deployment

- [x] 3.1 Add tests bound to the two "Worker processes serve worker metrics on the operations listener" scenarios; verify they fail on the base commit
- [x] 3.2 Serve worker metrics from `serve --role worker` on the operations listener and register them once in `--role all`; verify the 3.1 tests pass
- [x] 3.3 Expose the operations port on the Helm worker Deployment and scrape worker pods from the ServiceMonitor; verify with the chart's existing template check and document it in the chart README

## 4. Real-process coverage

- [x] 4.1 Add e2e tests that start real `serve` processes for the two "A running serve process reports configuration replacement outcomes" scenarios and the "Two worker processes probe and refresh a changed registry once" scenario, on SQLite and on PostgreSQL when `POSTGRES_DSN` is set; verify they fail on the base commit and pass after section 3

## 5. Documentation and measurement

- [x] 5.1 Update the reindex-and-monitor how-to, the health and metrics reference, and the operator configuration reference to list each worker series and log record and how to interpret them; verify every claim against the source
- [x] 5.2 Rerun the section 1 benchmarks into `after.txt`, compare with `benchstat`, and record the delta

## 6. Archive

- [ ] 6.1 `openspec archive complete-marketplace-reindex-operations --yes`
