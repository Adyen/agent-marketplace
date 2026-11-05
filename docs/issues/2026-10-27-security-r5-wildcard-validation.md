# R5 / R53: Wildcard Exclusion Validation

## Disposition

Partial R53/F136 work under #765, initially based on main
`bdb3bac4b95c0a7b0b49957253b7438555940bef`. That base already includes
#958's independent exact-locator parsing optimization. No new admission
policy, syntax, limits, dependencies, or user decision. The later
`27c8d6fa` integration has a separate paired matrix because it changes
Marketplace service packages in the config runtime dependency graph.

Replace temporary `strings.Split` slices in the bounded-wildcard branches
of `validateV4Exclusion` and `validateV4ExclusionForSource` with `Cut` and
remaining-slash checks. Keep validators, short-circuit ordering, exact
fallbacks, typed diagnostics, and even the existing unreachable
non-Marketplace diagnostic branch.

## Actual Boundaries

`parseV4Group` runs general validation, and `normalizeV4Tree` later binds
source-kind validation. `SetUserMarketplaceExclusion` calls the latter
directly before locking, reading, or writing user config.

That editor path does not have the general validator's boundedness
precheck. Preserve its wildcard-name acceptance and distinct refusals
instead of silently strengthening it. General `/*` acceptance and
Marketplace rejection, kind/registry ambiguity, empty/unknown source
kinds, dirty bytes, extra separators, and error priority remain intact.
This slice does not change any filesystem authority or publication path.

## Measurement

Go 1.27.3, Darwin/arm64, Apple M4 Max, native `CGO_ENABLED=0` test
binaries, default benchmark suffix `-16`. Baseline fixture tests, vet,
lint, and build pass before freezing definitions. Capture the first
actual before trial before editing production; run the remaining
before/after trials in `before, after, after, before` order repeated
three times. Six samples per variant across 33 cases: 396 retained rows.

Only production `internal/config/v4.go` differs between frozen source
maps. The new test and benchmark definitions are identical. Baseline
allocation profiling identifies both split call sites; the resulting
allocation reductions match removing two slices per exclusion.

The 33-case matrix contains eighteen direct validator shapes, six public
registry-wildcard cases, six existing qualified-wildcard cases, and three
unaffected runtime-adapter controls. Public cases use 1, 256, and 1,024
distinct Marketplace registry selectors with required/optional includes
and a retained command. These cases time YAML parsing and normalization,
not acquisition, config reads, or directory path-metadata operations.
Direct checks have no filesystem or network work.

### Original-Base Public Results

| Shape, 1,024 exclusions | Before allocations | After allocations | Bytes saved |
| --- | ---: | ---: | ---: |
| Qualified `registry/kind/*`, required | 20,492 | 18,444 | about 64 KiB |
| Registry `registry/*`, required | 20,492 | 18,444 | about 32 KiB |

Optional-selection cases also save 2,048 allocations. At 256 exclusions
both shapes save 512 allocations; at one exclusion they save two.
The 1,024 cases reduce allocation counts by 9.99%, independently of
#958's already-present savings.

No public-path speed-up claim. At 1,024 exclusions, qualified required
parsing is 7.737 to 7.757 ms (p=1.000); qualified optional is 30.79 to
30.72 ms (p=0.485). Registry required is 7.629 to 7.724 ms (p=0.310);
registry optional is 30.51 to 30.70 ms (p=0.589). Retain unfavorable
point estimates. The qualified required 256 case is nominally 4.56%
faster (p=0.041), a weak isolated signal across the matrix, not a headline.
Other public timing differences are not significant.

### Original-Base Direct Checks And Controls

Valid kind/registry checks drop from 16 bytes/one allocation to zero.
Qualified and long-registry checks drop from 32 bytes/one allocation to
zero. Whole-wildcard and exact-locator controls remain zero-allocation.
Refusal cases retain their error construction allocations but avoid
the split allocation.

Short kind checks are 17.11% faster in general validation and 40.30%
faster in source-kind validation; qualified checks are 13.26% and
14.28% faster (all p=0.002, n=6). Long-registry timings are not
significantly different. Neither exact/whole-wildcard timings nor
runtime-adapter controls show a significant timing change. Adapter
allocations are unchanged.

Do not headline a geomean, infer full CLI/acquisition latency, pool
unrelated datasets, claim a universal speed-up, or close R53/R5.

### Integrated Runtime-Dependency Base

Main `27c8d6fafada0376b6d466b55dd231c4dfe40e5f` changes Marketplace
installation metrics. Both `pkg/marketplace/api` and
`pkg/marketplace/config` are actual config runtime dependencies, not
merely test dependencies. Refresh the entire 33-case paired matrix
instead of treating the old binary inputs as unchanged.

`integrated/` retains another six alternating one-second pairs: 396
observations on that base, independently compiled and fingerprinted.
Both compiled variants pass all preservation/public/editor tests.
Before production matches `git show 27c8d6fa:internal/config/v4.go`;
after remains the same delivered Cut implementation. Test and benchmark
definitions remain identical to the original matrix.

The allocation result persists: at 1,024 exclusions, both qualified and
registry public parsing save 2,048 allocations, from 20,492 to 18,444
(-9.99%), about 64 KiB and 32 KiB respectively. Optional cases save
the same allocations. At 256 exclusions the saving remains 512; at one
exclusion it remains two.

All twelve public timing comparisons are nonsignificant. The 1,024
qualified required case is 7.688 to 7.601 ms (p=0.394), qualified
optional is 30.88 to 30.56 ms (p=0.310), registry required is 7.663 to
7.669 ms (p=1.000), and registry optional is 30.50 to 30.58 ms
(p=0.818). No public-path latency claim.

Adapter controls show no significant timing change and unchanged
allocation counts. Whole-wildcard controls are nonsignificant. The
unchanged general exact-locator control is nominally 1.19% faster
(p=0.026), a weak isolated signal, not an exact-parser optimization
claim; the source-specific exact control is nonsignificant. Preserve
all observations, including noisy refusal measurements.

### Rejected Gemini Eager-Lookup Proposal

Gemini proposes hoisting `_, isKind := v4Kinds[first]` before the
source-kind branch and reusing its result instead of the anonymous
lookup and the Marketplace single-component lookup. Those existing
branches are mutually exclusive: neither path repeats that first-kind
lookup. Hoisting it adds a lookup to qualified wildcard validation.

Measure the exact proposal on published head
`038c62fb83441931bc21560c36498afeb0b40071`, before the service
integration. `lookup-probe/` retains ten alternating one-second pairs
over six source-validator shapes: 120 observations. Both variants pass
preservation tests, and no production/test/benchmark source changes
are adopted.

| Source-validator shape | Delivered | Eager proposal | Comparison |
| --- | ---: | ---: | --- |
| Qualified wildcard | 79.38 ns | 87.02 ns | +9.63%, p<0.001 |
| Extra slash | 91.72 ns | 97.56 ns | +6.37%, p=0.003 |
| Invalid kind | 145.6 ns | 151.1 ns | +3.78%, p=0.001 |

Kind, registry, and long-registry timing comparisons are nonsignificant.
All six cases have unchanged byte and allocation counts. Reject the
proposal based on these measured costs, not a universal slowdown or
public-latency claim.

Reconstruct its before source from the published head. For after, add
`_, isKind := v4Kinds[first]` immediately after the source-specific
`Cut`; replace the sole anonymous `!ok` lookup with `!isKind`; replace
the Marketplace `if _, isKind := v4Kinds[first]; isKind` with
`if isKind`. All other source inputs remain identical.

## Reproduction And Provenance

Raw `before.txt`, `after.txt`, `benchstat.txt`, and `manifest.json` are in
`security-r5-wildcard-validation-2026-10-27/` and its `integrated/` and
`lookup-probe/` subdirectories. Each manifest records its own base,
Go version, every config-package Go input and module-file SHA-256,
frozen binary SHA-256, benchmark arguments/names, ordered trials, and
each trial's complete stdout hash.

Reconstruct before production from `git show <base>:internal/config/v4.go`
and use an overlay while keeping the published tests and benchmarks.
All shared validators and exact-locator fallback code are identical.
Frozen legacy validator bodies match that Git base byte-for-byte after
renaming their functions. No source snapshots or relabeled hashes are
needed because measured test definitions remain unchanged.

Compile each variant with `CGO_ENABLED=0 go test -c ./internal/config`.
Run its binary with:

```text
-test.run=^$
-test.bench=^Benchmark(V4WildcardValidation|ParseV4RegistryWildcardValidation|ParseV4RequiredWildcardsCPU|V4WildcardAdapterCPU)$
-test.benchtime=1s -test.benchmem -test.count=1
```

Capture six alternating samples of each, then run
`benchstat before.txt after.txt`. Setup and final result assertions are
outside `b.Loop`. Baseline allocation profiling uses the required
1,024 registry-wildcard subbenchmark with `-test.memprofile` and
`go tool pprof -alloc_objects -list=validateV4Exclusion`.
Profile timings and the one-iteration fixture smoke are not evidence
rows in the paired dataset. For the rejected proposal, restrict the
benchmark to source variants of kind, registry, qualified, long-registry,
extra-slash, and invalid-kind shapes; use ten pairs rather than six.

All 912 observations are accounted for as three distinct datasets:
396 original-base, 396 integrated-base, and 120 rejected-proposal.
Never pool them, relabel their bases, or add their allocation deltas.
The Git base pins other runtime dependency source; config-package and
module-file hashes do not claim to enumerate every transitive source.

## Preservation And Negative Controls

- Complete typed validation-error replay against both former split-based
  validators, including raw-byte values and arbitrary source/source-kind
  strings. Shared validators are deliberately unchanged, not independent
  tests of their own implementation.
- Public parsing covers all six resource kinds and all three source
  kinds, whole/registry/qualified wildcards, required protection,
  retained authored exclusions/history, and typed refusal priority.
  Directory unit setup uses a quoted test-owned temporary directory.
- The real user Marketplace editor first successfully writes a wildcard
  while retaining its prior command, then refuses malformed direct
  inputs with exact typed code/path/message and unchanged file bytes,
  mode, modification time, and directory contents.
- Seven semantic mutations fail direct replay without compile failures.
  Six also fail public parsing/editor tests: both arity checks,
  boundedness precedence, kind/registry ambiguity, empty-base behavior,
  and an incorrectly added general precheck in the editor path.
- Unknown/empty source-kind admission is caught only by direct replay
  in this suite; normal parsed source bindings cannot supply that kind.
  Do not call it a public mutation witness.
- Both native 15-second byte-fuzz variants pass. Before-overlay replay,
  public parsing, and editor checks pass in 0.387 seconds; focused race
  checks pass in 1.654 seconds. Offline `go mod tidy -diff` passes.

The negative-control harness initially assumed UTF-8 failure output.
Adversarial raw-byte diagnostics exposed that assumption. It now retains
raw output bytes and escapes only its diagnostic rendering; no tests,
production code, or performance observations change.

## Consumer Gates And Review

The first-base selected config/pipeline/resolver/TUI validation, vet, zero-issue
lint, and build pass. Strict specs pass: 68 items, 1,277 scenarios,
2,088 annotations, two active changes. All 81 terminal journeys plus
four utilities pass with zero skips.

Combined E2E passes in 724.925 seconds: 3,501 invocations, all 115
requirements, zero gaps, all three required refusal witnesses executed
and passed, plus shellcheck. CI owns the wider full root suite; scoped
local validation is not full-suite proof.

After main `aa21ea03` incorporates the already-decided diagnostic-consent
change, fresh selected config/pipeline/resolver/telemetry/TUI validation,
vet, zero-issue lint, and build pass again. Strict specs pass with 68
items, 1,279 scenarios, 2,090 annotations, and two active changes.
All 81 terminal journeys plus four utilities pass with zero skips.
Fresh combined E2E passes in 858.730 seconds: 3,502 invocations, all
114 requirements, no gaps, all three required refusal witnesses executed
and passed, plus shellcheck. The removed flag explains the changed
requirement count, not a waived witness.

Config test dependencies do not include the modified telemetry package;
all measured config/module inputs and definitions remain unchanged.
Retain the original benchmark base and earlier gate results rather than
relabeling them as this later integration run.

After integrating `27c8d6fa` and refreshing the paired measurements,
fresh selected config/pipeline/resolver/telemetry/TUI and Marketplace
API/config validation passes, with vet, zero-issue lint, and build.
Strict specs pass with 68 items, 1,281 scenarios, 2,098 annotations,
and two active changes. All 81 terminal journeys plus four utilities
pass with zero skips. Fresh combined E2E passes in 728.205 seconds:
3,502 invocations, all 114 requirements, zero gaps, all three required
refusal witnesses executed and passed, plus shellcheck.

Host adversarial `open-code-review-delegate` covers all seventeen own files,
including default-excluded tests/docs/raw data and private harnesses.
Verify all 912 observations, source reconstruction, unit/flag/sample
counts, and source/binary/trial/benchstat identities before every push
and handoff.

R53, R5, and #765 remain partial. Leave the clean PR open, unqueued,
with auto-merge disabled.
