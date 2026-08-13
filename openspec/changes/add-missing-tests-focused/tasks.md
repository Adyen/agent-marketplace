## 1. internal/collection package (0% → ≥80%)

- [x] 1.1 Add `internal/collection/collection_test.go` covering `Label`, `Find`, `DiscoverCollections` (missing dir, empty dir, non-dir entries, valid COLLECTION.yaml, missing YAML file, invalid YAML, missing name), and `parseCollectionFile` error paths.
- [x] 1.2 Add `internal/collection/expand_test.go` covering `Expand` for: empty refs, missing collection, default-source registry, explicit registry with/without ref, unknown registry, Via annotation map (exclusive vs direct), nested `includes` recursion, cycle detection, synthetic entry deduplication per `(url, ref)`, and source/catalogKey/exclude propagation.
- [x] 1.3 Add `internal/collection/expand_bench_test.go` with `BenchmarkExpand` exercising a realistic nested-collection workload.
- [x] 1.4 Verify `go test ./internal/collection/... -cover` reports ≥80% and all tests pass.
- [x] 1.5 Commit: `test(collection): add unit tests and Expand benchmark`.

## 2. internal/cache GC paths (0% on GC functions)

- [x] 2.1 Add `internal/cache/gc_test.go` covering `GarbageCollect`: cache dir missing, cache dir empty, keeps active hashes + removes inactive, dry-run reports without deleting, non-dir entries ignored, nil configs, empty configs, local registries skipped, blank URLs ignored.
- [x] 2.2 Add `internal/cache/gc_bench_test.go` with `BenchmarkActiveRegistryHashes`.
- [x] 2.3 Verify `go test ./internal/cache/... -cover` improves above 75%.
- [x] 2.4 Commit: `test(cache): cover GarbageCollect and activeRegistryHashes`.

## 3. internal/config helpers and path-based loaders

- [x] 3.1 Add `internal/config/source_test.go` exhaustively covering `Source.Precedence`, `Source.IsMachineScope`, `Source.IsWritable`, `Source.String` for every enum value including the unknown/default branch.
- [x] 3.2 Extend `internal/config/loader_test.go` with `TestLoadConfigFromPath*` and `TestLoadLocalConfigFromPath*` (missing file, valid file, invalid YAML, validation error for global).
- [x] 3.3 Extend `internal/config/config_test.go` with `TestSkillPath` (empty Path vs populated Path).
- [x] 3.4 Extend `internal/config/merge_test.go` with `TestRegistrySelectionKey` (git vs local entries, index disambiguation).
- [x] 3.5 Verify `go test ./internal/config/... -cover` improves above 75%.
- [x] 3.6 Commit: `test(config): cover Source helpers and path-based loaders`.

## 4. Wrap-up

- [x] 4.1 Run `make ci-lint`, `make test`, and `make build` — everything green.
- [x] 4.2 Run `go test ./... -cover` and record per-package deltas in the openspec change log.
- [x] 4.3 Leave benchmarks discoverable via `make bench`-compatible naming (`*_bench_test.go`).
