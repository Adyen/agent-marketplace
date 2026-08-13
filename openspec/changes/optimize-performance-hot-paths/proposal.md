## Why

The newly added benchmark suite shows several user-visible hot paths with high latency and allocation costs in `sync`, `sync --check`, `status`, TUI filtering/row construction, catalog discovery, and config editing. Optimizing these paths now turns the benchmark evidence into concrete latency improvements while keeping the existing behavior and resource-safety invariants intact.

## What Changes

- Optimize harness materialization and pruning paths by reducing repeated filesystem scans, hashing work, and slice membership checks.
- Optimize support-file copying so unchanged support trees are skipped or copied with less redundant work.
- Avoid unnecessary git refresh work on warm cache paths used by latency-sensitive inspect/list/status flows.
- Reduce catalog discovery allocation pressure, especially legacy `SKILL.md` frontmatter parsing.
- Reduce TUI row-building and filtering allocations for large catalogs.
- Reduce config editor add/remove overhead for large YAML configs where practical.
- Preserve all production behavior, generated output semantics, error reporting, and user-owned file protections.
- Require before/after benchmark evidence with `benchstat` for each implemented optimization.

## Capabilities

### New Capabilities

- `performance-hot-path-optimization`: Defines measurable optimization targets and guardrails for the benchmarked hot paths.

### Modified Capabilities

- (none)

## Impact

- Affected code: `pkg/harness-compilation`, `internal/materialize`, `internal/cache`, `internal/catalog`/`internal/skill`, `internal/tui`, `internal/config`, and potentially command-level plumbing under `cmd/agent-marketplace`.
- APIs: no public CLI or library API changes expected.
- Dependencies: no new runtime dependencies expected.
- Systems: local filesystem, local git cache, and generated harness output behavior must remain compatible and safe for user-owned files.

## Implementation Summary

- Materialization:
  - `internal/materialize` geomean: `-2.84% sec/op`, `-1.10% B/op`, `-3.25% allocs/op`
  - `SyncArtifactLinks/missing_links`: `-21.82% sec/op`, `-15.90% B/op`, `-18.39% allocs/op`
  - `SyncSkillLinks/already_synced`: `-12.56% sec/op`
  - `pkg/harness-compilation` geomean: `+1.93% sec/op`, `-9.61% B/op`, `-18.16% allocs/op`
- Warm cache and discovery:
  - `CloneOrPullWarmLocalRepo`: `-22.83% sec/op`, `-27.35% B/op`, `-25.11% allocs/op`
  - `ParseFrontmatter` geomean: `-48.24% sec/op`, `-75.94% B/op`
  - `DiscoverLegacyFallback` geomean: `+0.73% sec/op`, `-5.42% B/op`, `+0.76% allocs/op`
- TUI:
  - `BuildRowsLargeMixed`: `-28.24%` to `-33.52% sec/op` on mixed-catalog cases
  - `ApplyFilterColdAndWarm` geomean: `-39.35% sec/op`
  - Warm filter cases dropped to `0 B/op` and `0 allocs/op`
- Config editor:
  - `EditorAddRemoveLargeYAML` geomean: `-1.97% sec/op`, `-6.53% B/op`, `-21.35% allocs/op`
