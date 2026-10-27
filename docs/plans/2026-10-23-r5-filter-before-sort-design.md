# R5: Filter Excluded Requests Before Sorting

## Scope

This independent R53/F136 slice starts at main `c364c7a6`, including the
allocation-free wildcard matcher and required-request short-circuit.
It changes no consumer policy, accepted input, diagnostic, or output.
The frozen ledger and source index remain unchanged.

## Existing Cost

After final decision replay and include-only fallback insertion,
`V4Config.rebuildRequests` sorts every locator and only then discards
excluded requests. `effectiveV4Requests` already filters before sorting.
Dead locators need neither sorted positions nor request emission.

## Minimal Change

Filter by the final `excluded` map while collecting sort keys; sort and
emit only retained locators. Keep the existing capacity hint and maps.
For N known locators and K retained locators, this collection/emission
stage uses O(N + K log K) work instead of O(N log N). This is not a
complexity or boundedness claim for parsing or decision replay as a whole.

No cache, secondary index, early decision filtering, parser budget,
truncation, dependency, or new abstraction is introduced. Source-group
ordering and per-group lexical ordering remain explicit. Required flags,
replacement/reinclusion, include-only fallbacks, metadata, decisions, and
nil results retain the existing interpreter and emission contracts.

## Verification

- Independent per-locator properties and authored order/required/reinclude
  examples pass on both variants, including 1,000 generated checks.
- Runnable CPU-only rebuild/public-parser benchmarks cover 1, 256, and
  1,024 skills plus a retained-command positive control. Healthy, half-
  excluded, and all-skills-excluded inputs use the public v4 decoder.
- Six interleaved AB/BA pairs use immutable binaries, canonical physical
  overlay paths, identical benchmark source, and two-second samples.
- Semantic mutation controls, focused races, selected validation, strict
  specs, full terminal QA, and combined E2E precede handoff.
- Host `open-code-review-delegate` covers every changed file and all raw
  evidence before publication. CodeRabbit, Gemini, and current-head CI
  must be clear. Leave the PR open with auto-merge disabled.

No OpenSpec delta is needed: a `Spec-Impact: none` trailer records the
behavior-preserving change. R53 and R5 remain partial.
