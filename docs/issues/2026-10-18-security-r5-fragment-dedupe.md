# Security R5: Fragment Entry Deduplication (R28)

## Scope

This independent slice starts at main `3c13f497` while R42 PR #742 is queued.
PR #742 merges before integration gates; the slice is rebased onto
`ef28e6db`. The merger source, compiler fixtures and relevant compile/IR/
policy/validation dependencies are unchanged across that rebase.
Frozen R28/A89/F65 (`01a0a06b-ae6b-751c-ade3-ad5aa601353c`) concerns repeated
hook-entry serialization/comparison in `artifact.MergeFragments`. The
frozen ledger/source index remain unchanged from `2f55f85f`.

Only opt-in generated-fragment accumulation changes internally. No D1-D6
decision, new ceiling, truncation, deduplication policy or ownership rule is
introduced. R43's handwritten JSONC parse/patch work is separate.

## Confirmed Cost And Change

For each incoming object, `appendUniqueObjects` previously marshalled it
and every existing object repeatedly through `equalJSON`. Real hook
compilers contribute one rule per fragment, so a temporary set inside one
append would still repeatedly encode the growing destination prefix.

One JSON fragment group now retains an exact-encoding index for each
accumulated object array. A small state tree follows exact object keys,
avoiding joined-path ambiguities, pointer identity and digest collisions.
The index stores the same `encoding/json` byte equality as before.
An indexed destination is already known to contain objects, so later
appends also avoid rechecking that prefix.

Every replacement discards that key's state subtree, including after
empty/scalar/mixed arrays, objects or ancestor replacement. Initial
destination duplicates are never removed. Serialization failures remain
unequal, as before. State is private to one merge group/invocation; there
is no persistent cache or index for non-deduplicating groups.

The same public merger serves SDK `Convert`/`Materialize`, CLI aggregate
publication and native handwritten-entry retention. Root MCP definition
conflicts keep their special comparison path. Non-object last-wins rules,
fragment ordering, policy/mode metadata, text merging, output bytes and
refusal behavior retain their existing contracts. No writes are added.

## Measurements

Go 1.27.1, darwin/arm64, Apple M4 Max. Identical benchmark source/fixtures,
sequential `-benchtime 2s -count 5 -benchmem` runs with no concurrent local
validation. Fixtures are actual Devin hook compiler outputs: one fragment
per hook, including the compiler's real reconciliation metadata.

The timed public `MergeFragments` call includes fragment classification,
defensive JSON validation, JSON decode, merge and final serialization.
Compilation, fixture creation, expected-output preflight, disk and network
are excluded. These are not end-to-end CLI or filesystem timings.

Observed medians:

| Public merger fixture | Before ns/op | After ns/op | Before B/op | After B/op | Before allocs/op | After allocs/op |
|---|---:|---:|---:|---:|---:|---:|
| 16 distinct hooks | 159,812 | 53,980 | 103,260 | 63,193 | 2,964 | 1,197 |
| 128 distinct hooks | 8,246,694 | 423,385 | 3,677,420 | 583,509 | 138,337 | 9,408 |
| 512 distinct hooks | 138,374,236 | 1,711,243 | 52,685,184 | 2,376,789 | 2,126,521 | 37,480 |
| 512 duplicate hooks | 1,635,937 | 1,396,194 | 1,842,879 | 1,744,717 | 40,483 | 36,407 |

The 512-distinct fixture is about 81x faster, with about 95.5% fewer
allocation bytes and 98.2% fewer allocations. The 16/128-distinct fixtures
are about 3x/19.5x faster. The duplicate-heavy fixture improves about 15%
in timing. These timing comparisons have `p=0.008`, `n=5`.

Small/non-deduplicating controls retain allocation counts. A single
fragment measures 3,191 to 3,248 ns/op (`p=0.183`) with unchanged median
3,865 B/op and 73 allocations. The non-deduplicating 128-hook control
measures 342,897 to 348,083 ns/op (`p=0.222`) with unchanged 8,098
allocations; bytes measure 462,629 to 463,189 (`p=0.151`). These timing
differences are not significant in this sample, not proof of zero overhead.

All samples use `n=5`; retained benchstat warns that five samples do not
establish its 95% confidence intervals. Raw data and ASCII-normalized
tables: `docs/issues/security-r5-dedupe-2026-10-18/`.

An additional candidate allocation profile confirms remaining allocation
is dominated by ordinary JSON decode/validation work. The index also
retains exact encodings for unique entries until the group finishes:
lower allocated bytes per operation are not a peak-heap or memory-cap
claim. The profile is diagnostic, not another before/after benchmark.

```sh
CGO_ENABLED=0 go test ./pkg/harness-compilation/artifact -run '^$' \
  -bench '^BenchmarkFragmentDedupeCompiledHooks$' \
  -benchtime 2s -count 5 -benchmem
```

## Preservation And Negative Controls

- Ten authored public-merger cases pass on the original and candidate:
  reordered keys, contribution order, initial duplicate preservation,
  empty/scalar array replacement, object/ancestor replacement, distinct
  paths with slash-containing keys, nested array order, numeric encoding
  (including signed zero), and non-deduplicating accumulation.
- Those cases compare complete artifact values, including exact formatted
  bytes and mode/metadata, and repeat each merge to catch leaked state.
- Structured Rapid comparisons pin the original pairwise merge oracle.
  Both algorithms pass 1,000 cases at seed 28; the candidate also passes
  2,000 cases at seed 29. Generated paths and type replacements exercise
  live cache reuse and invalidation; root MCP conflicts retain existing
  dedicated controls rather than being restated in this oracle.
- A public-merger allocation guard with 256 real compiled hook fragments
  fails on the original scan: 538,707 allocations exceed its generous
  65,536 regression budget. It passes after the fix. The budget is only a
  test assertion and never limits admitted input; 512-hook fixtures merge
  fully.
- Removing subtree invalidation fails all four empty-array, scalar-array,
  object and ancestor reset controls.
- Rebuilding the set on every append fails the allocation guard (313,533
  allocations), proving the test needs reuse across real fragment calls,
  not merely faster comparisons inside a helper. Both mutations are
  removed.

Fresh integrated short-mode tests pass for all harness-compilation
packages, catalog and materializer, plus vet, zero-issue CI lint and
build. Six CLI consumer regressions pass for independent/shared native
hooks, committed lock preservation, additive selection, ownership,
review output and payload-free logging. Artifact-package races pass,
including 300 Rapid cases at seed 29. Strict spec checks pass: 60 items,
1,034 scenarios and 1,656 annotations; archive audit passes.
Combined E2E passes with 2,927 invocations, 113/113 command requirements
and all three required refusal witnesses.

Host OCR-delegation adversarial review covers all nine intended files:
the production merger and eight default-excluded tests/documents/raw-data
files. Zero files are skipped and no actionable findings remain.
These are scoped local gates, not a full root-suite, vendor-native or
released-client container validation claim. Review and tests do not
prove absence of other defects.

## Remaining Work

The confirmed R28 object-array accumulation cost is addressed, not all
JSON equality, hook processing or #434. Other equality/patching paths,
read/decoding budgets, aggregate/execution limits, repeated-work candidates
and D1-D6 retain their separate obligations. The group-local index adds
live state proportional to indexed encodings; no global processing or
memory bound follows from this optimization.
