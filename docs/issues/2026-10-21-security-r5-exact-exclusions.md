# R5 / R53: Exact Exclusions During V4 Request Rebuilding

## Scope And Provenance

This is partial progress on frozen **R53 / F136**, instance
`01a0cecf-843d-76bc-b634-48bc47cf8965`. The [frozen ledger](2026-10-16-security-r5-frozen-ledger.md)
and source index are unchanged. The [earlier adaptation report](2026-10-18-security-r5-v4-adaptation.md)
explicitly leaves request rebuilding and ordered request interpretation open.

The measured baseline is main `d59363cf2d4651d40a4ca4be44031c86ff7dff2c`.
The slice subsequently moves to an isolated worktree on cached main
`733e1e65729be30c2188d763fee024331655844f`, containing the separately merged
R10 PR #828 and TUI QA PR #859. No R10 implementation or unrelated site
deployment work is part of this diff.

The baseline `internal/config/v4.go` blob is identical at both main revisions:
`cbf1f1cfaff915933a70609d5d1ad0e2beadd121`. The optimized production blob is
`2ab38628d0f7997f6e648b3509e4860553ea0cb6`. Benchmarks precede other validation
workloads and use immutable compiled test binaries.

## Supported Consumer And Change

V4 root/include parsing normalizes ordered resource decisions and rebuilds
the effective explicit requests. The existing selected-request map is keyed
by full source-local locator. Before this change, each exclusion scans that
entire map, even when only one exact key can match.

The six-line optimization looks up exact exclusions in that existing map.
It marks an exclusion only for a request already present and not required.
Global `*` and suffix `/*` exclusions retain the original scan and matcher.
No additional map, cache, API, input limit, deadline, or policy is introduced.

This preserves full locator identity, required protection, optional replacement
of an earlier required request, later reinclusion, absent/future requests,
include-only fallbacks, source-group/locator order, decision order, options,
harness targets, and provenance. Parser errors and acquisition-lock/native
output contracts do not change. Use `Spec-Impact: none` for this preservation
optimization, not a new behavioral spec.

## Reproducible CPU Measurements

Environment: cached Go **1.27.3**, Darwin/arm64, Apple M4 Max,
`CGO_ENABLED=0`, `GOTOOLCHAIN=local`, `GOPROXY=off`. Both versions compile
the same benchmark definitions. Fixtures have 1, 32, 256, or 1,024 includes
and matching exact exclusions. The largest document is **49,290 bytes**,
below the existing 4 MiB per-file ceiling; no aggregate budget is inferred.

`BenchmarkV4RebuildExactExclusionsCPU` times only rebuilding.
`BenchmarkParseV4ExclusionsCPU` times the public `ParseV4` consumer,
including YAML parsing and normalization. Fixture generation is outside timing.
The wildcard control has 1,024 includes and one global exclusion. All cases
verify the effective request count and complete decision count.
Neither benchmark includes disk I/O, Git, network, or materialization.
These are not end-to-end CLI latency measurements.

The initial sequential before/after run uses the default `GOMAXPROCS=16`,
five two-second samples per case. It also reports an 8.24% wildcard improvement
despite the unchanged wildcard loop, so that control is not attributed to the
optimization. Retain this dataset and its five-sample confidence warning:
`before.txt`, `after.txt`, and `benchstat.txt` in the evidence directory.

The confirmation run fixes `GOMAXPROCS=2` and alternates immutable binaries
over **six pairs**, before/after on odd pairs and after/before on even pairs.
Each binary executes all nine cases with `-test.benchtime=2s`,
`-test.count=1`, and `-test.benchmem` per pair. No other validation gate runs
alongside these measurements. Compare the resulting six samples per case
with `benchstat`; the two sample sets are not pooled.

| CPU case | Requests | Before median | After median | Change |
|---|---:|---:|---:|---:|
| Rebuilding, exact | 1 | 311.6 ns | 273.6 ns | -12.18% |
| Rebuilding, exact | 32 | 20.102 us | 9.268 us | -53.90% |
| Rebuilding, exact | 256 | 685.16 us | 77.16 us | -88.74% |
| Rebuilding, exact | 1,024 | 11.1871 ms | 348.4 us | -96.89% |
| Public parsing, exact | 1 | 11.53 us | 11.26 us | Not significant, p=0.394 |
| Public parsing, exact | 32 | 88.95 us | 75.07 us | -15.61% |
| Public parsing, exact | 256 | 1.1690 ms | 524.6 us | -55.12% |
| Public parsing, exact | 1,024 | 13.157 ms | 2.172 ms | -83.49% |
| Public parsing, wildcard | 1,024 | 1.255 ms | 1.242 ms | Not significant, p=0.589 |

The significant CPU changes have `p=0.002, n=6`. Allocation counts and bytes
are effectively unchanged, not a memory-reduction claim. At 1,024 exact
requests public parsing retains 22,530 allocations and approximately 3.026 MB
per operation. Small byte-count fluctuations remain in the raw results.

Retained confirmation data:
[before](security-r5-exact-exclusions-2026-10-21/controlled-before.txt),
[after](security-r5-exact-exclusions-2026-10-21/controlled-after.txt), and
[comparison](security-r5-exact-exclusions-2026-10-21/controlled-benchstat.txt).
To repeat a case on either source variant:

```sh
GOMAXPROCS=2 CGO_ENABLED=0 go test ./internal/config -run '^$' \
  -bench '^(BenchmarkV4RebuildExactExclusionsCPU|BenchmarkParseV4ExclusionsCPU)$' \
  -benchtime=2s -count=1 -benchmem
```

For an interleaved comparison, compile both variants with `go test -c`
first and invoke the binaries with the corresponding `-test.*` flags.
Replace only the production file with the recorded baseline blob through a
Go overlay to retain identical benchmark sources.

## Preservation And Negative Controls

- Deterministic checks cover exact kind/registry identity, required protection,
  optional replacement, reinclusion, absent/future requests, global/kind/
  registry wildcards, and an unseen include-only fallback.
- A separately authored per-locator interpreter checks 1,000 structured Rapid
  histories across two source groups. It compares complete requests and decision
  streams, including options, harness targets, provenance, sorted output,
  repeat rebuilding, and byte-exact input-group nonmutation.
- A real include-tree load reintroduces a child-excluded request, retains a
  child-only selection absent from the root, excludes another child selection,
  checks winning-file provenance and runtime adaptation, and preserves authored
  file bytes. Test paths are canonicalized for macOS `/var` aliases.
- Go overlays independently remove required protection, exclude unseen fallback
  requests, bypass wildcard matching, and break reinclusion. Each fails the
  deterministic semantic tests, rather than compilation or setup. No mutation
  changes the working production source.

The complete preservation suite passes against the baseline overlay and
optimized implementation with 1,000 Rapid checks; the optimized focused race
run also passes. The pre-integration combined E2E/configuration gate passes with
3,389 invocations, 113/113 command requirements, all three required witnesses
executed and passed, and compatibility shell checks.

## Integrated Validation And Review

On main `733e1e65` plus this slice, selected validation passes for
`internal/config`, `internal/pipeline`, `internal/resolver`, and `internal/tui`,
alongside `go vet`, CI-pinned golangci-lint 2.14.0 (zero issues), and the build.
Strict spec checks pass: 64 items, 1,133 scenarios, 1,813 annotations, and
two active changes. Fresh focused races with 1,000 Rapid checks pass.
The fresh integrated combined E2E/configuration gate also passes: 3,389
invocations, 113/113 command requirements, zero listed gaps, all three required
refusal witnesses executed and passed, and compatibility shell checks.

Cached main then advances to `98e064b8`, the TUI-only viewport fix in PR #864.
It is integrated without conflicts; the R53 production blob, command-mode
CLI/configuration/lock paths, and all benchmark evidence remain unchanged.
Fresh selected validation, strict specs, and the committed spec gate pass
after that integration (64 items, 1,134 scenarios, 1,815 annotations, two active
changes). The combined E2E result above names the earlier `733e1e65` integration;
it is not relabeled as a rerun after the TUI-only merge.

The first broader TUI test run fails the existing include-format error-view
assertion: a long macOS temporary path makes the renderer wrap `v4.api-version`
as `v4.api-` and `version`. The diagnostic is present, but the test searches
the unjoined string. An isolated default-path rerun passes, as do 20 baseline
overlay repetitions. Integrated validation uses canonical `TMPDIR=/private/tmp`
to avoid this path-length-dependent rendering assertion; `/tmp` itself is a
macOS symlink and correctly fails organization ancestor trust checks.
No renderer, trust policy, assertion, or production environment is changed.
This invalid included fragment refuses before request rebuilding.

Host adversarial `open-code-review-delegate` review covers all 11 own files,
including both tests, the design/report, and all six raw evidence files:
zero skipped, no unresolved actionable findings. The review checks actual
consumer reachability, exact/wildcard equivalence, required/reinclusion
semantics, source/scope/order boundaries, absent/fallback behavior, lack of
new writes, regression non-vacuity, and measurement provenance. Raw sample
counts and medians are independently parsed and both `benchstat` outputs
regenerated byte-exactly. This is not an external Kimi or bot review, nor a
claim that clean checks prove absence of defects.

## Remaining Scope

R53 stays **partial**. The ordered request interpreter, wildcard exclusion
scans, other source/group/include-tree costs, and aggregate work or memory
budgets are not fixed or bounded here. Existing limits and all D1-D6 decisions
remain unchanged. Do not present these fixture gains as a global CLI speed-up
or a completed security round.
