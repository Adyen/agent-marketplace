## Why

The efficient-go inspection identified several user-visible hot paths where benchmark coverage is either missing or too small to guide future performance work. Adding representative benchmarks now makes optimization claims measurable and prevents regressions in `sync`, `status`, `list`, TUI filtering, registry discovery, and harness materialization.

## What Changes

- Add benchmark coverage for resolver selection/deduplication scenarios, including default/required all, tag selection, cross-layer excludes, local selections, and TUI-facing `ResourcesSelected`.
- Add benchmark coverage for catalog loading/discovery, config merge/load/edit paths, command row builders, cache GC, and local warm-cache clone/pull behavior.
- Expand materialization and harness-compilation benchmarks for large mixed resources, dry-run drift checks, symlink sync/pruning, skill frontmatter handling, support-file copying, artifact materialization, fragment merging, existing JSON merge, and compile/convert bundle flows.
- Add TUI benchmarks for large row building, cold/warm filtering, full view recomputation, and visible-row rendering.
- Standardize benchmark commands around repeated `go test -run '^$' -bench ... -benchtime 2s -count 5 -benchmem` runs and `benchstat` comparison.
- Do not change production behavior as part of this proposal; benchmark-driven optimizations should be separate follow-up changes with before/after evidence.

## Capabilities

### New Capabilities

- `performance-benchmark-coverage`: Defines required benchmark coverage for performance-sensitive agent-marketplace paths and the measurement workflow for future optimizations.

### Modified Capabilities

- (none — no user-facing runtime behavior changes)

## Impact

- Affected code: benchmark/test files under `internal/resolver`, `internal/materialize`, `internal/tui`, `internal/catalog`, `internal/config`, `internal/cache`, `cmd/agent-marketplace`, and `pkg/harness-compilation`.
- No runtime API changes, no new runtime dependencies, and no expected behavior changes.
- Developer workflow impact: performance changes should reference the relevant benchmark baseline and `benchstat` comparison before claiming improvement.
