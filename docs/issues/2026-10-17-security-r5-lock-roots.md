# Security R5: R48 Git Root Matching

## Scope And Disposition

This slice addresses R48 / F117 from the
[immutable frozen ledger](https://github.com/Adyen/agent-marketplace/blob/2f55f85f58a3fe3caca4965095bd90f8a1d82518/docs/issues/2026-10-16-security-r5-frozen-ledger.md).
Its obligation is to verify nested `ProjectGitRoots` comparisons while
preserving lock freshness and refusal results. The function previously
performed roots-times-requirements comparisons on valid large inputs.

The candidate uses invocation-local indexes when both dimensions exceed
32. At most 32 roots or requirements retain the original scans, avoiding
index setup on small or sparse inputs. This is an algorithm dispatch, not
an admission ceiling: no inputs or results are rejected or truncated by it.
Large-input matching becomes expected linear work in roots plus
requirements, in addition to processing path bytes. Index construction
follows the first requirement's path validation.

No D1-D6 policy is introduced. This removes the measured R48 repeated-work
cost; it does not bound total lock/configuration bytes, acquisition, or the
other frozen rows. #434 remains open. The frozen ledger and source index
are not edited.

## Preserved Behavior And Consumers

Explicit requirements still run before implicit ones. Explicit matches use
the first matching stored root; implicit matches use the first selected
root for that path or the last matching root otherwise. Output preserves
stored order and complete resource/support metadata. Requirement
normalization, missing-root/harness refusal, invalid-path refusal, empty
projection shape, and input immutability remain unchanged.

The shared helper serves CLI reconciliation, CLI unselected-layer
freshness, and read-only pipeline freshness. Existing exact-lock lifecycle
controls witness commit retention, root removal, equivalent auto/explicit
requirements, and no-op config/lock bytes. Pipeline matching adds large
fresh and extra-root/stale cases that reach the indexed branch.

Thirty authored selection/refusal controls cover both branches and compare
independently selected expected root indexes with both implementations.
The bounded fuzz oracle's body exactly matches baseline production code;
the final 10-second run completes 307,132 executions without a mismatch.
A deliberate last-explicit-match mutation fails two indexed compatibility
cases, proving duplicate-root selection and metadata assertions are live.
The mutation is removed.

## Measurements

Go 1.27.1, darwin/arm64, Apple M4 Max. Baseline: `cb2bf890`. The same
benchmark source and fixture setup run before and after the matcher edit,
using `-benchtime 2s -count 5 -benchmem`, sequentially without concurrent
local validation. Setup remains outside `b.Loop`. Measurements precede the
non-overlapping rebase to `790a1570`; they are not relabeled as post-rebase
measurements.

These are in-memory matching benchmarks, not complete command latency.
The CLI wrapper includes conversion of configuration entries into root
requirements, but neither benchmark includes disk or network I/O.

Observed medians for 2,048 paths:

| Fixture | Before ns/op | After ns/op | Before B/op | After B/op | Before allocs/op | After allocs/op |
|---|---:|---:|---:|---:|---:|---:|
| Explicit (one root/requirement per path) | 5,051,419 | 261,206 | 182,272 | 543,136 | 2 | 22 |
| Implicit (one root/requirement per path) | 8,827,177 | 228,546 | 182,272 | 543,136 | 2 | 22 |
| Paired (two roots, three requirements per path) | 26,742,654 | 601,762 | 364,544 | 1,086,176 | 2 | 38 |
| CLI wrapper (one root, two requirements per path) | 5,203,317 | 360,570 | 313,344 | 674,208 | 3 | 23 |

All four large-case timing comparisons have `p=0.008`, `n=5` in benchstat.
The tables retain benchstat's warning that five samples are insufficient
for its 95% confidence intervals. The one-root common cases and 32-root
explicit/implicit cases retain identical allocation bytes/counts.

The tradeoff is deliberate and visible: the indexed helper uses about
three times the allocation bytes on large fixtures; the CLI wrapper uses
about 2.15 times. Nothing here establishes an aggregate memory ceiling.

Raw samples and ASCII-normalized benchstat tables:
`docs/issues/security-r5-lock-roots-2026-10-17/`.

```sh
CGO_ENABLED=0 go test ./internal/lockfile -run '^$' \
  -bench '^BenchmarkProjectGitRoots$' -benchtime 2s -count 5 -benchmem
CGO_ENABLED=0 go test ./cmd/agent-marketplace -run '^$' \
  -bench '^BenchmarkProjectLockedRoots$' -benchtime 2s -count 5 -benchmem
```

## Verification

The baseline controls, final matching controls, bounded differential fuzz
and mutation check pass as described. Post-rebase scoped `make validate`
passes selected short-mode lockfile/pipeline package tests, vet, zero-issue
CI lint and build. Seven focused CLI consumer regressions and focused
lockfile/pipeline races pass. Strict spec checks pass: 59 items, 991
scenarios, 1,598 annotations. Both benchmark suites pass a post-rebase
one-iteration smoke check; these are not replacement performance samples.
The combined E2E/configuration-compatibility gate passes: 2,883 invocations,
113/113 command requirements covered, zero listed gaps, and all three
required witnesses run and pass. This is scoped root validation plus the
combined E2E gate, not a claim that `make validate-full` ran.

The host uses OCR delegation for deterministic selection and rule
resolution, and manually reviews default-excluded tests, prose and
measurement artifacts. Implementation revision `ac6a8bcc` and the
documentation follow-up are reviewed: one production file and 13
default-excluded test/doc/data files, 14/14 overall, zero skipped.
No unresolved actionable critical/high/medium finding remains. Neither
tests nor review proves absence of defects.
