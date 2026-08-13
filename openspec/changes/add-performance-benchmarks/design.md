## Context

The repository already has targeted benchmarks for a few paths (`resolver.Resolve`, support-file materialization, TUI filtering, legacy skill discovery, collection expansion, and selected config/cache helpers), but the efficient-go inspection found that many user-visible latency paths still lack representative coverage. The highest-risk paths run during `sync`, `sync --check`, `status`, `list`, TUI snapshot loading, TUI keystroke filtering, catalog discovery, and harness-compilation materialization.

This change adds benchmark coverage only. It intentionally avoids optimizing production code until benchmark baselines and future `benchstat` comparisons show a stable improvement opportunity.

## Goals / Non-Goals

**Goals:**

- Add representative benchmarks for every performance-sensitive path identified in the efficient-go inspection.
- Keep benchmarks deterministic, local-only, and independent of external Git/network services.
- Cover multiple input sizes and realistic scenarios so regressions show up on the correct axis: number of registries, resources, harnesses, artifacts, stale files, rows, or config entries.
- Make future performance work data-driven by documenting repeatable `go test -bench` and `benchstat` workflows.

**Non-Goals:**

- Do not change production behavior in this proposal.
- Do not introduce `sync.Pool`, `unsafe`, broad rewrites, or other optimizations without follow-up benchmark evidence.
- Do not require benchmarks to run in normal `make test`; they remain opt-in via `go test -bench`.
- Do not benchmark real remote registries or mutable developer machine state.

## Decisions

1. **Benchmark before optimizing**
   - Decision: add benchmarks as the first change, with no production optimization bundled in.
   - Rationale: efficient-go requires stable before/after evidence; mixing optimization with benchmark creation would make baseline quality unclear.
   - Alternative considered: apply obvious allocation reductions immediately. Rejected because benchmark gaps make the actual impact and priority uncertain.

2. **Use package-local synthetic fixtures**
   - Decision: construct benchmark fixtures in the same package as the hot path, using temp directories, `fstest.MapFS`, synthetic configs, and local bare repositories where subprocess Git cost is intentionally measured.
   - Rationale: this keeps benchmarks deterministic and close to the implementation while avoiding network dependencies.
   - Alternative considered: command-level end-to-end benchmarks only. Rejected because they are useful smoke benchmarks but too noisy to isolate resolver, catalog, materialize, or TUI regressions.

3. **Separate micro-path and scenario benchmarks**
   - Decision: add focused benchmarks for narrow allocation-heavy helpers and broader scenario benchmarks for user-visible flows.
   - Rationale: scenario benchmarks catch regressions in real flows; micro-path benchmarks explain which helper regressed.
   - Alternative considered: only large end-to-end benchmarks. Rejected because failures would be hard to diagnose.

4. **Represent cold and warm paths explicitly**
   - Decision: benchmark both cold setup costs and warmed steady-state where behavior differs, especially TUI search cache, already-synced materialization, and warm registry cache paths.
   - Rationale: these paths have different allocation and latency profiles, and optimizing one can regress the other.
   - Alternative considered: one benchmark per function. Rejected because it would hide important mode-specific costs.

5. **Keep benchmark outputs out of the repository**
   - Decision: tasks will prescribe `tee before.txt`/`after.txt` for local comparison, but generated result files should not be committed.
   - Rationale: the benchmark functions are the regression guard; local result files are transient evidence for reviews.

## Risks / Trade-offs

- Benchmark fixtures may not match production registry scale → Use multiple sizes and include realistic mixed resource kinds, harnesses, tags, and stale-file layouts.
- Filesystem and Git subprocess benchmarks can be noisy → Mark those as integration-style benchmarks, run repeated counts, and compare with `benchstat` before drawing conclusions.
- Adding many benchmarks can increase maintenance burden → Keep helper builders reusable, package-local, and explicit about what each benchmark measures.
- Benchmarks might be mistaken for proof of an optimization → Require future optimization changes to include before/after repeated runs and correctness validators.
