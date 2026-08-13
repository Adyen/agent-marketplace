## ADDED Requirements

### Requirement: Resolver benchmark coverage

The repository SHALL include benchmarks for resolver workloads that exercise large catalogs, selection policies, deduplication, excludes, and local overrides.

#### Scenario: Large resolver workloads are measurable

- **WHEN** resolver benchmarks are listed with `go test -list '^Benchmark' ./internal/resolver`
- **THEN** benchmarks MUST exist for default or required all-resource selection, tag selection across large catalogs, cross-layer excludes, local selections, and `ResourcesSelected`

#### Scenario: Resolver benchmarks report allocations

- **WHEN** resolver benchmarks are run with `go test ./internal/resolver -run '^$' -bench . -benchmem`
- **THEN** each benchmark MUST execute successfully and report allocation metrics

### Requirement: Materialization benchmark coverage

The repository SHALL include benchmarks for materialization workloads used by `sync`, `sync --check`, `status`, `list`, and TUI drift inspection.

#### Scenario: Materialization dry-run workloads are measurable

- **WHEN** materialization benchmarks are listed with `go test -list '^Benchmark' ./internal/materialize`
- **THEN** benchmarks MUST exist for large mixed-resource dry runs, artifact symlink synchronization, skill symlink synchronization, skill frontmatter insertion, and recursive support-file copying

#### Scenario: Materialization benchmarks cover synced and stale outputs

- **WHEN** materialization benchmarks are run with stale, missing, and already-synced fixture outputs
- **THEN** the benchmarks MUST complete without mutating files outside their temporary directories

### Requirement: TUI benchmark coverage

The repository SHALL include benchmarks for TUI row construction, filtering, view recomputation, and visible-row rendering.

#### Scenario: TUI list interaction workloads are measurable

- **WHEN** TUI benchmarks are listed with `go test -list '^Benchmark' ./internal/tui`
- **THEN** benchmarks MUST exist for large mixed `buildRows`, cold and warm `applyFilter`, full `recomputeView`, and visible-row list rendering

#### Scenario: Filter benchmarks distinguish cold and warm caches

- **WHEN** TUI filter benchmarks are run
- **THEN** the benchmark set MUST include at least one case where searchable row caches are empty and one case where they are pre-populated

### Requirement: Catalog and config benchmark coverage

The repository SHALL include benchmarks for catalog discovery/loading and config processing paths that are part of command and pipeline startup.

#### Scenario: Catalog workloads are measurable

- **WHEN** catalog benchmarks are listed with `go test -list '^Benchmark' ./internal/catalog`
- **THEN** benchmarks MUST exist for `LoadFS` HCC ingestion, mocked `LoadRegistries` orchestration, and legacy discovery fallback

#### Scenario: Config workloads are measurable

- **WHEN** config benchmarks are listed with `go test -list '^Benchmark' ./internal/config`
- **THEN** benchmarks MUST exist for large `MergeLayers`, large config loading, large registry URL validation policy sets, repository root lookup, and editor add/remove operations

### Requirement: Command and cache benchmark coverage

The repository SHALL include benchmarks for command row-building paths and cache maintenance paths that can affect user-visible CLI latency.

#### Scenario: Command row builders are measurable

- **WHEN** command package benchmarks are listed with `go test -list '^Benchmark' ./cmd/agent-marketplace`
- **THEN** benchmarks MUST exist for list row construction, status row/table construction, and a local-registry `sync --check` path

#### Scenario: Cache maintenance workloads are measurable

- **WHEN** cache benchmarks are listed with `go test -list '^Benchmark' ./internal/cache`
- **THEN** benchmarks MUST exist for garbage-collection dry runs over many cache directories and warm local repository clone-or-pull behavior

### Requirement: Harness-compilation benchmark coverage

The repository SHALL include benchmarks for harness-compilation artifact and bundle operations that are transitively used by agent-marketplace materialization.

#### Scenario: Harness materialization workloads are measurable

- **WHEN** harness-compilation benchmarks are listed with `go test -list '^Benchmark' ./pkg/harness-compilation/...`
- **THEN** benchmarks MUST exist for large artifact materialization, fragment merging, existing JSON merge, and large bundle compile or convert flows

#### Scenario: Harness benchmarks avoid external services

- **WHEN** harness-compilation benchmarks are run
- **THEN** they MUST use in-memory filesystems, temporary directories, or synthetic IR resources instead of remote services

### Requirement: Benchmark workflow documentation in tasks

The implementation tasks SHALL prescribe repeated benchmark runs and `benchstat` comparison for future optimization claims.

#### Scenario: Benchmark command workflow is explicit

- **WHEN** implementers read `tasks.md`
- **THEN** it MUST include commands using `go test -run '^$' -bench ... -benchtime 2s -count 5 -benchmem` and `benchstat before.txt after.txt`

#### Scenario: Benchmarks are not treated as optimizations

- **WHEN** this change is implemented
- **THEN** production code MUST NOT be changed unless needed only to make benchmark-only test fixtures compile
