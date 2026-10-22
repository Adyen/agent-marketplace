# R5 / R53: Remove Per-Comparison Wildcard Locator Slices

## Scope

This is partial progress on frozen ledger row R53 / F136:
[selection normalization and adaptation](2026-10-16-security-r5-frozen-ledger.md).
It follows the exact-exclusion slices without changing wildcard policy.

`v4ExclusionMatches` used `strings.Split(locator, "/")` for every non-global
wildcard comparison. Replace that slice with two `strings.Cut` operations,
retaining two/three-component recognition and the final literal comparison.
The production change is four added lines and three removed lines.

Seven production call sites share the helper: request rebuilding, effective
request interpretation, low-level v4 editor wildcard/suppression checks,
layer-editor inherited/include checks, and Marketplace include refusal.
The fix stays in their shared matcher rather than creating caller workarounds.

Global `*`, exact equality, registry/kind boundaries, required selections,
decision order, provenance, editor diagnostics, and write/refusal behavior
remain unchanged. Empty components, extra slashes, invalid UTF-8, and literal
wildcard spellings retain the old private-helper results; this adds no input
validation rule. There are no new limits, deadlines, truncation, persistent
caches, dependencies, or release-version changes.

The existing qualified-selector concatenation is deliberately unchanged.
Long qualified names may still allocate its temporary string. The claim is
removal of locator slices, not allocation-free matching for every input.

## Measurements

Go 1.27.3, Apple M4 Max, darwin/arm64, `CGO_ENABLED=0`, `GOMAXPROCS=2`.
Each dataset has six alternating before/after pairs, two seconds per case,
eleven cases, and 132 raw benchmark samples. Pair order alternates AB/BA.
No own heavy validation runs concurrently with collection.

Both variants in each stage use identical benchmark sources and preservation
test sources. The benchmark source blob is
`e08f1b1a8628182cda07907ba74fa025a708d7dd`. All result/error assertions are
outside `b.Loop`; fixture construction is outside timing.

Go overlays use physical canonical paths for both the source key and
replacement, and compilation uses the physical checkout directory. The
baseline source hash, different executable build IDs, and a slow/allocating
versus fast/nonallocating smoke comparison are checked before each collection.
Measurements come from immutable executables, not source edits between runs.

### Integrated Main Baseline

Main `e8dcaba9` includes merged #888 and upstream TUI/deployment-security
changes. Both variants include all of those changes; they differ only in the
wildcard matcher. Production blobs:

- Before: `b27ab6a567a78b45bbafd938acafd47f6cb3f893`.
- After: `8d4a211a6b10e34193f08b0e5acc5ea27b86b71a`.

Primary raw evidence:
[before](security-r5-wildcard-2026-10-22/before.txt),
[after](security-r5-wildcard-2026-10-22/after.txt),
[benchstat](security-r5-wildcard-2026-10-22/benchstat.txt).

| Case | Before | After | Result |
|---|---:|---:|---|
| Kind match | 21.11 ns, 32 B, 1 alloc | 9.980 ns, 0 B, 0 alloc | CPU -52.72% |
| Kind miss | 21.15 ns, 32 B, 1 alloc | 9.337 ns, 0 B, 0 alloc | CPU -55.85% |
| Registry match | 28.93 ns, 48 B, 1 alloc | 12.35 ns, 0 B, 0 alloc | CPU -57.32% |
| Registry/kind match | 38.48 ns, 48 B, 1 alloc | 21.70 ns, 0 B, 0 alloc | CPU -43.62% |
| Public adapter, 1,024 skills plus retained command | 265.8 us, 844,088 B, 4,152 allocs | 252.8 us, 811,288 B, 3,127 allocs | CPU -4.91%; bytes -3.89%; allocs -24.69% |
| Public parser, same fixture | 1.281 ms, 16,375 allocs | 1.264 ms, 15,350 allocs | CPU not significant; allocs -6.26% |

The four wildcard microbenchmark CPU comparisons have `p=0.002, n=6`.
The largest adapter CPU comparison has `p=0.009, n=6`; its allocation/byte
comparisons have `p=0.002, n=6`. Parser CPU is not significant
(`p=0.240, n=6`). Exact and global controls have no significant timing change
and retain zero allocations. Full size curves and confidence intervals
remain in the raw comparison.

The public fixtures select N skills and one command, then apply `skill/*`.
The retained command proves a healthy result, not merely absence of skills.
At N=1,024, each interpretation avoids 1,025 temporary slices: 32,800 bytes
and 1,025 allocations. Adapter timing excludes parsing. The parser benchmark
includes public YAML parsing and normalization; neither includes disk/network.

### Initial Main Snapshot

The independent initial collection starts at main `e1c844c3`, before #888
lands. Keep its [before](security-r5-wildcard-2026-10-22/initial-e1c844c3/before.txt),
[after](security-r5-wildcard-2026-10-22/initial-e1c844c3/after.txt), and
[comparison](security-r5-wildcard-2026-10-22/initial-e1c844c3/benchstat.txt)
separate. Its baseline production blob is
`2ab38628d0f7997f6e648b3509e4860553ea0cb6`.

The initial adapter/parser timings are not significantly different, while
their allocation reductions agree with the integrated collection. A tiny
exact-control timing change appears in that stage despite its unchanged
algorithm; do not present it as an exact-selector improvement. No samples are
pooled across baselines, and no before/after main comparison is claimed.

This is not an end-to-end CLI speed-up, a file-loading improvement, a bound on
wildcard scanning, or a general zero-allocation claim.

## Preservation And Controls

The independently authored token-based `selectorMatchesV4Locator` oracle
compares every pair in a raw-shape table, including long qualified registry
names, malformed shapes, empty components, exact literals, UTF-8/invalid bytes,
and colliding prefixes. Rapid generates arbitrary component histories and
terminal wildcard forms. A bounded native fuzz target compares raw strings;
its size guard is a test resource budget, not a new production input limit.

Both baseline and optimized implementations pass 1,000 generated cases.
Existing public layer-editor wildcard refusal and Marketplace inherited
wildcard refusal controls run against both. The Marketplace control also
admits a healthy selection outside the excluded kind and verifies unchanged
root/fragment bytes and metadata on refusal. Existing structured rebuild and
effective-request controls preserve ordering, required policy, options,
provenance, input immutability, and public runtime/intent/lock behavior.

Six generated mutants deliberately accept extra slashes, widen qualified-kind
matching, accept registry prefixes, drop literal equality, disable global
matching, or disable wildcard matching. All six fail semantically. The disabled
wildcard mutant also fails both public parser and adapter benchmark guards.
An initially malformed prefix mutant does not compile; it is rejected as an
invalid control, corrected, and rerun. Compilation failure is never counted
as semantic regression evidence.

Initial selected config/pipeline/resolver/TUI tests, focused races, and a
20-second native fuzz run pass. After integration, the enriched raw/long-name
controls and existing structured/editor controls pass 1,000 generated cases
on the canonically overlaid baseline. Corresponding optimized focused races
pass. All six mutants and both public benchmark guards fail semantically
again against the integrated implementation. A second bounded native fuzz run
passes as well.

## Integrated Validation

On the `e8dcaba9` integration, selected config/pipeline/resolver/TUI validation,
vet, zero-issue lint, and build pass. Strict specs pass 65 items, 1,160
scenarios, 1,875 annotations, and two active changes.

The combined host E2E/configuration gate passes in 714.7 seconds: 3,423
invocations, all 113 command requirements covered, no listed gaps, and all
three required refusal witnesses executed and passed. Git configuration is
isolated only for the validation process and fixture children, preventing
personal SSH signing from affecting fixture commits; global settings stay
unchanged. The actual shellcheck command passes. A redundant post-gate
`make shellcheck` invocation has no Make target and is not counted as a
validation result.

The full terminal suite passes all 47 executed PTY tests and four separate
utility/process tests, with no failures or skips. Exact locked QA dependencies
are installed from the offline cache: five reused packages, zero downloads,
no lockfile or global configuration change.

Native harness probes, released-client/container gates, and frontend checks
are not claimed as locally executed. Relevant CI gates remain authoritative.

## Publication Integration And Review

Merge main `8ed852a8` without rewriting history. Its nine files add upstream
TUI diagnostics/journeys and test-only upgrade-notice controls; they are not
own R53 changes. The measured production and benchmark blobs remain identical.
No raw measurements are refreshed, relabeled, or pooled at this stage.

Fresh selected validation, vet, zero-issue lint, and build pass. Strict specs
pass 65 items, 1,160 scenarios, 1,879 annotations, and two active changes. The
expanded terminal run passes all 51 executed PTY tests and four separate
utility/process tests, with zero failures or skips. The earlier
3,423-invocation E2E gate is evidence from the unchanged configuration/matcher
implementation before this last TUI integration, not a new run on that head.

Host `open-code-review-delegate` adversarial review covers all eleven own
files, including both Go tests, the plan/report, and all six raw evidence
files. Eleven reviewed, zero skipped, no unresolved actionable findings.
The review checks all seven production call sites, raw token equivalence,
editor refusal/healthy controls, semantic mutants, fixture assertions,
canonical overlays and variant proof, and measurement/validation provenance.
This is host review, not an external model or bot review, and is not a claim
that passing checks proves absence of defects.

## Remaining Scope

R53 remains **partial**. Wildcard scans still scale with selected locators and
wildcard decisions. Qualified-name concatenation, other source/group/include
processing, aggregate memory/work budgets, and acquisition-lock costs are not
bounded or closed here. Do not mark R53 or R5 complete.
