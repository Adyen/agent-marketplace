# Tasks: agent-marketplace-pf99.4

## Operator configuration
- [x] `pkg/marketplace/config/config.go` — v1 operator config types, `LoadConfig`,
  defaults, validation.
- [x] `pkg/marketplace/config/stores.go` — `OpenStores` factory for metadata and blob
  backends.
- [x] `pkg/marketplace/config/config_test.go` — valid/invalid/default tests, credential
  leak tests, limit bounds, duplicate IDs, path traversal.

## License propagation
- [x] `pkg/marketplace/analyze/options.go` — add `License` to `Source`.
- [x] `pkg/marketplace/artifact/builder.go` — add `License` to `BuildResult` and populate
  `base.Manifest.License`.

## Git acquisition
- [x] `internal/cache/registry.go` — expose `ValidateGitURL`, `ValidateGitRef`,
  `ResolveRemoteRef`, and `GitEnv` for reuse.
- [x] `pkg/marketplace/acquire/acquire.go` — isolated `Clone` with workspace creation,
  askpass, environment allowlist, URL/ref validation, and limit enforcement.
- [x] `pkg/marketplace/acquire/acquire_test.go` — valid clone, invalid URL/ref, limit
  rejections, context cancellation.
- [x] `pkg/marketplace/acquire/acquire_bench_test.go` — `BenchmarkClone`.

## Registry ingestion
- [x] `pkg/marketplace/artifact/revision.go` — `CanonicalRegistryRevisionPayload`.
- [x] `pkg/marketplace/ingest/ingest.go` — `BuildRegistryRevision` with catalog load,
  collection discovery, per-resource artifact build, policy/allowlist application, and
  manifest construction.
- [x] `pkg/marketplace/ingest/ingest_test.go` — end-to-end, `index-only`, and license
  rejection cases.
- [x] `pkg/marketplace/ingest/ingest_bench_test.go` — `BenchmarkBuildRegistryRevision`.

## Worker/scheduler
- [x] `pkg/marketplace/worker/worker.go` — `Refresh`, `EnsureMarketplace`,
  `EnsureRevocations`, `Run`, `Reindex`, durable job lease loop, and context-aware
  cancellation.
- [x] `pkg/marketplace/worker/worker_test.go` — end-to-end refresh, license rejection,
  index-only mode, previous-revision preservation, `Reindex`.
- [x] `pkg/marketplace/worker/worker_bench_test.go` — `BenchmarkRefresh`.

## CLI
- [x] `cmd/agent-marketplace/serve.go` — `serve --role worker` and `serve reindex`.
- [x] `cmd/agent-marketplace/serve_test.go` — missing config, unsupported role, reindex
  config errors.

## Verification
- [x] `go test ./pkg/marketplace/...` and `go test ./cmd/agent-marketplace/...`.
- [x] `go build ./...` and `go vet ./...`.
- [x] `gofmt -w` on edited files.
- [x] `openspec/changes/agent-marketplace-pf99.4/{.openspec.yaml,proposal.md,design.md,tasks.md}`.
- [x] Commit all work to `worker/agent-marketplace-pf99.4`.
