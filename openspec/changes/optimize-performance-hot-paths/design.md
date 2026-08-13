## Context

The `add-performance-benchmarks` change added coverage for the hot paths that users feel during `sync`, `sync --check`, `status`, `list`, and TUI interaction. Representative benchmark results show the largest opportunities in filesystem-heavy materialization, recursive support-file copying, warm git cache refresh, legacy catalog discovery, TUI row/filter allocations, and config editor YAML rewrites.

The optimization work must preserve the current safety model:

- generated output remains ephemeral and derived from config
- user-owned regular files/directories are not overwritten or pruned incorrectly
- required/default registry selection semantics remain unchanged
- `sync --check` and dry-run paths must not mutate repository or harness output
- no optimization claim is accepted without repeated before/after benchmark evidence

## Goals / Non-Goals

**Goals:**

- Reduce latency and allocation pressure for the benchmarked hot paths with small, targeted changes.
- Prioritize optimizations that affect multiple user-visible commands: `sync`, `sync --check`, `status`, `list`, and TUI drift/filter flows.
- Keep every optimization independently measurable with existing benchmarks and `benchstat`.
- Preserve filesystem safety around symlinks, user-owned files, stale generated artifacts, and support-file copying.
- Keep changes readable and local to the measured bottleneck.

**Non-Goals:**

- No broad rewrites of the resolver, materializer, TUI model, or harness-compilation architecture.
- No public CLI/API behavior changes.
- No `unsafe`, broad `sync.Pool`, manual `runtime.GC`, or speculative caching without benchmark evidence.
- No benchmark-only changes unless they are needed to support a specific optimization measurement.
- No remote-network benchmarking or dependence on mutable external registries.

## Decisions

1. **Optimize materialization before smaller CPU-only paths**
   - Decision: start with `pkg/harness-compilation.Materialize`, `internal/materialize` symlink sync, pruning, and support-file copying.
   - Rationale: these benchmarks are the highest-latency paths and affect `sync`, `sync --check`, `status`, and TUI drift inspection.
   - Alternative considered: begin with resolver/TUI micro-allocations. Rejected because they are smaller absolute costs and less likely to dominate command latency.

2. **Prefer metadata checks before hashing or copying**
   - Decision: for file sync/copy paths, use cheap file metadata and path-set membership first, and hash/read file contents only when necessary.
   - Rationale: benchmarks show filesystem traversal and content hashing dominate several scenarios; avoiding full reads on unchanged files is the smallest readable improvement.
   - Alternative considered: always maintain persistent manifest state. Rejected for this change because it adds a new state model and rollback concerns.

3. **Preserve user-owned file safety over aggressive pruning**
   - Decision: pruning optimizations must continue to distinguish generated symlinks/artifacts from user-owned regular files and directories.
   - Rationale: existing safety semantics are a core product guarantee and more important than maximal prune speed.
   - Alternative considered: faster broad directory deletion. Rejected because it risks user-owned content.

4. **Separate warm-cache avoidance from update semantics**
   - Decision: optimize list/status/inspect paths to avoid unnecessary git work, but keep explicit refresh/update/sync refresh semantics intact.
   - Rationale: warm `CloneOrPull` is expensive; avoiding it on commands that can use cached catalogs is valuable, while users still need explicit refresh paths.
   - Alternative considered: globally skip `git fetch` when cache exists. Rejected because it could make sync/update semantics stale.

5. **Improve legacy discovery by parsing only what is required**
   - Decision: optimize legacy `SKILL.md` frontmatter parsing before broader catalog ingestion changes.
   - Rationale: `DiscoverLegacyFallback` shows high allocations from parsing many files; frontmatter lives at the top of each file, so full-file splitting is avoidable.
   - Alternative considered: remove legacy fallback. Rejected because compatibility still matters.

6. **TUI optimizations should reuse buffers or avoid copies without changing model semantics**
   - Decision: reduce allocations in filter and row construction with preallocation, cached searchable strings, row-index buffers, or reused slices where safe.
   - Rationale: the TUI should remain responsive on each keystroke; optimizations must not make state ownership unclear.
   - Alternative considered: large model refactor to pointer rows. Rejected unless benchmarks show smaller changes are insufficient.

7. **Config editor improvements are lower priority**
   - Decision: handle editor YAML rewrite overhead after materialization, cache, discovery, and TUI work.
   - Rationale: install/remove operations are less frequent than sync/status/TUI interactions, even though the benchmark shows high allocation cost.

## Risks / Trade-offs

- Filesystem metadata can be insufficient for correctness → fall back to content comparison when size/modtime cannot prove equality.
- Caching can become stale → restrict new caching to command modes with explicit cached semantics or invalidate on input changes.
- Preallocated/reused slices can accidentally retain data → reset length and keep ownership local to the benchmarked function.
- Avoiding git refresh can surprise users expecting freshness → retain refresh behavior for `sync`, `update`, and explicit refresh paths; document command mode boundaries in code/tests.
- Micro-optimizations can reduce readability → require benchmark evidence and keep each patch scoped to one hotspot.

## Migration Plan

- Implement optimizations in small independent patches, one hotspot at a time.
- For each patch, capture a repeated baseline:
  `go test -run '^$' -bench '<target benchmark regex>' -benchtime 2s -count 5 -benchmem <package> | tee before.txt`
- Apply the change, rerun the exact command into `after.txt`, compare with `benchstat before.txt after.txt`, and keep transient result files uncommitted.
- Run focused tests for the touched package plus `go test ./...` before completing the change.
- Rollback is standard git revert for any optimization that regresses correctness or benchmark results.

## Open Questions

- What minimum improvement threshold should justify merging each micro-optimization: percent reduction, absolute latency reduction, allocation reduction, or command-level impact?
- Should cache freshness policy be exposed as a user-facing setting, or remain an internal command-mode distinction?
- Should future work introduce a generated-artifact manifest if directory walking remains dominant after smaller optimizations?
