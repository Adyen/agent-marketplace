# Design

## Context

See proposal.md for motivation. The worker already logs observation outcomes, refresh requests, host-budget delays, stale-generation discards, and configuration candidate outcomes. The only Prometheus registry lives in the API server and is served on the operations listener, which a `--role worker` process never opens. Observation and refresh state is durable in the metadata store, shared by every worker.

## Goals / Non-Goals

- Goals: one worker metrics set, served by every process that runs the worker; signals that separate the five operational conditions in `specs/marketplace-worker-telemetry`; real-process tests for configuration replacement and multi-process coordination; 200-registry benchmarks.
- Non-goals: due-probe delay histograms, pending queue age, or any other signal that needs a new metadata-store query; webhooks or provider APIs; new authentication; API revocation hot reload.

## Decisions

- **Process-local instruments, one owner.** The worker owns a small metrics type holding its counters, histograms, and gauges, registered once into a Prometheus registry passed in by `serve`. In `--role all` the API server registers it into its existing operations registry; in `--role worker` `serve` starts an operations-only HTTP server with `/metrics` and nothing else. Alternative considered: store-derived gauges computed by the API at scrape time. Rejected because configuration rejection and host-budget delay are process-local events that never reach the store, and split-mode workers would still be unscrapeable.
- **Per-registry gauges are set by the process that did the work.** Last successful observation, last successful index, and consecutive probe failures are set from the observation record each process just wrote. With several workers each process reports what it last saw; operators aggregate with `max by (registry)`. This avoids a store change. Series are deleted when a registry leaves the runtime configuration.
- **Refresh outcome is decided once.** Refresh processing classifies each finished job as `succeeded`, `failed`, or `superseded` at its single exit point, records the counter, duration histogram, and the `refresh finished` log together, and reads the trigger from the job payload (`unknown` when absent). Host-budget refusal and lease conflicts release the job without a finished outcome.
- **Configuration candidate outcome comes from the runtime source.** The runtime source reports `applied`, `rejected` (once per distinct rejected content, matching its log), and `apply_failed`, and the restart-required state, through a narrow callback or interface supplied by the worker, so the config package takes no Prometheus dependency.
- **Label sets are closed.** Every label value is a constant from the metric's documented set or a registry ID taken from configuration; free-form inputs map to `unknown`.
- **Multi-process tests use real binaries.** The end-to-end tests start `serve` processes from the e2e module against a local Git HTTP server and read each process's `/metrics`. They run on SQLite by default and on PostgreSQL when `POSTGRES_DSN` is set. Two processes on one SQLite file is a test topology only; the SQLite deployment guidance stays single-process.

## Risks / Trade-offs

- [A worker-only process now binds `operations_listen`, which can collide with an API sharing a network namespace] → documented in the release note, operator configuration reference, and Helm chart; Kubernetes pods and Compose services have separate namespaces.
- [Instrumentation adds work to the observation hot path] → 200-registry benchmarks measured before and after; budget is 10%.
- [Per-registry gauges diverge across workers] → documented aggregation with `max by (registry)`.

## Migration Plan

No data migration. Rolling back removes the worker `/metrics` and the chart's worker scrape; no state depends on them.
