## 1. Benchmark harness setup

- [x] 1.1 Confirm `benchstat` is available locally or install it with `go install golang.org/x/perf/cmd/benchstat@latest`.
- [x] 1.2 Add reusable package-local benchmark fixture helpers where they reduce duplication without hiding scenario inputs.
- [x] 1.3 Keep all benchmark result files (`before.txt`, `after.txt`, profiles, temporary outputs) uncommitted.

## 2. Resolver benchmarks

- [x] 2.1 Extend `internal/resolver/resolver_bench_test.go` with `BenchmarkResolveDefaultAllLargeCatalog` covering `default:true` and `required:true` all-resource selection at multiple registry/resource sizes.
- [x] 2.2 Add `BenchmarkResolveTagSelectionLargeCatalog` covering many tags per resource and multiple configured wanted tags.
- [x] 2.3 Add `BenchmarkResolveCrossLayerExcludes` covering many resolved resources and large higher-precedence exclude lists.
- [x] 2.4 Add `BenchmarkResolveLocalSelections` covering local includes that are already resolved and local includes that require scanning configured catalogs.
- [x] 2.5 Add `BenchmarkResourcesSelectedTUI` for the per-registry selection path used by TUI row construction.
- [x] 2.6 Verify with `go test ./internal/resolver -run '^$' -bench . -benchmem`.

## 3. Materialization benchmarks

- [x] 3.1 Extend `internal/materialize/materialize_bench_test.go` with `BenchmarkRunDryRunLargeMixedResources` for 100, 500, and 1000 mixed resources across multiple harnesses.
- [x] 3.2 Add `BenchmarkSyncArtifactLinks` covering missing links, already-synced links, stale symlinks, and user-owned regular files.
- [x] 3.3 Add `BenchmarkSyncSkillLinks` covering empty, already-synced, stale symlink, and user-owned directory cases.
- [x] 3.4 Add `BenchmarkEnsureSkillFrontmatter` for small, 100KB, and 1MB skill bodies with and without existing frontmatter.
- [x] 3.5 Add `BenchmarkCopySupportFilesRecursive` for skill support trees with 10, 100, and 1000 nested files.
- [x] 3.6 Verify with `go test ./internal/materialize -run '^$' -bench . -benchmem`.

## 4. TUI benchmarks

- [x] 4.1 Extend `internal/tui/actions_bench_test.go` with `BenchmarkBuildRowsLargeMixed` covering hundreds to thousands of resources across registries, kinds, drift states, local excludes, and same-name resources.
- [x] 4.2 Extend `internal/tui/search_bench_test.go` with `BenchmarkApplyFilterColdAndWarm` covering cold searchable cache, warm searchable cache, no-match, tag-match, registry-match, and broad-match cases.
- [x] 4.3 Add `BenchmarkRecomputeView` covering scope, registry, and status tabs with empty and active filters.
- [x] 4.4 Add `BenchmarkRenderListVisibleRows` covering visible heights of 25, 50, and 100 rows.
- [x] 4.5 Verify with `go test ./internal/tui -run '^$' -bench . -benchmem`.

## 5. Catalog, config, command, and cache benchmarks

- [x] 5.1 Add `internal/catalog` benchmarks for `BenchmarkLoadFSHCCIngest`, `BenchmarkLoadRegistriesMockedDiscover`, and `BenchmarkDiscoverLegacyFallback`.
- [x] 5.2 Add `internal/config` benchmarks for `BenchmarkMergeLayersLargeConfig`, `BenchmarkLoadConfigFromPathLargeYAML`, `BenchmarkValidateRegistryURLsLargePolicy`, `BenchmarkFindRepoRoot`, and `BenchmarkEditorAddRemoveLargeYAML`.
- [x] 5.3 Add `cmd/agent-marketplace` benchmarks for `BenchmarkBuildListRows`, `BenchmarkBuildStatusRowsAndTables`, and `BenchmarkRunSyncCheckLocalRegistry`.
- [x] 5.4 Add `internal/cache` benchmarks for `BenchmarkGarbageCollectDryRun` and `BenchmarkCloneOrPullWarmLocalRepo`.
- [x] 5.5 Verify with `go test ./internal/catalog ./internal/config ./cmd/agent-marketplace ./internal/cache -run '^$' -bench . -benchmem`.

## 6. Harness-compilation benchmarks

- [x] 6.1 Add `pkg/harness-compilation` benchmarks for large artifact `Materialize` workloads, including dry-run, already-synced, and prune-stale cases.
- [x] 6.2 Add `pkg/harness-compilation/artifact` benchmarks for `MergeFragments` with text and JSON fragments at multiple group sizes.
- [x] 6.3 Add `pkg/harness-compilation/artifact` benchmarks for `MergeWithExistingJSON` with managed, user-owned, added, removed, and unchanged entries.
- [x] 6.4 Add bundle-level `Compile` or `Convert` benchmarks for large synthetic skill, command, subagent, hook, and MCP bundles.
- [x] 6.5 Verify with `go test ./pkg/harness-compilation/... -run '^$' -bench . -benchmem`.

## 7. Benchmark evidence and validators

- [x] 7.1 Run a repeated baseline for representative new benchmarks, for example `go test -run '^$' -bench 'BenchmarkResolve(DefaultAllLargeCatalog|TagSelectionLargeCatalog|CrossLayerExcludes|LocalSelections|ResourcesSelectedTUI)$' -benchtime 2s -count 5 -benchmem ./internal/resolver | tee before.txt`.
- [x] 7.2 For any follow-up optimization, rerun the same command to `after.txt` and compare with `benchstat before.txt after.txt`.
- [x] 7.3 Run `go test ./...` to ensure normal tests remain green.
- [x] 7.4 Run `make ci-lint` if `golangci-lint` is available in the local environment.
- [x] 7.5 Confirm `git diff` contains benchmark/test files only and no production behavior changes.
