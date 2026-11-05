# Proposal

## Why

The Marketplace worker clones, checks out, ingests, and republishes every configured registry once per `refresh.interval`, even when the tracked ref has not moved. The durable observation state, registry work lease, host budgets, and refresh coalescing from `marketplace-registry-observation` now exist, so the worker can ask Git cheaply whether a registry changed before doing that expensive work.

## What Changes

- Add a bounded remote-ref resolution that reuses the existing acquisition Git path: URL and ref validation, hardened Git arguments, the credential environment, the custom CA, the timeout, output bounds, and redaction. Branches resolve to their tip, annotated tags to the peeled commit, and a full commit needs no remote request. A missing ref is a source failure; a short ref naming both a branch and a tag resolves to the tag, as acquisition does.
- Replace the worker's unconditional periodic refresh in operator-config management mode with source observation. Each registry is observed at a stable per-registry phase of `refresh.interval`. A refresh is requested only when the observed commit differs from the last successfully indexed commit or the startup configuration of that registry changed, when a periodic reconciliation is due, or when an operator runs `serve reindex`.
- Every observation and refresh takes the registry work lease and admission from the deployment-wide host budget, so several workers sharing one metadata store neither duplicate probes nor exceed a host's concurrent operations.
- A failed observation keeps the last successfully published registry head and refresh status, and backs off that registry deterministically, capped at the reconciliation interval.
- `serve reindex` coalesces with an equivalent active refresh instead of queueing a second one.
- The worker logs each observation outcome, refresh request, coalesced trigger, and host-budget delay with registry identifiers and redacted origins, never credentials.
- The reconciliation interval (24 hours) and probe timeout (30 seconds) are code-owned defaults; no operator configuration field is added, and configuration is still read only at startup.

## Capabilities

### New Capabilities

- `marketplace-source-observation`: change-aware observation of registry repositories by the Marketplace worker, covering remote ref resolution, conditional and coalesced refreshes, deterministic scheduling and backoff, multi-worker coordination, and secret-free observation telemetry.

### Modified Capabilities

None.

## Impact

- Marketplace acquisition package (new remote-ref resolution and source host identity) and the worker scheduler, refresh job processing, and `serve reindex` in operator-config management mode.
- Database management mode, the API, metadata-store schemas, and configuration loading are unchanged.
- Operator documentation for `refresh.interval` and `serve reindex` changes to describe observation-driven refreshes.

## Release note

The Marketplace worker now checks each registry's tracked ref before reindexing and skips clone and ingestion when the commit has not changed since the last successful index. `refresh.interval` now sets how often refs are checked, a full reindex still runs at least once a day, and `serve reindex` still forces a refresh.
