# R5 / R53: Shared V4 Locator Parsing

## Disposition

Remove the shared locator parser's temporary split slice while preserving
its language, refusal order, blank error outputs, and all consumer behavior.
This is partial R53/F136 work under performance tracker #765, not a new
admission limit or closure of R5.

## Reachability And Preserved Contracts

`parseV4Locator` is shared by `parseV4Includes`, `validateV4Exclusion`,
`validateV4ExclusionForSource`, `LayerEditor.validateLocators`,
`marketplacePinV4Request`, and `LayerEditor.ExcludeMarketplacePin`.

Two `Cut` calls and a remaining-slash check replace only the split and
component-count logic. The second cut's success preserves the former
qualified-shape check. Keep, in order:

1. Clean-locator refusal.
2. Exact two/three-component arity.
3. Kind/name checks.
4. Marketplace name-segment validation.
5. Qualified registry-slug validation.
6. Nonempty-source validation.

Every error still returns three blank strings. Existing callers retain
their typed code, field, authored location, source-kind admission, and
selection semantics. Required protection, ordering, include/exclude replay,
locks, acquisition, publication, native output, and editor staging are not
changed. Private nonempty source strings are not newly normalized.

## Measurement Scope

The same 27 cases run in six alternating before/after pairs, one second
per case: 324 rows per stage, 648 total rows across two separate stages.
Go 1.27.3, Darwin/arm64, Apple M4 Max, native `CGO_ENABLED=0` test binaries,
default benchmark CPU suffix `-16`. Each stage changes only the production
`internal/config/v4.go` input between its frozen binaries.

Six direct cases cover unqualified/qualified names, long names, arity,
invalid names, and clean-input refusal. Nine public cases cover Git,
directory, and Marketplace sources with 1, 256, or 1,024 skill includes and
matching exact exclusions, plus a retained command. Twelve unchanged
controls cover preparsed Git/wildcard adaptation and Marketplace lock
hydration. Construction and assertions are outside timing.

Whole public parsing includes YAML, validation, and rebuilding. Relative
directory fixtures also execute real path-safety filesystem metadata work:
`Lstat`, `EvalSymlinks`, and working-directory resolution. These directory
rows are not disk-free CPU measurements. No case reads configuration or
resource payload files, acquires a registry, or performs network requests.

### Scope Correction

The initial new benchmark comment and private harness interpretation
incorrectly described directory cases as disk-free. Caller tracing found
the metadata checks before publication. Correct the comment before the
integrated binaries; keep timed fixtures unchanged.

The initial manifest explicitly corrects that narrative without changing
raw observations, binary/source identities, or trial hashes. Its original
benchmark definition is retained as
`initial/benchmark-definition.go.txt`, so the measured source SHA remains
reconstructible. The archived source comment is historical, not the current
scope claim. Both stages include the same directory metadata checks.

## Integrated Results

Final measured base:
`0b8284f620a3185ed8ebd56fff597035975bbfa2`, after #953. Its Marketplace
runtime-guard improvement is already present in both variants and is not
credited again.

| Direct parser case | Before | After | Bytes / allocations |
| --- | ---: | ---: | --- |
| Unqualified | 163.4 ns | 153.5 ns (-6.12%) | 32 / 1 to 0 / 0 |
| Qualified | 244.4 ns | 226.8 ns (-7.20%) | 48 / 1 to 0 / 0 |
| Long qualified name | 5.292 us | 5.244 us (not significant) | 48 / 1 to 0 / 0 |
| Extra slash | 56.01 ns | 32.30 ns (-42.33%) | 80 / 2 to 16 / 1 |
| Invalid name | 104.75 ns | 86.88 ns (-17.06%) | 64 / 2 to 16 / 1 |
| Dirty input | 8.646 ns | 8.701 ns (not significant) | 16 / 1 unchanged |

The short valid cases and two post-split refusals have p=0.002, n=6.
Long-name and dirty-input comparisons have p=0.132 and p=0.937. Errors
still allocate; no claim is made that every refusal becomes allocation-free.

| Public parse, 1,024 skills and exclusions | Before | After | Change |
| --- | ---: | ---: | ---: |
| Git | 2.018 ms | 1.976 ms | -2.07% |
| Directory, including path metadata | 2.066 ms | 2.003 ms | -3.05% |
| Marketplace | 2.411 ms | 2.336 ms | -3.09% |

All three large public comparisons have p=0.004, n=6. Each eliminates
3,073 allocations: one per locator parse, with 1,025 includes and 1,024
exclusions validated twice. Save about 96 KiB for Git/directory and
144 KiB for Marketplace. The complete YAML parser still allocates; this
does not make the whole consumer allocation-free.

Single-resource public timings are not significant. Preparsed adaptation
and hydration controls show no significant timing changes or allocation
changes. Retain all observations, including unfavorable medians: hydration
is 21.55 to 22.78 us, p=0.065, and the single directory case is 26.58 to
27.85 us, p=0.394. Non-significance is not proof of latency preservation.

Do not headline a geomean, extrapolate to whole CLI or acquisition latency,
or infer admission budgets from these bounded fixtures.

## Initial Results And Reproduction

Initial base: `3ad0341dacdb1d31894baf357150a220aa40a323`, before #953.
Its 324 rows remain separate under `initial/`; do not pool the stages,
rename the initial base, or add the two stages' allocation deltas.

Initial valid unqualified/qualified parsing also removes one 32/48-byte
allocation; large public cases also remove 3,073 allocations. Initial
public timing comparisons are mostly non-significant; the large
Marketplace row is -2.74%, p=0.026. Keep those weaker observations rather
than describe both stages as a uniform public speed-up.

Manifests fingerprint every config Go source, root dependency files, both
binaries, ordered trial stdout, benchmark names, and flags. To reconstruct
the initial production inputs, use the initial Git base for the before
file and replace only its `parseV4Locator` function with the published
function for the after file. Use the archived initial benchmark-definition
text for that stage's measured new benchmark file. Both stages use
`measured-locator-tests.go.txt` for the measured new test file; other
initial config sources are unchanged. The two test files added by #953
belong only to the integrated package snapshot.

After measurement, CI lint required a tagged switch instead of the
equivalent if/else chain selecting the public test fixture's source kind
(`QF1003`). CodeRabbit then identified that the directory unit fixture
inspected a checkout-relative path; use a quoted, test-owned temporary
directory for that unit case. Only untimed test setup changes; production,
the oracle, and benchmark functions are unchanged. Preserve the measured
test source instead of silently replacing its fingerprint with the final
file, and revalidate the final test form on both production variants.

## Correctness And Negative Controls

- The frozen former parser independently preserves split/arity behavior,
  validator order, all return fields, and error text. Only unchanged
  validators are shared with production.
- Thirty-five raw cases cross three source values, including empty and
  unusual nonempty sources, empty components, malformed arity, all relevant
  name/slug refusals, NUL, invalid UTF-8, controls, and long strings.
- Public witnesses exercise all six kinds across three source kinds and
  include/exclusion typed code/path/message refusals.
- The editor witness rejects malformed locators for all four locator
  operations without changing rendered state or user bytes. A successful
  include proves that the exercised editor can change a selection.
- All five semantic mutations fail direct replay without compilation
  failures. Extra-slash and registry mutations also fail public parsing.
  Missing first separator, blank error outputs, and source-refusal
  precedence are caught only by direct replay in this control suite;
  public fixtures still pass those three mutants.
- Both production variants pass native two-string byte fuzzing:
  359,959 before and 353,370 after executions. These are correctness runs,
  not comparable throughput measurements.
- Focused parser/layer-editor/Marketplace-editor races pass in 1.838
  seconds. Offline root and E2E module-tidy checks pass.
- Nonempty compiled-parser disassembly contains `strings.genSplit`
  before, and no such call after. Separate allocation-space profiles
  support the removed parser allocation; they are not paired samples or
  a claim that all public parsing has no split calls.

## Consumer Validation And Review

All consumer gates pass on the integrated base:

- Fresh selected config/pipeline/resolver/TUI tests pass. The initial
  validation stops on the new test's `QF1003`; after the fixture-only
  tagged-switch repair, `make validate` freshly reruns the affected config
  package, vet, zero-issue lint, and build. Unchanged consumer-package
  results from the first run remain separate, not falsely called reruns.
- The final test form passes against the before production overlay too.
- CodeRabbit's directory-unit isolation follow-up passes the before
  overlay in 0.469 seconds and a fresh affected-config validation in
  7.846 seconds, plus vet, zero-issue lint, and build. Production and
  timed benchmark definitions remain unchanged; both stages' full
  source/binary/trial provenance still verifies.
- Strict specs: 67 items, 1,216 scenarios, 2,002 annotations, two active
  changes.
- All 68 real-terminal journeys plus four utilities, zero skips.
- Combined E2E: 740.415 seconds, 3,455 invocations, all 113 command/flag
  requirements, no gaps, all three required refusal witnesses executed
  and passed, plus shellcheck.

Host adversarial `open-code-review-delegate` covers all 15 own files,
including excluded tests/docs, both raw datasets, both measured-source
snapshots, and the private harnesses. Verify the full 648-row provenance,
stage boundaries, source/binary identities, and reported qualifications
before publication. CI owns the wider full root suite; selected local
validation must not be called full-suite proof.

R53 and R5 remain partial. No new policy or user decision is introduced.
