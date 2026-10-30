# R5 / R53: Exact Exclusions In Effective Requests

## Scope And Provenance

This is partial progress on frozen **R53 / F136**, independently measured
from main `e5f27f36`. That base contains PR #868, merged as `22f16cff`, plus
the separately merged plugin-root and editor-reload fixes. When queue
admission for #868 is requested, GitHub already reports it merged; this
work performs no enqueue or merge operation.

The frozen ledger and source index are unchanged. This slice adds no
accepted-input changes, deadlines, size/count limits, truncation, persistent
cache, acquisition, or publication behavior, and makes no D1-D6 decision.

The baseline `internal/config/v4.go` blob is
`2ab38628d0f7997f6e648b3509e4860553ea0cb6`; the optimized blob is
`b27ab6a567a78b45bbafd938acafd47f6cb3f893`.

## Supported Consumers And Change

`effectiveV4Requests` interprets ordered selections for Git/directory runtime
adaptation, Marketplace acquisition-lock adaptation, effective exclusion
derivation, and `LayerIntent.ExplicitSelections`. The earlier R53 slice
optimizes request rebuilding, not this separate interpreter.

Every exact exclusion previously scanned every request selected so far.
The six added lines use the helper's existing locator map when the selector
is neither `*` nor suffixed by `/*`. They mark only a present, non-required
request excluded and continue. Wildcard matching, replacement/reinclusion,
sorting, request payloads, and exclusion-state construction are unchanged.

The expected repeated exact-match work changes from selected-requests times
exact-exclusions to one map lookup per exact exclusion, plus string hashing
and the existing map construction and sort. No additional index is built.
The helper intentionally differs from rebuilding: it ignores include-only
fallbacks and returns a non-nil empty slice when nothing survives.

## CPU Measurements

Go 1.27.3, darwin/arm64, Apple M4 Max, `GOMAXPROCS=2`. Both immutable test
binaries contain the same benchmark source, blob
`c47cc9edad39e1b6288e4d061a14312c542a27c7`. Each exact fixture has N selected
skills, N exact exclusions, and one unexcluded command. This positive control
must survive both the direct helper and public adapter. The one-exclusion
fixture therefore has two selected resources, not one.

Fixture parsing is outside timing. These CPU-only cases contain no timed disk
or network work. Public `ToLegacyConfig` timing includes complete runtime
adaptation and its effective-exclusion derivation; it is not CLI invocation,
configuration-file loading, or Marketplace lock normalization latency.
Final results and errors remain checked after `b.Loop` stops timing.

The baseline uses a Go overlay with canonical physical paths for both key
and replacement. Its production blob is checked before compilation. Distinct
binary build IDs and a 1,024-exclusion slow/fast smoke check (8.461 ms versus
189.5 us) verify actual variants before collecting the comparison. This is a
measurement setup control, not a new production timing requirement.

Six pairs alternate before/after then after/before, two seconds per case,
one sample per immutable binary invocation. No validation workload from this
task runs concurrently with collection. The initial 200 ms exploratory
samples and setup smoke checks are not pooled into the six-pair comparison.

| CPU case | Exact exclusions | Before median | After median | Change |
|---|---:|---:|---:|---:|
| Effective requests | 1 | 282.05 ns | 255.3 ns | -9.48%, p=0.041 |
| Effective requests | 32 | 14.0655 us | 5.380 us | -61.75%, p=0.002 |
| Effective requests | 256 | 542.4865 us | 42.162 us | -92.23%, p=0.002 |
| Effective requests | 1,024 | 8.2824 ms | 176.2 us | -97.87%, p=0.002 |
| Public runtime adapter | 1 | 1.2735 us | 1.211 us | -4.91%, p=0.002 |
| Public runtime adapter | 32 | 36.686 us | 18.104 us | -50.65%, p=0.002 |
| Public runtime adapter | 256 | 1.1423 ms | 137.883 us | -87.93%, p=0.002 |
| Public runtime adapter | 1,024 | 16.9905 ms | 576.9 us | -96.60%, p=0.002 |

The 1,024-request wildcard controls show no significant change: direct
173.208 us versus 172.8275 us (`p=0.589`), public adapter 252.646 us versus
251.3465 us (`p=0.485`). All p-values use six samples per variant.
Noise remains visible in the raw confidence ranges, especially the
32-exclusion direct-helper after samples; do not infer a universal ratio.

All ten cases retain identical B/op and allocation counts in every sample.
At 1,024 exact exclusions the helper retains 449,360 B/op and 1,067
allocations; the public adapter retains 1,661,120 B/op and 6,283 allocations.
This is not a memory-reduction or global CLI speed-up claim.

Raw [before](security-r5-effective-requests-2026-10-21/before.txt),
[after](security-r5-effective-requests-2026-10-21/after.txt), and
[comparison](security-r5-effective-requests-2026-10-21/benchstat.txt) retain
all cases. Independently parse all 120 samples (ten cases, six samples per
variant), verify medians, and regenerate `benchstat` byte-exactly.

To reproduce the comparison, build `./internal/config` test binaries with
identical benchmark sources, replacing only `v4.go` with the recorded
baseline blob for the baseline binary. Resolve both overlay paths physically,
verify the variants, and alternate binaries with:

```sh
GOMAXPROCS=2 ./config.test -test.run '^$' \
  -test.bench '^BenchmarkV4EffectiveRequests(CPU|AdapterCPU)$' \
  -test.benchtime=2s -test.benchmem -test.count=1
```

## Preservation And Negative Controls

The focused preservation suite passes on both the canonically overlaid
baseline and the optimized source, each with 1,000 Rapid histories.
The independent per-locator oracle is reused without its rebuild-only
fallback behavior; it does not call the optimized helper or its matcher.
Complete request values, ordering, required protection, options, harnesses,
provenance, repeatability, and input immutability are compared.

Table controls cover exact/missing/future exclusions, reinclusion,
required/optional replacement, kind and registry boundaries, global/kind/
registry wildcards, sorted output, non-nil empty output, ignored include-only
fallbacks, and nil request decisions. Public controls verify both surviving
and excluded selections through Git runtime adaptation and explicit intent.
The Marketplace control retains verified locked evidence while excluding
an absent lock locator; configuration and lock inputs stay unchanged.
Existing include-tree, Marketplace, effective-exclusion, and runtime
compatibility tests remain in the selected config/consumer gates.

Fresh physical-path Go overlays remove required protection, disable exact
exclusions, omit reinclusion clearing, or disable wildcard matching. All four
mutants fail semantic regressions. Exact and wildcard mutants also fail both
post-loop benchmark result guards. An initially noncompiling required-guard
mutation is rejected as an invalid control, corrected, and rerun; compilation
failure is never counted as semantic regression evidence.

The generated requests retain consistent registry, kind, and name fields
alongside their locators. Final baseline preservation and optimized race
checks each execute 1,000 histories after that metadata enrichment.

## Integrated Validation

Fast-forward to main `0e8b2ac2` imports only five upstream TUI QA files:
three journeys, their fixture additions, and README entries. These are not
own changes. Production and benchmark blobs stay identical to the measured
variants; the benchmark data is not relabeled or pooled.

Fresh selected config/pipeline/resolver/TUI validation passes, with vet,
zero-issue lint, and build. Strict specs pass 64 items, 1,154 scenarios,
1,864 annotations, and two active changes. Final canonically overlaid
baseline preservation passes with 1,000 histories; the corresponding
optimized focused race run passes with 1,000 histories.

The first host E2E attempt stalls in fixture setup because Git commits
inherit personal SSH signing. That run is stopped and is not counted as a
passing gate. Rerun with process-local `GIT_CONFIG_NOSYSTEM=1`,
`GIT_CONFIG_GLOBAL=/dev/null`, `GIT_CONFIG_COUNT=0`, prompt disabling, and
inherited repository/config-parameter variables cleared. No global Git
configuration is changed.

The isolated combined E2E/configuration gate passes in 669.4 seconds:
3,423 invocations, all 113 command requirements covered, zero listed gaps,
and all three required refusal witnesses executed and passed. Shellcheck
also passes. Native harness probes, container/released-binary gates, and
frontend checks are not represented as locally executed by this slice.

The enlarged terminal suite passes all 44 executed PTY tests and four
separate utility/process tests, with zero failures, skips, or cancellations.
The exact locked dependencies are installed from the offline cache using an
isolated tool configuration; neither the lockfile nor global settings change.
Final required/exact/reinclusion/wildcard mutations also fail semantically
against the enriched property metadata, with Rapid fail-file writing disabled.

Host `open-code-review-delegate` adversarial review covers all eight own
files, including both Go tests, the design/report, and all three raw evidence
files: eight reviewed, zero skipped, no unresolved actionable findings.
The review checks all four consumer families, selector classification,
required/replacement/reinclusion semantics, metadata and nil/empty contracts,
input immutability, positive and mutation controls, and measurement provenance.
This is host review, not an external Kimi or bot review, and is not a claim
that clean checks prove absence of defects.

## Publication Integration

Before publication, merge main `61f52b44` without rewriting history. Its
changes since `0e8b2ac2` are confined to five upstream TUI QA files. The
R53 production and benchmark blobs still match the measured variants.
No raw measurements are refreshed or pooled.

Fresh selected config/pipeline/resolver/TUI validation, vet, zero-issue lint,
and build pass. Strict specs pass 64 items, 1,154 scenarios, 1,867 annotations,
and two active changes. The enlarged terminal run passes all 46 executed PTY
tests and four separate utility/process tests, with zero skips or failures.
The earlier 3,423-invocation E2E result remains evidence for the unchanged Go
implementation, not a newly executed E2E run on this TUI-test-only integration.
Host adversarial delegation review still covers all eight own changed files.

## Remaining Scope

R53 remains **partial**. Wildcard scans, other source/group/include-tree
processing, broader runtime adaptation and acquisition-lock costs, and
aggregate memory/work policies are not bounded or closed here. Do not treat
this fixture-specific exact-lookup improvement as completion of R53 or R5.
