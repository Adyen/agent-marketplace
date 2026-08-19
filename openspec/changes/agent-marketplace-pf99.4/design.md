# Design: Marketplace bounded indexing worker

## Dependencies

- `agent-marketplace-pf99.2` provides analyzers, canonical artifacts, and source revision
  manifest helpers.
- `agent-marketplace-pf99.3` provides `pkg/marketplace/store`, `pkg/marketplace/job`, and
  atomic `PublishSourceRevision`.

## Decisions

- Operator configuration lives in a separate `pkg/marketplace/config` package and loads a
  dedicated YAML file (`apiVersion: agent-marketplace.io/marketplace/v1`). It is intentionally
  not the same schema as the CLI user config and is not merged with it.
- `Config` is a plain Go struct with custom `Duration` and `Credential` types. Defaults are
  applied in code, then validation rejects invalid drivers, unknown fields, unsafe
  credentials, and path-traversing root paths.
- `OpenStores` constructs metadata and blob stores from `storage` config using an existing
  factory pattern. SQLite metadata and filesystem blobs are the day-one defaults.
- Git acquisition (`pkg/marketplace/acquire`) creates a fresh temporary workspace per clone,
  validates URLs/refs against option/command-injection patterns, and runs `git` with a
  tightly scoped environment and optional `GIT_ASKPASS` helper. Limits are enforced before
  and after clone via repository bytes, file count, and per-file bytes.
- Source ingestion (`pkg/marketplace/ingest`) loads the source catalog per configured root,
  discovers collections, builds each resource artifact, applies the license allowlist and
  acquisition mode (`mirror` vs `index-only`), and assembles the source-revision manifest.
  Catalog errors fail ingestion; warnings are logged and do not block.
- Policy enforcement for rejected resources: a rejected resource still receives a
  `ContentDigest` and `Findings` but has an empty `ArchiveDigest`, so the source manifest
  records the rejection without publishing a downloadable artifact.
- The worker (`pkg/marketplace/worker`) exposes three operations:
  - `Refresh(ctx, deps, source)` — one-shot durable publish of a single source.
  - `Run(ctx, deps, cfg)` — background scheduler that enqueues refresh jobs on startup and
    on each interval tick, then leases and processes jobs with bounded concurrency.
  - `Reindex(ctx, deps, cfg, slugOrID)` — one-shot refresh of one or all sources via
    explicit operator command.
- `serve --role worker` is the only role in this release; `--role api` is rejected.
- `serve reindex` reindexes all configured sources or a single source identified by slug
  or ID.
- Security hardening: no inline secrets in logged config, `git` environment allowlisted,
  refs and paths validated, askpass scripts written to a private temp directory and
  cleaned up, subprocesses tied to context cancellation.
- Atomic publication is delegated to `store.PublishSourceRevision`, which writes blobs
  before committing metadata in a transaction.

## Files

- `pkg/marketplace/config/config.go` and `stores.go` — operator config loader, defaults,
  validation, and store factory.
- `pkg/marketplace/config/config_test.go` — table-driven config validation tests.
- `pkg/marketplace/acquire/acquire.go` — `Clone`, credentials, askpass, validation, limits.
- `pkg/marketplace/acquire/acquire_test.go` and `acquire_bench_test.go`.
- `pkg/marketplace/ingest/ingest.go` — `BuildSourceRevision` and policy helpers.
- `pkg/marketplace/ingest/ingest_test.go` and `ingest_bench_test.go`.
- `pkg/marketplace/worker/worker.go` — `Refresh`, `Run`, `Reindex`, scheduling.
- `pkg/marketplace/worker/worker_test.go` and `worker_bench_test.go`.
- `cmd/agent-marketplace/serve.go` and `serve_test.go`.
- `pkg/marketplace/analyze/options.go` — `License` field on `Source`.
- `pkg/marketplace/artifact/builder.go` — `License` field on `BuildResult` and manifest.
- `pkg/marketplace/artifact/revision.go` — `CanonicalSourceRevisionPayload`.
- `internal/cache/registry.go` — shared `ValidateGitURL`, `ValidateGitRef`,
  `ResolveRemoteRef`, `GitEnv`.
