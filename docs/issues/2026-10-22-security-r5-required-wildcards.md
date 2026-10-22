# Security R5: Required Wildcard Guard (R53 / F136)

## Scope

`rebuildRequests` evaluates `v4ExclusionMatches` before checking whether the
selected request is required. Required selections cannot be excluded, so
matching them does unnecessary work. The sibling `effectiveV4Requests`
interpreter already tests the required flag first.

This slice reorders that one condition. The matcher is pure and total, so
skipping it for protected requests changes no selection, ordering, metadata,
diagnostic, refusal, accepted input, or publication. No index, cache, cap,
deadline, truncation, dependency, or graph/policy change is introduced.

The map still scans every selected request for every wildcard exclusion.
Its worst-case O(N*E) work is not bounded by this change. Required matching is
only one cost; **R53 and R5 remain partial**.

## Measurements

CPU-only Go benchmarks use public `ParseV4` and private rebuilding, not
network, acquisition, lock loading, filesystem, or whole-CLI latency.
Valid Marketplace documents contain N distinct registry-qualified skills,
N distinct registry/kind exclusions, and a retained command. Required and
optional groups are measured separately at N=1,256,1024. All documents fit
the existing configuration byte limit. Fixture construction, parsing for
the private benchmark, and result checks are outside the timed loops.

Go 1.27.3, darwin/arm64, Apple M4 Max, CGO disabled, GOMAXPROCS=2. Each
stage alternates six before/after pairs, reversing order every other pair,
at two seconds per case. Four immutable executables use identical benchmark
definitions. Physical canonical overlays, distinct build IDs, source hashes,
and protected-allocation smoke checks distinguish the variants before
collection. Each raw file has twelve cases with six samples each: **288
samples across two separate stages**. No own heavy validation overlaps
collection; wide observed timing intervals remain in the raw reports.

| Variant | Full `v4.go` blob |
|---|---|
| Main before, base `81c6a6e2` | `b27ab6a567a78b45bbafd938acafd47f6cb3f893` |
| Main after, guard only | `2036998586c0e45e43d23a38f0b5fab559e613b3` |
| PR #900 matcher before, base `74c31078` | `bfe754265bd9091be94ccedac070b4ef867633ed` |
| PR #900 matcher plus guard | `0f6caf86f403bc88c3eb1265c970f8c9be0c173e` |

All four use benchmark blob `b335506b167af7ded8295c2223b342a9ee1952ab`.
The second stage overlays only `v4.go`; other sources/dependencies and
benchmark definitions stay identical to the first stage.

### Main's Existing Matcher

| Required workload | Before | After | Difference |
|---|---:|---:|---:|
| Rebuild, N=256 | 3.803 ms | 0.459 ms | -87.94% |
| Rebuild, N=1024 | 59.020 ms | 6.305 ms | -89.32% |
| Parse, N=256 | 4.360 ms | 0.938 ms | -78.48% |
| Parse, N=1024 | 61.189 ms | 8.063 ms | -86.82% |

These differences have p=0.002, n=6. The main N=1024 parser baseline has a
wide 88% interval; do not interpret the point estimate as a universal ratio.
N=1 CPU controls and all optional-group CPU controls are not significant.
Protected N=1024 rebuilding drops from 1,050,691 to 1,091 allocations and
49.724 to 1.677 MiB per call. Parsing drops from 51.629 to 3.582 MiB.

Raw: `security-r5-required-wildcards-2026-10-22/{before,after,benchstat}.txt`.

### Interaction With PR #900

PR #900 independently removes matcher allocations. **Its allocation win
overlaps this guard's main-baseline allocation savings; do not add them.**
This separate stage measures the guard after that allocation-free matcher,
regardless of which PR merges first.

| Required workload | Before | After | Difference |
|---|---:|---:|---:|
| Rebuild, N=256 | 1.921 ms | 0.456 ms | -76.26% |
| Rebuild, N=1024 | 32.122 ms | 6.289 ms | -80.42% |
| Parse, N=256 | 2.443 ms | 0.965 ms | -60.50% |
| Parse, N=1024 | 34.340 ms | 8.213 ms | -76.08% |

These differences have p=0.002, n=6. Bytes and allocations are effectively
unchanged throughout this stage. The smallest required rebuild improves
5.46% (p=0.015); smallest public parsing is not significant. Optional private
controls are not significant. Optional parsing at N=256 and N=1024 also
measures 1.84% and 5.37% lower, respectively (p=0.026 and p=0.002).
Those optional requests still execute the matcher; do not attribute these
secondary observations to protected-work elimination. Several public timing
intervals are wide. Neither stage proves a global CLI or end-to-end speed-up.

Raw: `security-r5-required-wildcards-2026-10-22/matcher-cut/`.
The stages are not pooled, relabeled, or summarized with a mixed geomean.

## Preservation And Controls

- Five deterministic cases extend the existing interpreter table: required
  kind, registry, registry/kind, and required/optional replacement ordering.
- The existing independent per-locator interpreter compares complete requests
  and decisions, including options, harnesses, provenance, required flags,
  input immutability, fallback inclusions, and stable repeated evaluation.
  Baseline, guard, and allocation-free-matcher-plus-guard runs pass 1,000
  generated checks; focused race checks pass too.
- Every benchmark checks selected count, the retained command, required flags,
  and the complete authored decision count after timing.
- Four semantic mutants remove protection, invert it, ignore wildcard
  matching, or ignore the selector. All four fail both the deterministic
  interpreter tests and public parser benchmark guards: eight actual semantic
  failures. An initial unused-variable mutant compilation failure is rejected
  as invalid evidence, repaired, and rerun; it is not credited as a control.
- An exploratory fixture repeated the same same-document wildcard. The
  existing `v4.duplicate` refusal correctly rejects it. That fixture is not
  benchmark evidence, and duplicate acceptance is not changed.

## Initial Validation

On `81c6a6e2` plus this slice, before the subsequent main integration:

- Selected `make validate` passes config, pipeline, resolver, and TUI tests,
  vet, zero-issue CI lint, and build.
- Specs pass: 65 items, 1,163 scenarios, 1,884 annotations, two active changes.
- Full terminal gate passes 53 journeys and four utilities, zero skips.
- Combined E2E/configuration gate passes in 692.928 seconds: 3,423 invocations,
  all 113 requirements, zero listed gaps, and all three required refusal
  witnesses executed and passed. Included shellcheck passes.
- Exact offline QA dependencies are reused; no global settings or dependency
  lockfiles change. Git fixture isolation is process-local.

Main subsequently advances through unrelated TUI changes. Its `v4.go`,
`internal/config`, `go.mod`, and `go.sum` remain unchanged from the measured
base. Integration validation and adversarial review must still finish before
publication; these initial results are not relabeled as later-head runs.
