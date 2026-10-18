# Security R5: V4 Adaptation (R52 / R53)

## Scope

This independent slice starts at main `06a6fdbd`, after merged PR #730,
and is rebased onto main `27b29c8a`. Production/test sources are byte-identical
before and after the rebase. Measurements and validation run on October 18.
It addresses two repeated-work portions of frozen R52/F135 and R53/F136.
The [frozen ledger](2026-10-16-security-r5-frozen-ledger.md) and source index
remain unchanged from `2f55f85f58a3fe3caca4965095bd90f8a1d82518`.

These are partial dispositions, not closure of either broad row or #434.
No D1-D6 decision, new ceiling, truncation or incomplete-result policy is
introduced.

## R52: Collection Reachability

Direct request matching already has indexes. The confirmed remaining cost
is the fixed-point walk over all locked resources: a valid child-before-
parent chain requires one full scan per additional reachable collection.

The candidate builds parent-to-child links using the same collection
identity and revision checks, then visits reachable nodes with an
invocation-local worklist. This computes the same least reachability set,
in expected resources-plus-selection-edges work, plus identity-byte
processing, rather than repeated full passes. All-direct inputs need no
adjacency/worklist allocation.

This does not change collection expansion multiplicity. It replaces only
an existing Boolean set calculation. Resource/pin order, direct status,
aliases, complete selected-by metadata, projection pruning, and the first
strict unreachable-resource diagnostic retain their original code paths.
Unseeded cycles remain unreachable; seeded cycles terminate as before.

## R53: Effective Exclusions

Each distinct exact exclusion previously rebuilt and sorted the complete
effective-request snapshot. The candidate computes that snapshot lazily
once and keeps its locator set for reinclusion checks. Wildcard-only and
empty decision streams need no snapshot.

The ordered interpreter is unchanged: required requests survive exclusions,
later includes replace earlier options, wildcard exclusions remain active,
and first-distinct exclusion order and nil/empty results are preserved.
The cache is invocation-local and cannot survive a subsequent edit.

## Measurements

Go 1.27.1, darwin/arm64, Apple M4 Max; same benchmark source/fixtures before
and after, `-benchtime 2s -count 5 -benchmem`, sequential measurements without
concurrent local validation. Each benchmark calls the public adapter:
R52 includes lock codec normalization; R53 includes complete runtime
adaptation. Fixture parsing/encoding, disk and network I/O are excluded.
These are not end-to-end command or file-loading latency measurements.

Observed medians for the largest fixtures:

| Public adapter fixture | Before ns/op | After ns/op | Before B/op | After B/op | Before allocs/op | After allocs/op |
|---|---:|---:|---:|---:|---:|---:|
| R52: 512-collection reverse chain | 3,217,829 | 797,109 | 1,086,415 | 1,112,022 | 12,086 | 12,607 |
| R53: 128 exact exclusions | 18,285,361 | 308,003 | 7,241,233 | 200,392 | 20,019 | 843 |

That is about 4x and 59x faster respectively. R52 uses about 2.4% more
allocation bytes for adjacency/worklist storage. R53 uses about 97.2% fewer
bytes. Common-case costs remain visible: a one-resource lock retains 30
allocations; a one-exclusion adaptation adds one allocation and 48 bytes.
There is no claim that every workload allocates less.

All timing comparisons have `p=0.008`, `n=5`. Raw samples and ASCII-normalized
benchstat tables retain the warning that five samples do not establish its
95% confidence intervals:
`docs/issues/security-r5-v4-2026-10-18/`.

```sh
CGO_ENABLED=0 go test ./internal/config -run '^$' \
  -bench '^BenchmarkV4(LockChain|ExactExclusion)Adaptation$' \
  -benchtime 2s -count 5 -benchmem
```

## Preservation And Negative Controls

- Thirteen independently authored exclusion cases and ten reachability
  cases pass on the original and candidate algorithms.
- Structured Rapid properties compare decision replay and graph closure
  with baseline algorithm oracles. Before the change, both properties pass
  1,000 cases at seed 52; the candidate passes 1,000 at seed 53 and 2,000 at
  seed 52. These are bounded semantic controls, not exhaustive proofs.
- All lock benchmark sizes pass the real YAML decoder and its existing
  byte/node/depth rules. The 512-resource fixture also hydrates twice through
  `LoadConfigFromPath`, retaining configuration/lock bytes, modes and mtimes.
- Restoring per-exclusion replay fails the final allocation regression:
  5,390 allocations exceed the 256 test budget for 64 exclusions. That
  budget guards replay overhead; it never limits admitted configuration.
- Omitting the worklist's parent-revision check fails both strict and
  projected stale-parent cases. Both mutations are removed.

Pre-rebase scoped validation passes for config/lockfile/pipeline short-mode
package tests, vet, zero-issue CI lint and build. Six focused CLI regressions
cover ordered/read-only exclusions, exact lock preservation, updates and
stale staged-lock refusal. Focused config races and strict spec checks pass
(59 items, 994 scenarios, 1,602 annotations). The combined pre-rebase E2E
gate passes: 2,907 invocations, 113/113 command requirements and all three
required witnesses.

The post-rebase candidate passes the same fresh scoped validation, six
CLI regressions and focused races. Strict spec checks pass with 59 items,
996 scenarios and 1,604 annotations. The combined post-rebase E2E gate
also passes: 2,907 invocations, 113/113 command requirements, all three
required witnesses, and compatibility shell checks.

## Gemini Review Follow-Up

[Gemini's capacity-hint suggestion](https://github.com/Adyen/agent-marketplace/pull/736#discussion_r4305988996)
is applied to the invocation-local active-locator map. The already-computed
effective-request count sizes the map; request evaluation, laziness and
selection semantics are unchanged. An empty snapshot still produces a
non-nil map, so it is not replayed for later exclusions.

The original all-excluded fixture leaves this map empty and cannot measure
growth. The additional public-adapter fixture retains 1, 64 or 1,024
requests and excludes one unselected name. Three authored controls verify
the complete ordered selection, exact exclusion and unchanged config.
Controls pass before and after the hint, as do 1,000 Rapid replay
comparisons at seed 53.

Same-fixture measurements compare published `42706507` with the capacity
hint, using the same host/toolchain and five 2-second samples. Parsing,
disk and network remain outside timing:

| Active requests | Before ns/op | After ns/op | Before B/op | After B/op | Before allocs/op | After allocs/op |
|---|---:|---:|---:|---:|---:|---:|
| 1 | 935.9 | 937.6 | 2,800 | 2,800 | 16 | 16 |
| 64 | 35,136 | 33,654 | 115,352 | 111,944 | 321 | 314 |
| 1,024 | 591,314 | 558,503 | 1,837,963 | 1,783,603 | 4,221 | 4,205 |

The 1,024-request fixture uses about 3.0% fewer allocation bytes and is
about 5.5% faster (`p=0.008`). The one-active-request timing is statistically
unchanged (`p=0.690`). The zero-active-map control, one exact excluded
request, retains 2,160 B/op and 12 allocs/op but measures 761.4 to 778.3 ns/op
(+2.2%, `p=0.016`); no claim is made that every shape gets faster.
The five-sample confidence-interval warning is retained.

Raw follow-up data is separate from the initial measurements:
`active-map-before.txt`, `active-map-after.txt` and `active-map-benchstat.txt`
in the same evidence directory.

Fresh follow-up config/lockfile/pipeline scoped validation, zero-issue CI
lint/build, six CLI consumer regressions and focused races pass. Strict
spec checks pass with 59 items, 996 scenarios and 1,604 annotations.
Combined E2E passes with 2,907 invocations, 113/113 command requirements,
all three required witnesses, and compatibility shell checks.
Host OCR-delegation review covers all seven follow-up files, including
six default-excluded test/doc/data files: zero skipped, no actionable
findings. The frozen ledger/source index and partial dispositions remain
unchanged.

## Remaining Work

R52 loading/normalization costs beyond this measured reachability loop are
not globally bounded by this change. For R53, `rebuildRequests` and the
ordered request interpreter still scan existing selections for exclusions;
source/group/include-tree scaling beyond the removed replay needs separate
evidence. This does not close R51 or collection expansion/budget decisions.
Existing per-file limits do not become a total-work or memory guarantee.

Initial host OCR-delegation review covers all 10 files: the production file
and nine default-excluded tests/documents/raw measurement files, with zero skipped.
The post-rebase sources and final documentation receive the same follow-up
review; no actionable findings remain. These are scoped local gates, not
a claim to have run the full root suite, vendor-native or released-client
container probes.
