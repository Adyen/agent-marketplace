## ADDED Requirements

### Requirement: Optimizations are benchmark-evidenced
Performance changes to benchmarked hot paths SHALL include repeated before/after benchmark evidence and SHALL preserve normal test correctness.

#### Scenario: Before and after benchmark evidence is captured
- **WHEN** an optimization changes a benchmarked hot path
- **THEN** the implementation MUST include a repeated baseline and repeated after-run using the same `go test -run '^$' -bench ... -benchtime 2s -count 5 -benchmem` command
- **AND** the implementation MUST compare the runs with `benchstat`

#### Scenario: Correctness validators remain green
- **WHEN** an optimization is complete
- **THEN** the touched package tests MUST pass
- **AND** `go test ./...` MUST pass before the change is considered complete

### Requirement: Materialization filesystem work is reduced safely
The system SHALL reduce redundant filesystem work in materialization, pruning, symlink sync, and support-file copying without weakening user-owned file protections.

#### Scenario: Dry-run materialization remains non-mutating
- **WHEN** `sync --check`, `status`, or TUI drift inspection invokes materialization in dry-run mode
- **THEN** the optimized materialization path MUST NOT write, remove, rename, or overwrite files outside temporary benchmark fixtures
- **AND** the dry-run report MUST preserve the same written/removed/diagnostic semantics as before the optimization

#### Scenario: User-owned files remain protected
- **WHEN** stale generated entries are pruned from a harness output directory containing user-owned regular files or directories
- **THEN** the optimized prune path MUST NOT delete or overwrite those user-owned entries
- **AND** generated symlinks/artifacts that are stale MUST still be reported and removed according to existing sync semantics

#### Scenario: Unchanged support files are not redundantly copied
- **WHEN** a skill support-file tree contains files that are already present and unchanged in the compiled cache
- **THEN** support-file copying SHOULD skip rewriting those unchanged files
- **AND** changed or missing support files MUST still be copied with their expected contents and permissions

### Requirement: Warm cache and discovery paths avoid unnecessary work
The system SHALL reduce unnecessary warm-cache git work and legacy discovery allocation pressure while preserving freshness semantics for commands that require refresh.

#### Scenario: Inspect-style commands avoid unnecessary git refresh
- **WHEN** a command uses inspect or cached catalog semantics and a usable warm cache exists
- **THEN** the command SHOULD avoid unnecessary `git fetch`/reset work
- **AND** explicit refresh, update, and sync refresh paths MUST retain their existing freshness behavior

#### Scenario: Legacy skill discovery parses frontmatter efficiently
- **WHEN** legacy skill discovery scans many `SKILL.md` files
- **THEN** frontmatter parsing SHOULD avoid reading or splitting more file content than needed to extract metadata
- **AND** malformed skill reporting and tag extraction MUST remain behaviorally equivalent

### Requirement: Interactive and editor paths reduce avoidable allocations
The system SHALL reduce avoidable allocations in TUI row/filter operations and large config editor operations without changing visible behavior.

#### Scenario: TUI filtering remains functionally equivalent
- **WHEN** a user filters resources in the TUI by name, tag, or registry
- **THEN** the optimized filter path MUST return the same matching rows as the current implementation
- **AND** warm-cache filtering SHOULD reduce per-keystroke allocation pressure in the benchmarked warm filter cases

#### Scenario: TUI row construction preserves row metadata
- **WHEN** TUI rows are built for large mixed catalogs with drift, local excludes, same-name resources, and multiple registries
- **THEN** optimized row construction MUST preserve row availability, scope, registry, harness, drift, and local exclude metadata
- **AND** it SHOULD reduce allocation pressure or latency in `BenchmarkBuildRowsLargeMixed`

#### Scenario: Config editor operations preserve YAML semantics
- **WHEN** resources are added to and removed from a large YAML config
- **THEN** optimized editor operations MUST preserve existing comments, ordering behavior, and idempotency guarantees covered by tests
- **AND** they SHOULD reduce latency or allocation pressure in `BenchmarkEditorAddRemoveLargeYAML`
