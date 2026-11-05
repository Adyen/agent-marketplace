# R5 Allocation-Free Exclusion Kind/Name Parsing

## Scope

Independent partial R53/F136 progress initially measured from main
`027056e3`, after #944 and #945. During PR review, main merged #947's
request-reference optimization as `6e7f0134`; its integration is measured
separately. R53, R5, #434, and performance track #765 remain partial. The
frozen ledger/source index does not change.

Replace the intermediate `strings.Split` slice in `v4ExclusionKindName`
with standard-library `Cut`/`Contains`, retaining the original single
`TrimSuffix`. No grammar, validation, ordering, scope, required protection,
re-inclusion, wildcard policy, metadata, diagnostics, acquisition/lock, or
publication behavior changes. No new limits, deadlines, caches, or indexes.

The helper's private quirks stay intact: for example, `tools/skill/*`
returns `("tools", "skill")`, whereas `tools/skill/item` returns two empty
strings. This helper is not a new Marketplace locator validator.

## Method

Initial-stage raw evidence is in `security-r5-exclusion-kind-2026-10-25/`:
`before.txt`, `after.txt`, `benchstat.txt`, and `manifest.json`.

- Go 1.27.3, darwin/arm64, Apple M4 Max; `GOMAXPROCS=16`, `GOGC=100`,
  `GOMEMLIMIT=off`, `CGO_ENABLED=0`, offline toolchain/modules.
- Six alternating before/after and after/before trials, one second per
  case: 34 identities, six samples per variant, 408 rows.
- Initial original production blob: `25e41226c3fd3242ab8212b574cd38e0721a2bb2`.
- Initial optimized production blob: `2aaef3a99e11187b3c2012c1543d067bfb3f8d27`.
- New benchmark blob: `a50913c5da1e1f331191b7aa0c988ab11592fc59`.
- Compile original/delivered binaries with identical tests and benchmark
  definitions; the physical-path baseline overlay replaces only `v4.go`.
- Verify source/binary fingerprints throughout the run, every row's
  identity/units/values, six samples per case, and byte-exact regeneration
  of the retained benchstat table.

Three-sample exploratory probes are separate and not pooled into the final
evidence. The complete paired dataset retains large timing outliers on
both variants. Allocation reductions, not a general speed-up, are the
primary result. P-values are nominal, not adjusted across the matrix.

All public timed paths are CPU-only. Parsing, directory/fixture setup,
include-tree reads, and network acquisition are excluded. New benchmarks
assert results after `b.Loop`; reused active-selection/lock benchmarks
retain identical older in-loop assertions in both variants.

The matrix includes six helper shapes; public Git/directory adaptation at
1/256/1,024 skills with zero/half/all exclusions and a retained command;
active-selection and repeated-decision controls; wildcard adaptation;
Marketplace lock hydration; and accepted user-layer Marketplace
exclusion-only adaptation at 1/256/1,024 exact locators, retaining an
unrelated Git command and requiring no acquisition lock.

## Initial Results

All sampled helper calls drop from one allocation to zero:

| Helper Shape | Before B/op | After B/op | Before Allocs/op | After Allocs/op |
|---|---:|---:|---:|---:|
| Global / kind wildcard | 16 | 0 | 1 | 0 |
| Exact / long exact | 32 | 0 | 1 | 0 |
| Qualified exact private input | 48 | 0 | 1 | 0 |
| Extra-component private input | 64 | 0 | 1 | 0 |

Selected public-adapter medians:

| Case | Before B/op | After B/op | Before Allocs/op | After Allocs/op |
|---|---:|---:|---:|---:|
| Git, 1,024 skills / 512 excluded | 1,082,697.5 | 1,049,930 | 4,190 | 3,166 |
| Git, 1,024 skills / 1,024 excluded | 1,211,763.5 | 1,146,226 | 5,216 | 3,168 |
| Directory, 1,024 skills / 512 excluded | 1,082,698 | 1,049,929.5 | 4,190 | 3,166 |
| Directory, 1,024 skills / 1,024 excluded | 1,211,762.5 | 1,146,227 | 5,216 | 3,168 |
| Marketplace, 1,024 exact exclusions | 358,544.5 | 309,393 | 2,101 | 1,077 |

Full-exclusion Git/directory fixtures save about 64 KiB and 2,048
allocations per call; half-exclusion fixtures save about 32 KiB and 1,024
allocations. Marketplace saves about 48 KiB and 1,024 allocations. Small
B/op rounding/seed variation is retained rather than replaced with the
theoretical slice size.

Healthy no-exclusion Git/directory and Marketplace lock-hydration
allocation counts do not change. Active-selection fixtures save two
allocations, repeated decisions save one, and wildcard fixtures save two.
The complete byte/count table is retained.

| Timing Context | Before | After | Interpretation |
|---|---:|---:|---|
| Git, 1,024 skills / 1,024 excluded | 380.8 us | 367.7 us | Not significant, p=0.589 |
| Git, 1,024 skills / 512 excluded | 340.5 us | 336.3 us | Not significant, p=0.818 |
| Marketplace, 1,024 exact exclusions | 149.2 us | 130.8 us | Not significant, p=0.065 |
| Directory, 1,024 skills / 1,024 excluded | 383.9 us | 348.2 us | Nominal -9.31%, p=0.041 |
| Directory, 1,024 skills / 512 excluded | 362.0 us | 321.4 us | Nominal -11.23%, p=0.009 |
| Marketplace lock hydration | 24.08 us | 24.07 us | Not significant, p=0.818 |

Directory comparisons are supporting observations amid multiple
comparisons and timing outliers, not the headline. Other public timing
controls show no significant regression. No public Git/Marketplace
speed-up, universal latency, geomean, parsing, I/O, or processing-budget
claim is made.

Allocation profiles locate the old split under `v4ExclusionKindName`;
the delivered profile no longer attributes allocation there. Compiled
helper disassembly contains the old `strings.genSplit` call only before
the change. Profile total bytes are not compared because operation counts
differ; paired B/op is the per-operation evidence.

## Integrated Results After #947

While the first head was in CI, main merged #947 as `6e7f0134`, changing
both request interpreters from value maps to request-reference maps.
Merge that upstream work without rewriting history and repeat the full
measurement. Do not credit those separate request-storage savings to
this parser change.

The four additional artifacts are under
`security-r5-exclusion-kind-2026-10-25/integrated/`. This is another 408
rows, not a replacement or pool with the original stage:

- Fresh baseline: `6e7f0134d8d85bc06d41b9c35c51e30f1646e55d`.
- Baseline production blob: `691425dc80642c87000ecf427cb613ca59c79ea2`.
- Integrated optimized blob: `efa7f80a8a771082972eb4bb1bd920695e584296`.
- Benchmark definition blob stays
  `a50913c5da1e1f331191b7aa0c988ab11592fc59`.
- Same Go/environment, 34 identities, six alternating one-second trials.
  The baseline overlay is from the fresh main, not the optimized merge
  commit. Both variants include #947 and identical preservation/benchmark
  definitions. Their only production difference is this parser.
- Both variants pass the same preservation and 1,000 replay cases,
  including upstream request-copy/allocation controls. New source/binary
  identities, units, counts, names, and byte-exact stats are verified.

The allocation win remains on top of #947:

| Integrated Case | Before B/op | After B/op | Before Allocs/op | After Allocs/op |
|---|---:|---:|---:|---:|
| Git, 1,024 skills / 512 excluded | 869,502.5 | 836,734.5 | 3,165 | 2,141 |
| Git, 1,024 skills / 1,024 excluded | 998,565 | 933,031 | 4,191 | 2,143 |
| Directory, 1,024 skills / 512 excluded | 869,501 | 836,732.5 | 3,165 | 2,141 |
| Directory, 1,024 skills / 1,024 excluded | 998,564 | 933,031.5 | 4,191 | 2,143 |
| Marketplace, 1,024 exact exclusions | 358,338.5 | 309,187.5 | 2,100 | 1,076 |

The incremental parser savings are still about 64 KiB / 2,048
allocations for full-exclusion Git/directory adaptation, 32 KiB / 1,024
for half-exclusion, and 48 KiB / 1,024 for Marketplace. All six helper
shapes remain zero-byte/zero-allocation. Healthy no-exclusion and
Marketplace lock-hydration allocation counts remain unchanged.

**Integrated timing is too noisy for a credible latency estimate.** All
samples are retained, including unfavorable medians. For example,
full-exclusion Git is 838.6 us to 1,283.6 us (p=0.065), half-exclusion Git
is 723.0 us to 885.0 us (p=0.485), and Marketplace is 412.3 us to 343.7 us
(p=0.937). Broad intervals and non-significance do not establish unchanged
latency or rule out a regression. This stage supports the independently
observed allocation reduction, not a latency-preservation or speed-up
claim. The initial stage's directory timing observations are not promoted
to claims about the integrated code.

Fresh integrated mutation controls pass again, including the same
private-only versus public-consumer distinction. Focused races including
the upstream request-copy controls pass in 3.662 seconds. Fresh profiles
and nonempty compiled-helper disassembly confirm the removed parser split.

The integrated validation exposed a reproducible TUI test-lifecycle
failure, not a runtime assertion failure. An upgrade-notice test called
`Init`, which eagerly starts its progressive snapshot worker, then
returned without consuming/joining that worker. The next test changed
HOME/config loaders, allowing the orphan to populate a Git cache while
that next fixture was being removed.

The warning-only test passes ten isolated repetitions on each config
variant, but the two-test notice/warning pair reproduces the cleanup
failure twice in five repetitions. Repair the upstream notice fixture,
not the warning assertion or production scheduling: reuse the existing
isolated directory-source fixture and register cleanup that executes only
the startup snapshot command and drains its stages to closure before
restoring HOME/loaders. Keep first-frame and zero-release-request
assertions; do not execute the deliberately blocked upgrade command.
No sleeps, timeout increases, cleanup retries, assertion weakening, or
production TUI change is introduced.

With that test-only repair, the pair passes 20 repetitions on each config
variant, and the fresh selected validation passes without changing its
assertions. The earlier failures remain recorded rather than called
successful runs. All integrated gates now pass:

- Offline module tidy, selected config/pipeline/resolver/TUI validation,
  vet, zero-issue lint, and build.
- Strict specs: 66 items, 1,199 scenarios, 1,966 annotations, two active
  changes; all 62 terminal journeys plus four utilities, zero skips.
- Fresh combined E2E: 1,021.068 seconds, 3,427 invocations, all 113
  requirements, no gaps, all three refusal witnesses ran/passed, and
  shellcheck.

The TUI test-only repair does not change the measured config benchmark
definitions or production variants. Final host adversarial review covers
all 15 own files, zero skipped, including both separate 408-row datasets,
their identities and timing qualifications, the independent oracles, and
the fixture-lifecycle repair.

## Preservation And Negative Controls

Both implementations pass identical manual private-input examples and
public selector tests for all six resource kinds, with exact/wildcard
cases and an unrelated retained kind. Public Marketplace exclusion-only
fixtures verify locator order, endpoint identity, no unnecessary
acquisition, a retained Git command, and unchanged configuration bytes.
Existing required/re-inclusion/directory/Marketplace witnesses also pass.

The existing 1,000-case effective-exclusion replay now calls the frozen
split-based test parser, not the changed production helper. This prevents
a shared parser defect from contaminating actual and expected results.
Raw-byte fuzzing against that oracle passes 349,047 executions in a
15-second run, including slash/suffix edge cases, NUL, and invalid UTF-8.

Four overlay mutations fail manual/oracle witnesses. Wrong exact names
and a missing wildcard default also fail the public runtime-selector
tests. Dropping the single suffix trim and accepting extra slashes fail
private-input witnesses; no public acceptance is invented for malformed
inputs merely to label those controls end-to-end. Focused races pass in
3.051 seconds.

All initial local gates pass on base `027056e3`:

- Explicit offline `GOWORK=off go mod tidy -diff`: no output or dependency
  file changes.
- Selected config/pipeline/resolver/TUI validation, vet, zero-issue lint,
  and build. This is not a local full-root-suite claim.
- Strict specs: 66 items, 1,199 scenarios, 1,966 annotations, two active
  changes.
- All 62 terminal journeys plus four utilities, zero skips, after a frozen
  offline dependency install without downloads/lock changes.
- Fresh combined E2E/configuration validation: 788.001 seconds, 3,427
  invocations, all 113 requirements, no gaps, all three required refusal
  witnesses executed/passed, and included shellcheck.

Host adversarial `open-code-review-delegate` accounts for all ten own
files, including excluded tests/docs/raw evidence and the manifest.
Both helper callers, private grammar equivalence, independent oracles,
public kind/Marketplace controls, all 408 rows, source/binary identities,
and timing qualifications are inspected. Zero files are skipped and no
actionable finding remains. Private measurement/control harnesses are also
reviewed; these checks do not prove the absence of all defects.

## Reproduce

Use identical test/benchmark definitions on each production variant:

```sh
go test ./internal/config -run '^$' \
  -bench '^(BenchmarkV4ExclusionKindNameCPU|BenchmarkV4SparseExclusionAdapterCPU|BenchmarkV4DirectoryRequestReuseCPU|BenchmarkV4ActiveExclusionAdaptation|BenchmarkV4WildcardAdapterCPU|BenchmarkV4MarketplaceLockHydration|BenchmarkV4RequestReuseChurnCPU|BenchmarkV4MarketplaceExclusionKindCPU)$' \
  -benchtime=1s -count=6 -benchmem
benchstat before.txt after.txt
```

For the recorded interleaving, overlay `v4.go` from `027056e3` for the
baseline, compile each binary once with `go test -c`, then alternate them
for six trials with `-test.count=1`. Match the manifest settings and verify
all 34 identities and six samples per variant.
For the separate integrated stage, use `6e7f0134` as the baseline and its
manifest identities; keep the two stages' outputs separate.
