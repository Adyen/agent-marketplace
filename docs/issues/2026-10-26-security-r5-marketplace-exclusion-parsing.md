# R5 / R53: Marketplace Exclusion Runtime Parsing

## Disposition

Remove one temporary split allocation per effective Marketplace exclusion
without changing runtime acceptance, refusal diagnostics, or selection
behavior. R53/F136 and performance track #765 remain partial.

This is a policy-free implementation slice: no new limits, validation,
configuration grammar, required-resource rule, lock format, publication
boundary, or native-output behavior.

## Production Change

`V4Config.toLegacyConfig` previously split every Marketplace exclusion into
a slice merely to require three components, reject literal middle
`collection`, and reject literal final `*`.

Two standard-library `Cut` calls and a remaining-slash check perform that
same guard without allocating the temporary slice. The preceding
user-layer-only override check and the existing error construction stay
in place.

This guard is deliberately not another input validator. Empty components,
unknown kinds, and embedded unusual bytes can exist in manually constructed
private inputs even though `ParseV4` rejects their authored equivalents.
The frozen legacy predicate pins those results, rather than imposing a
new grammar.

## Provenance

- Measured base: `867731e54408d4e0b63a55871ad46efcc498155a`, after
  #946 and #948. The earlier exclusion-kind helper is allocation-free in
  both measured variants; its saving is not credited again.
- Go 1.27.3, Darwin/arm64, Apple M4 Max, native compiled test binaries with
  `CGO_ENABLED=0`. Default benchmark CPU suffix is `-16`.
- Identical test and benchmark definitions in both binaries. Only
  `internal/config/v4.go` differs; the baseline uses the frozen pre-change
  production file. The manifest records package-source, dependency, and
  binary identities.
- Main subsequently advanced to `dd5df8d8` through terminal-only QA.
  Integrate that coverage without changing or relabeling the measurements:
  measured configuration, runtime consumers, and dependencies are unchanged.
- Primary and confirmation stages are separate. Preserve all observations,
  including unfavorable control timings; do not pool stages or headline
  the aggregate geomean.

## Primary Measurement

Six alternating before/after pairs, one second per case, 32 cases per
variant: 384 benchmark rows. Parsing, fixture construction, result checks,
disk access, network acquisition, and lock acquisition are outside timing.

Reuse `BenchmarkV4MarketplaceExclusionKindCPU` from #948, including its
retained Git command control. Add only CPU-only public refusal benchmarks.
Other controls cover Marketplace lock hydration, Git and directory
adaptation, retained selections, exact exclusions, churn, and wildcards.

| Public Marketplace exclusions | Before | After | Change |
| --- | ---: | ---: | ---: |
| 1 | 1.009 us | 1.003 us | not significant, p=0.071 |
| 256 | 29.03 us | 24.51 us | -15.56%, p=0.002 |
| 1,024 | 115.25 us | 95.02 us | -17.56%, p=0.002 |

Allocation counts are 14 to 13, 296 to 40, and 1,076 to 52 respectively.
At 1,024 exclusions, the delivered incremental saving is 1,024 allocations
and about 48 KiB per call. The complete runtime adapter still allocates;
only the temporary guard slice disappears.

All four broad-refusal cases retain their error and avoid one allocation,
saving 16, 32, or 48 bytes according to the old split shape. Neither
refusals nor the healthy single-exclusion case support a universal
latency claim.

The primary matrix also contains unfavorable nominal timing comparisons:
active=1 Git adaptation is +1.72% (p=0.011), and directory adaptation with
1,024 retained skills is +4.33% (p=0.015). Those paths do not execute the
Marketplace guard. Keep those samples, rather than describe every control
as unchanged.

## Separate Confirmation

Use the same frozen binaries in six alternating pairs, two seconds per
case, for the two nominally slower controls, the corresponding retained-Git
control, and the primary 1,024-Marketplace-exclusion case: 48 more rows.

| Case | Before | After | Comparison |
| --- | ---: | ---: | --- |
| Directory, 1,024 retained | 222.1 us | 221.5 us | not significant, p=0.937 |
| Git, active=1 | 685.8 ns | 687.0 ns | not significant, p=0.818 |
| Git, 1,024 retained | 223.0 us | 224.1 us | not significant, p=0.394 |
| Marketplace, 1,024 exclusions | 112.24 us | 94.54 us | -15.76%, p=0.002 |

The nominal healthy-path slowdowns do not reproduce in this separate run.
That is not proof of unchanged latency on every workload or machine.
The Marketplace improvement and allocation reduction persist. No
whole-parser, I/O, acquisition, budget, or universal speed-up is claimed.

## Preservation And Negative Controls

- Both production variants pass the same private-input replay table across
  all four layers, including empty components, missing/extra separators,
  literal and embedded wildcards, collection spelling, NUL, invalid UTF-8,
  and long names.
- The test-only oracle is the former split predicate, not the new guard
  or a validating parser. Successful replay checks endpoint, byte-exact
  locator, user source, exclusion-only acquisition, and a retained Git
  command. Refusals pin exact diagnostic identity and layer precedence.
  Adaptation preserves the normalized input, including raw byte strings.
- Public witnesses cover all five non-collection kinds and all four broad
  refusal shapes through `ParseV4` and `ToLegacyConfig`.
- Before and after fuzzing pass 84,122 and 118,444 executions respectively.
  These are correctness exercises, not benchmark throughput comparisons.
- All four semantic mutations are rejected. Removing collection, wildcard,
  or arity checks fails through public consumers. Removing the remaining
  slash check fails private replay while public controls still pass:
  malformed four-component authored locators are rejected earlier by the
  parser. Do not mislabel that last control as public-path coverage.
- Focused config/Marketplace/effective-request/copy-preservation races pass
  in 2.011 seconds.
- Allocation-space profiles show the old split contribution disappearing.
  Nonempty disassembly of the compiled runtime adapter contains a
  `strings.genSplit` call before, and no such call after. Profiling runs
  are not part of either paired dataset.

## Consumer Validation

Offline module-tidy verification passes for the root and E2E modules.
All fresh consumer gates pass on the terminal-QA-integrated base:

- `make validate` with explicitly selected config, pipeline, resolver, and
  TUI packages at `-count=1`; vet, zero-issue CI lint, and build.
- Strict specs: 67 items, 1,216 scenarios, 2,000 annotations, two active
  changes.
- All 67 real-terminal journeys plus four utilities, zero skips.
- Combined E2E: 766.456 seconds, 3,455 invocations, all 113 command/flag
  requirements covered, no gaps, all three required refusal witnesses
  executed/passed, and shellcheck.

Host adversarial `open-code-review-delegate` covers all 13 own files,
including excluded tests, documentation, both raw datasets and their
manifests, plus the private measurement/control harnesses. Verify all 432
rows, binary/source identities, stage boundaries, and reported timing
qualifications before publication; do not claim these local selected
packages are the full root test suite. CI owns that wider check.

## Evidence

`security-r5-marketplace-exclusions-2026-10-26/` retains the primary
before/after samples, benchstat output, and manifest.
Its `confirmation/` directory retains the separate four-case dataset
and its original binary identities. Total samples: 432, not a pooled
36-case timing summary.

Initial one-iteration fixture probes are not measurement evidence. A
zero-exclusion/no-lock probe correctly refused under the existing
acquisition contract; remove that invalid fixture rather than relax the
contract. The delivered primary benchmark instead reuses #948's valid
exclusion-only fixture, and healthy hydration is a separate control.
