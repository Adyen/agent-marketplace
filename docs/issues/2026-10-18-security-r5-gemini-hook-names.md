# Security R5: Gemini Hook Names (R42)

## Scope

This slice starts independently at main `ab3ab76c`, while PR #736 is queued.
PR #736 merges while measurements run; this slice is then rebased onto
main `299bf2ab`. The loader source and relevant ingest/IR/JSON/policy
dependencies are unchanged across the rebase.
Frozen R42/F106 (`01a0cecf-842e-75ee-af73-5b23f99b44d6`) concerns repeated
Gemini hook-name allocation. The frozen ledger and source index remain
unchanged from `2f55f85f58a3fe3caca4965095bd90f8a1d82518`.

Only collision bookkeeping changes. No D1-D6 decision, new input ceiling,
truncation, hook execution or naming policy is introduced.

## Confirmed Cost And Change

The public Gemini `LoadHooks` loader validates/decodes JSON, sorts events,
flattens legacy/native groups, and allocates catalog resource names.
Every repeated event/type previously restarted the suffix search at 2,
formatting and probing names already assigned by earlier rules.

The existing name map now stores positive next-suffix cursors instead of
Boolean occupancy. Zero still means unused. Each newly generated name
starts at cursor 2, so a later literal base reserves the same namespace.
The global occupancy loop remains: a name such as
`pretooluse-command-2` can come from a literal `command-2` type before a
`command` rule tries suffix 2.

Names never disappear during a load, so skipped lower suffixes cannot
become free. Each suffix candidate belongs to one base, and each base
remembers earlier collisions. Expected naming work is proportional to
rules and generated/probed name bytes rather than repeated prefixes.
There is still only one invocation-local map and no persistent cache.

Historical canonical-event names, native IR event/source, rule/event
order, global collisions, defaults, typed payloads, matchers, zero timeout,
group/rule extras, warnings and malformed-input behavior retain their
existing paths. `LoadBundle`, registered SDK ingestion and catalog
discovery all reach the same loader. It performs no writes.

## Measurements

Go 1.27.1, darwin/arm64, Apple M4 Max. Identical fixtures/benchmark source
before and after; sequential `-benchtime 2s -count 5 -benchmem` runs without
concurrent local validation. Benchmarks include JSON validation/decoding
and IR construction. Fixture creation, disk and network are excluded.
The bundle fixture additionally includes normal bundle discovery/loading;
none of these numbers is end-to-end CLI or materialization latency.

Observed largest-fixture medians:

| Public loader fixture | Before ns/op | After ns/op | Before B/op | After B/op | Before allocs/op | After allocs/op |
|---|---:|---:|---:|---:|---:|---:|
| Flat: 2,048 hooks | 139,904,898 | 11,119,347 | 108,600,775 | 11,922,251 | 5,991,364 | 196,576 |
| Native group: 2,048 hooks | 139,745,240 | 10,383,605 | 108,182,535 | 11,507,363 | 5,972,980 | 178,199 |
| Bundle: 512 native hooks | 10,857,109 | 3,511,681 | 9,221,757 | 3,727,371 | 357,109 | 63,570 |

The 2,048-hook fixtures are about 12.6x/13.5x faster with about 89% fewer
allocation bytes. The bundle fixture is about 3.1x faster and uses about
60% fewer bytes. Timing comparisons for these fixtures have `p=0.008`.

Small-case tradeoffs remain visible. One flat hook measures 7,430 to
7,579 ns/op (+2.0%, `p=0.024`), with unchanged median 7,502 B/op and
133 allocs/op. One grouped hook has unchanged allocation count (178) and
no significant timing difference (`p=0.095`). The one-hook bundle retains
384 allocations. There is no claim that every workload gets faster or
that distinct-base map memory cannot increase.

All timing samples use `n=5`; retained benchstat warns that five samples
do not establish its 95% confidence intervals. Raw samples and
ASCII-normalized tables:
`docs/issues/security-r5-gemini-2026-10-18/`.

```sh
CGO_ENABLED=0 go test ./pkg/harness-compilation/ingest/gemini -run '^$' \
  -bench '^BenchmarkGeminiHookNames(Bundle)?$' \
  -benchtime 2s -count 5 -benchmem
```

## Preservation And Negative Controls

- Fourteen authored flat/grouped cases pass on the original and candidate:
  empty hook maps, defaults, pre-existing numeric suffixes, nested suffixes,
  canonical aliases, case/space normalization and type-case distinction.
  The public bundle loader retains the same resource keys, and source
  bytes remain unchanged.
- Structured Rapid comparisons pin the original restart-scan oracle and
  independently authored historical event spelling. They verify names,
  rule ordering, typed payload/provenance, native group extras and warning
  counts. Both algorithms pass 1,000 cases at seed 42.
  The candidate also passes 2,000 cases at seed 43.
- A 512-hook public-loader allocation guard fails before the fix:
  337,940 allocations exceed its generous 65,536 regression budget.
  It passes after the fix. This test budget never limits accepted input;
  the larger 2,048-hook fixtures also load fully.
- Replacing the occupancy loop with a single probe produces duplicate
  names in both reserved-suffix cases. Those controls fail as intended,
  and the mutation is removed.

Fresh scoped short-mode tests pass for all harness-compilation packages,
catalog and materializer, plus vet, zero-issue CI lint and build. Six CLI
consumer regressions preserve independent/shared native hooks, committed
locks, historical hook evidence, additive selection, ownership and review.
Focused races pass, including 300 Rapid cases at seed 43. Strict spec
checks pass: 59 items, 1,026 scenarios and 1,638 annotations.
Combined E2E passes with 2,907 invocations, 113/113 command requirements,
all three required refusal witnesses, and compatibility shell checks.

Host OCR-delegation adversarial review covers all nine files: the
production loader plus eight default-excluded tests/documents/raw-data
files. Zero files are skipped and no actionable findings remain.
These are scoped local gates, not a full root-suite, vendor-native or
released-client container validation claim. Review and passing tests do
not prove absence of other defects.

## Remaining Work

The confirmed R42 repeated suffix-scan cost is addressed, not all Gemini
processing or #434. File reads, decoding/JSON budgets and broader aggregate
or execution limits retain their separate frozen obligations. Other
repeated-work candidates and D1-D6 remain outstanding. No global processing
or memory bound follows from this optimization.
