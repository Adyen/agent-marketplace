## Why

Three correctness-critical packages have visible coverage gaps:

- `internal/collection` sits at **0.0%** statement coverage despite implementing
  recursive collection expansion, cycle detection, and cross-registry resolution.
  Every `sync`/`status` invocation goes through `collection.Expand`, so untested
  branches here translate directly into silent user-visible bugs.
- `internal/cache` has `GarbageCollect` and `activeRegistryHashes` at **0%**,
  meaning the code that deletes cache directories on disk is not exercised.
- `internal/config` is missing coverage for the `Source` helper methods and the
  path-based loader variants (`LoadConfigFromPath`, `LoadLocalConfigFromPath`).

This change closes those gaps without modifying any production behavior.

## What Changes

- Add `internal/collection/collection_test.go`, `expand_test.go`, and
  `expand_bench_test.go` to drive coverage to ≥80% and introduce
  `BenchmarkExpand` alongside the hot path.
- Add `internal/cache/gc_test.go` covering `GarbageCollect` under all error,
  dry-run, and active/inactive branches plus `gc_bench_test.go` with
  `BenchmarkActiveRegistryHashes`.
- Add `internal/config/source_test.go` covering every `Source` enum value for
  `Precedence`, `IsMachineScope`, `IsWritable`, `String`. Extend the existing
  `loader_test.go`, `config_test.go`, and `merge_test.go` with cases for the
  path-based loaders, `SkillPath`, and `RegistrySelectionKey`.
- Commit strategy: one commit per package
  (`test(collection): ...`, `test(cache): ...`, `test(config): ...`).

No production code is modified.

## Capabilities

### New Capabilities

- (none — this is a test-only change)

### Modified Capabilities

- (none — no spec-level behavior changes)

## Impact

- Affected code: `internal/collection/**`, `internal/cache/**`,
  `internal/config/**` — test files only.
- No new runtime dependencies. Benchmarks remain in-tree per AGENTS.md
  performance policy and will run under `go test -bench`.
- Coverage targets: `internal/collection` ≥80%, `internal/cache` ≥75%,
  `internal/config` ≥75%. Full `make test` continues to pass.
