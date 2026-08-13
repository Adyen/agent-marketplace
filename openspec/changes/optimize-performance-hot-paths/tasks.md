## 1. Measurement Setup

- [x] 1.1 Confirm `benchstat` is available locally (`benchstat --help`) or install it with `GOPROXY=direct go install golang.org/x/perf/cmd/benchstat@latest`.
- [x] 1.2 Run and save baseline results for the prioritized benchmarks with `go test -run '^$' -bench 'Benchmark(MaterializeLargeArtifacts|MaterializePruneStale|SyncArtifactLinks|SyncSkillLinks|CopySupportFilesRecursive|CloneOrPullWarmLocalRepo|DiscoverLegacyFallback|BuildRowsLargeMixed|ApplyFilterColdAndWarm|EditorAddRemoveLargeYAML)$' -benchtime 2s -count 5 -benchmem ./pkg/harness-compilation ./internal/materialize ./internal/cache ./internal/catalog ./internal/tui ./internal/config | tee before.txt`.
- [x] 1.3 Keep benchmark output files (`before.txt`, `after.txt`, profiles) uncommitted.

## 2. Materialization and Support Files

- [x] 2.1 Profile `BenchmarkMaterializeLargeArtifacts` and `BenchmarkMaterializePruneStale` with CPU and allocation profiles to identify redundant hashing, file reads, and membership checks.
- [x] 2.2 Optimize `pkg/harness-compilation.Materialize` membership checks and skipped-file lookups so repeated slice scans are replaced with set lookups where measurable.
- [x] 2.3 Optimize stale-file discovery/pruning to avoid unnecessary work while preserving user-owned file protections and deterministic reporting.
- [x] 2.4 Optimize `internal/materialize` artifact symlink sync paths for missing, already-synced, stale, and user-owned regular file cases.
- [x] 2.5 Optimize `internal/materialize` skill symlink sync paths for empty, already-synced, stale, and user-owned directory cases.
- [x] 2.6 Optimize recursive support-file copying to skip unchanged files where safe and still copy missing/changed files with correct contents and permissions.
- [x] 2.7 Add or adjust focused tests for unchanged support-file skips, stale generated file pruning, and user-owned file/directory preservation.
- [x] 2.8 Rerun `go test -run '^$' -bench 'Benchmark(MaterializeLargeArtifacts|MaterializePruneStale|SyncArtifactLinks|SyncSkillLinks|CopySupportFilesRecursive)$' -benchtime 2s -count 5 -benchmem ./pkg/harness-compilation ./internal/materialize | tee after-materialize.txt` and compare with `benchstat before.txt after-materialize.txt`.

## 3. Warm Cache and Catalog Discovery

- [x] 3.1 Profile `BenchmarkCloneOrPullWarmLocalRepo` to separate unavoidable git subprocess cost from avoidable warm-cache checks.
- [x] 3.2 Optimize inspect/cached catalog flows to avoid unnecessary `git fetch`/reset work when a usable warm cache already exists.
- [x] 3.3 Add tests proving refresh/update/sync refresh semantics still perform the required freshness work.
- [x] 3.4 Profile `BenchmarkDiscoverLegacyFallback` and `BenchmarkParseFrontmatter` to confirm legacy frontmatter parsing allocation sources.
- [x] 3.5 Optimize legacy `SKILL.md` frontmatter parsing to avoid reading or splitting full files when metadata is contained in the leading frontmatter block.
- [x] 3.6 Add tests proving malformed skill reporting, tag extraction, and body/source fallback behavior remain unchanged.
- [x] 3.7 Rerun `go test -run '^$' -bench 'Benchmark(CloneOrPullWarmLocalRepo|DiscoverLegacyFallback|ParseFrontmatter)$' -benchtime 2s -count 5 -benchmem ./internal/cache ./internal/catalog ./internal/skill | tee after-cache-catalog.txt` and compare with `benchstat before.txt after-cache-catalog.txt`.

## 4. TUI Interaction Hot Paths

- [x] 4.1 Profile `BenchmarkBuildRowsLargeMixed` to identify allocation sources in per-scope maps, resource keys, row construction, and harness metadata.
- [x] 4.2 Optimize TUI row construction with targeted preallocation and reduced duplicate key/string work while preserving row scope, registry, harness, drift, and exclude metadata.
- [x] 4.3 Profile `BenchmarkApplyFilterColdAndWarm` to confirm warm-filter allocation sources.
- [x] 4.4 Optimize warm TUI filtering by reusing filtered row buffers or filtering via indices/pointers where ownership remains clear.
- [x] 4.5 Add tests proving TUI filtering by name, tag, and registry returns identical rows before and after optimization.
- [x] 4.6 Rerun `go test -run '^$' -bench 'Benchmark(BuildRowsLargeMixed|RecomputeView|ApplyFilterColdAndWarm)$' -benchtime 2s -count 5 -benchmem ./internal/tui | tee after-tui.txt` and compare with `benchstat before.txt after-tui.txt`.

## 5. Config Editor Large YAML Path

- [x] 5.1 Profile `BenchmarkEditorAddRemoveLargeYAML` to identify YAML parse, node traversal, and write-back costs.
- [x] 5.2 Optimize config editor add/remove paths only where the profile shows local, readable improvements.
- [x] 5.3 Add tests proving comment preservation, ordering behavior, and idempotency remain unchanged for add/remove operations.
- [x] 5.4 Rerun `go test -run '^$' -bench 'BenchmarkEditorAddRemoveLargeYAML$' -benchtime 2s -count 5 -benchmem ./internal/config | tee after-config.txt` and compare with `benchstat before.txt after-config.txt`.

## 6. Validation and Wrap-up

- [x] 6.1 Run `go test ./...`.
- [x] 6.2 Run `golangci-lint run ./...` if `golangci-lint` is available locally.
- [x] 6.3 Confirm `git diff` contains focused production optimizations, related tests, and no committed benchmark output files.
- [x] 6.4 Update this OpenSpec task list with completed items and record benchmark deltas in the implementation summary.

## Notes

- Baselines were captured in `before.txt` and `before-cache-catalog.txt`.
- After runs were captured in `after-cache-catalog.txt`, `after-config.txt`, `after-hcc.txt`, `after-materialize-internal.txt`, and `after-tui.txt`.
