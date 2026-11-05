# Security R5: Filter Excluded Requests Before Sorting

## Scope And Contract

This independent R53/F136 slice starts at main `c364c7a6`, after PRs #900
and #912 merged, and later integrates main `2968b44f`, `9f71ff74`, and
`385b067e`. The frozen ledger and source index remain unchanged.
This is not closure of R53, R5, or #434.

`V4Config.rebuildRequests` previously collected and sorted every locator
before discarding excluded requests during emission. It now filters keys
using the final exclusion flags, sorts retained keys, and emits them.
The sibling `effectiveV4Requests` already uses this pattern.

Decision replay and include-only fallback insertion finish before filtering.
Filtering a sorted set and sorting its retained subset produce the same
ordered result. Source-group sorting, request/decision metadata, required
protection, replacement/reinclusion, fallback selection, accepted inputs,
diagnostics, and nil results retain their existing contracts.

No cache, index, extra state, dependency, parser budget, deadline, truncation,
or policy decision is introduced. Existing capacity hints are retained.
Only the collection/emission stage changes from O(N log N) to
O(N + K log K), for N known and K retained locators. Parsing, map storage,
wildcard replay, and whole-command costs remain outside that claim.

## Measurement Method

Go 1.27.3, darwin/arm64, Apple M4 Max, GOMAXPROCS=2, CGO disabled.
Each dependency stage uses six alternating AB/BA pairs, two-second samples,
immutable before/after executables, and identical benchmark source. No
other local validation runs concurrently with measurements.

Fixtures contain 1, 256, or 1,024 distinct skills plus an unrelated retained
command. They exclude zero, half, or all skills; the smallest fixture omits
the meaningless half case. Each stage has sixteen cases, six samples per
variant, and 192 numeric rows. The two stages remain separate: 384 rows are
retained, not pooled into twelve-sample comparisons.

`BenchmarkV4FilterSortRebuildCPU` excludes parsing. The complete public
`BenchmarkParseV4FilterSortCPU` includes YAML decoding, validation, and
normalization. Both exclude disk/network work. Fixture construction and
result checks stay outside timing. Every fixture passes the public v4
decoder and the existing configuration byte ceiling.

### Provenance

The complete production blobs are `0f6caf86` before and `a9cc6e5b` after;
the benchmark blob is `0ea66e6a`. These bytes remain identical across main
integration. Both stages have distinct before/after build IDs. Overlay
source keys use resolved physical paths, not macOS temporary-path aliases.

The first stage uses `c364c7a6` dependencies. The refreshed stage uses
`2968b44f`, including upstream-only dependency and TUI QA changes:
`go.mod` blob `2e56ece8`, `go.sum` blob `3f92da9d`, and `go.work` blob
`d73fa3e0`. No upstream QA file is part of this slice's own diff.

Raw evidence under `security-r5-filter-sort-2026-10-23/` contains:

- Root `before.txt`, `after.txt`, and `benchstat.txt`: initial dependency
  stage, retained rather than relabeled.
- `latest-main/before.txt`, `after.txt`, and `benchstat.txt`: refreshed
  stage. These are the headline results.

An earlier three-sample, 100-ms exploratory probe remains separate from
these datasets and does not support the final significance claims.

## Refreshed Results

Selected medians from the latest-main stage:

| CPU-only fixture | Before | After | Change | p |
|---|---:|---:|---:|---:|
| Rebuild, 256 skills, half excluded | 88.29 us | 79.55 us | -9.90% | 0.002 |
| Rebuild, 256 skills, all excluded | 82.87 us | 67.36 us | -18.72% | 0.002 |
| Rebuild, 1,024 skills, half excluded | 380.5 us | 350.0 us | -8.03% | 0.002 |
| Rebuild, 1,024 skills, all excluded | 369.8 us | 292.4 us | -20.92% | 0.002 |
| Public parse, 1,024 skills, all excluded | 2.304 ms | 2.230 ms | -3.21% | 0.041 |

"All excluded" means all fixture skills; the command remains selected.
Healthy zero-exclusion controls show no significant timing difference.
Other public-parser comparisons in the refreshed stage are not significant.
The smallest direct excluded-skill case improves 3.29% (p=0.015).
Full intervals and nominal, unadjusted p-values remain in the tables; the
public-path signal is smaller and weaker than the direct rebuild gain.
There is no whole-CLI or universal speed-up claim.

The initial stage's 1,024/all case measured rebuilding 357.8 to 280.4 us
(-21.62%, p=0.002) and public parsing 2.256 to 2.137 ms (-5.28%, p=0.009).
These are not substituted for the refreshed results.

Direct rebuild bytes are identical; allocation-count medians are unchanged
in all cases in both stages. Public-parser byte medians vary by at most
12.5 bytes per call, below 0.001%. The 256/all parser case increases by
5 bytes initially (p=0.024) and 5.5 bytes in the refresh (p=0.030), displayed
as +0.00%. Raw values retain these small differences. No allocation
reduction or exact equality of all parser byte samples is claimed.

## Preservation And Negative Controls

- Authored filtered-order and required/reinclude cases check retained
  selection against independently written ordered expectations.
- The existing independent per-locator interpreter compares complete
  requests/decisions, options, harnesses, provenance, required flags, input
  immutability, fallback inclusions, and stable repeated evaluation.
  Both variants pass 1,000 generated checks in each dependency stage.
- Benchmarks check exact request/decision counts, the retained command,
  every remaining skill's identity/order, and optional flags after timing.
- Three private mutations drop the filter, invert it, or omit sorting.
  Each fails both interpreter tests and the public parser benchmark guard:
  six semantic failures, not compilation failures or missing-test matches.
- Focused race checks pass on the original dependency stage. Mutation
  failfiles are retained privately, not committed as test fixtures.

Preservation checks correctly pass the original algorithm. They do not prove
algorithmic improvement; the repeated paired measurements provide that evidence.

## Initial Validation

On `c364c7a6` plus this slice:

- Selected validation passes config/pipeline/resolver/TUI tests, vet,
  zero-issue CI lint, and build.
- Strict specs pass: 65 items, 1,172 scenarios, 1,898 annotations, two
  active changes.
- Full terminal QA passes 54 journeys plus four utilities, zero skips.
  Exact offline dependencies are reused; no lockfile or global settings change.
- Combined E2E/configuration validation passes in 703.752 seconds:
  3,427 invocations, all 113 requirements, no gaps, all three required
  refusal witnesses executed and passed, and included shellcheck.

One initial TUI package run fails only during TempDir removal after the
warning-only update's functional assertions (a Git-cache directory is not
empty). Ten isolated repetitions on the unchanged baseline and ten on the
candidate pass, as does the entire selected rerun. The failure is retained;
no assertion, cleanup guard, timing bound, or retry policy is weakened.

## Integration And Publication

Main integration adds dependencies and upstream-only terminal QA coverage,
not production configuration changes. The refreshed measurement uses those
new dependency bytes. Initial validation results are not relabeled as
executions on the integrated head.

On `2968b44f` plus the identical measured candidate:

- Selected config/pipeline/resolver/TUI tests, vet, zero-issue CI lint, and
  build pass. The enclosing validation target exits zero, but its separate
  module-tidy invocation cannot fetch a checksum-pinned transitive test
  dependency from the offline cache. This output is not counted as a clean
  module-hygiene check; exact dependency download or current-head CI must
  verify it. Dependency files and global settings remain unchanged.
- Strict specs pass: 65 items, 1,172 scenarios, 1,921 annotations, two
  active changes.
- The enlarged terminal suite passes 55 journeys plus four utilities,
  zero skips. No TUI behavior or journey is changed by this slice.
- Focused race checks with 1,000 generated cases pass in 3.118 seconds.
- Fresh combined E2E/configuration validation passes in 734.669 seconds:
  3,427 invocations, all 113 requirements, no gaps, all three required
  refusal witnesses executed and passed, and included shellcheck.

GitHub-required integration through main `9f71ff74` adds its compact-dialog
fix and terminal coverage without changing the measured configuration,
benchmark, or dependency bytes. Selected Go/vet/lint/build and the committed
spec gate pass. Strict specs pass 65 items, 1,172 scenarios, and 1,926
annotations; the enlarged terminal suite passes 57 journeys plus four
utilities, zero skips. CI's lint log on `d379c7d2` fetches the exact
checksum-pinned test dependency, completes module hygiene without errors or
diffs, and reports zero lint issues. No local dependency download is
performed by this work; the offline output is not retroactively called a pass.

## Terminal Journey Repair

Initial-head Linux CI fails `launch-browse-inspect`: the journey expects to
fold Enabled, but its captured screen is still filtered to alpha and shows
folded Available. Its search assertion can accept an existing detail pane;
its clear assertion can accept the pre-search headings. Neither proves the
intended query-frame transition.

The journey now observes the alpha query banner and absence of beta before
clearing, then observes the absent query banner and restored beta before
folding. Existing selection, heading, detail, and output assertions remain.
There is no runtime change, sleep, timeout increase, or retry-policy change.
Omitting the search input in a private generated-JavaScript mutation fails
at the new search-frame guard as intended. The repaired journey and full
57-journey/four-utility suite pass, with zero skips; strict specs still pass.

## Final Main Integration

Main `385b067e` merges its already-decided default-install-scope change.
That upstream policy and its specs/tests are retained, not introduced or
reinterpreted by the R5 diff. Configuration, benchmark, and dependency
bytes remain identical to the refreshed measured variants.

On that integration plus this slice:

- Selected config/pipeline/resolver/TUI validation, vet, zero-issue lint,
  and build pass. Six targeted command scope regressions pass in 8.924 seconds.
- Strict specs pass 65 items, 1,174 scenarios, 1,928 annotations, and two
  active changes; the committed spec gate passes.
- All 57 terminal journeys plus four utilities pass, with zero skips.
- Fresh combined E2E/configuration validation passes in 721.191 seconds:
  3,427 invocations, all 113 requirements, no gaps, all three required
  refusal witnesses executed and passed, and included shellcheck.
- The shared dependency cache becomes populated outside this session.
  Explicit `GOPROXY=off GOWORK=off go mod tidy -diff` passes with no
  output or dependency-file changes, independently of the validation
  recipe's exit handling. No new dependency download is performed by
  this work and no global settings change.

Host `open-code-review-delegate` initially covers eleven own files and then
all twelve after this journey repair, including OCR-excluded tests,
documentation, and all six evidence files. All 384 rows, sample counts,
source/build distinctions, statistics, and stage-specific claims are checked;
zero files are skipped and no actionable finding remains. CodeRabbit,
Gemini, and all current-head required CI checks must be clear before
handoff. Leave the PR open, unmerged, unqueued, and with auto-merge disabled.
