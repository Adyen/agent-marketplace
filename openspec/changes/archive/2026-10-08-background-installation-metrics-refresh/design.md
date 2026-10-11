## Context

The installation reader currently serializes search-triggered fetch, parsing and aggregation under a shared mutex. Registry reindexing belongs to a separate worker process and is not the owner of API count freshness.

## Goals / Non-Goals

Goals are non-blocking search, proactive refresh, independently configurable bounded timing, unchanged inventory identity rules and observable refresh health. Non-goals are shared storage, new services, generic schedulers, reindex changes, telemetry query or aggregation changes, UI or public response changes, production deployment and database-authority fixes.

## Decisions

Keep the reader per API process. Split it into `refresh(ctx, registries)`, `lookup(registries)` and `run(ctx, registryIndex)` operations. Lookup has no request context because it does no I/O. A complete immutable snapshot carries counts, registry-origin signature, evaluation time and hard expiry, published through an atomic pointer. Published maps are never mutated.

The serial loop loads authoritative registry metadata before each attempt with a server-owned bounded attempt context, evaluates telemetry, builds privately and publishes only an unexpired candidate. Search uses its own existing registry index to reject mismatches. A capacity-one hint channel wakes the loop without waiting or spawning goroutines. On a hint, metadata reload establishes whether the current snapshot is still compatible and fresh; if it is, no telemetry request occurs. Hints are ignored as a reason to bypass failure backoff. Scheduled proactive attempts still refresh a fresh snapshot. Metadata load errors participate in failure retry, preserving only the originally valid last-good aggregate.

After success, schedule at evaluation plus interval and jitter, or just after an earlier inventory horizon. When a candidate is already expired, treat the attempt as unavailable and retry no sooner than the retry interval after completion. Schedule all failures from completion to avoid slow attempts consuming backoff. Avoid repeated timers at a past horizon. Registry changes during a fetch cannot leak mapped counts because lookup checks the request's signature; a coalesced hint triggers current-metadata recovery.

Store timing defaults in Marketplace operator configuration and reuse its Duration parser. Use optional duration fields to distinguish omitted values from explicit zero, particularly jitter; resolve defaults once through a shared configuration helper usable by YAML-loaded and directly constructed configs. Validate positivity, non-negative jitter and overflow-safe headroom. Unknown YAML fields remain errors. URL absence disables runtime work. Registry `refresh.interval` has no effect on these settings.

`NewServer` builds no goroutines. `Server.Start` starts the worker only after successful listener setup, shares process cancellation and joins it during shutdown. Tests that directly use handlers explicitly refresh or start a cancellable worker. Backend telemetry never gates readiness.

Register three metrics with the existing per-server Prometheus registry: `agent_marketplace_installation_metrics_refresh_total` with outcome `success`, `unavailable` or `invalid-response`; `agent_marketplace_installation_metrics_refresh_duration_seconds`; and `agent_marketplace_installation_metrics_last_success_timestamp_seconds`. Observe once per complete background attempt. Log safe failure category once per attempt; no per-search refresh warning.

Retain existing fetch query, parsing, aggregation, HTTP redirect policy, token lookup and eight-MiB body cap. Use configured timeout in the HTTP client and the worker attempt context. Fresh last-good data survives failures without expiry extension; expired, mismatched and missing snapshots fall back.

## Verification

Write a search regression test with a local telemetry handler held behind a channel. Search must return before release during cold startup, expired refresh and warm proactive refresh; assert exact fallback/count behavior and that cancellation does not abort the worker. Test atomically published maps under concurrent readers, fresh-to-expired failure transitions, registry change during fetch, inventory horizon and publication expiry, serial attempts, coalescing/backoff, independent config defaults/overrides/invalid values, side-effect-free construction, disabled metrics and shutdown cancellation.

Retain existing parser, identity, ranking/filter/pagination and credential tests. Add scenario annotations required by specaudit. For performance, use local deterministic telemetry with a fixed 25-ms delay in a cold installation-ranked search benchmark and compare identical workload on base and candidate. This measures the search handler, not complete worker startup; after the change cold searches intentionally return fallback rather than count availability. Also capture a warm snapshot lookup/search benchmark without telemetry I/O. Use two-second benchmark samples, five runs and allocation reporting; keep inputs identical and retain base/candidate output plus benchstat.

Run focused tests/race checks and actual Marketplace process lifecycle E2E. Final native checks are make validate, make spec-check and strict all-spec validation. Archive the OpenSpec change in the documentation gate.

## Risks / Trade-offs

Proactive refresh increases healthy backend query frequency modestly. API startup can temporarily omit counts. Each API process retains an independent snapshot and jittered cadence. Snapshot max age remains a hard bound even if a fetch or metadata read stalls; headroom validation cannot guarantee refresh completion. Early inventory expiry may cause temporary fallback, but must not create unbounded retry traffic.
