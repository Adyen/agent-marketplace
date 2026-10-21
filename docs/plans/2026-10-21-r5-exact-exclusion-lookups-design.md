# R5: Exact Exclusions During V4 Request Rebuilding

## Scope

This is a follow-on to frozen R53/F136, measured from main `d59363cf`,
independently of R10 tag-streaming PR #828. The slice is subsequently based
on cached main `733e1e65`, which contains the separately merged R10 PR and
a TUI QA journey; its v4 request-rebuilding baseline is byte-identical.
The frozen ledger and source index remain unchanged.

`rebuildRequests` already indexes the current request for each locator.
Nevertheless, every exclusion scans the entire index, including exact
exclusions which can match only one key. Measure that repeated work before
changing it.

## Approach

Use the existing locator map for exclusions other than `*` and `.../*`.
Only exclude a request already present and not required. Preserve the old
scan for wildcard exclusions, including its match semantics. Do not add an
index, cache, dependency, limit, deadline, or public API.

Alternatives are retaining the full scan, which leaves the measured
repeated work, or indexing wildcard families, which adds bookkeeping and
requires separate evidence. Neither is part of this narrow optimization.

## Preservation

- Later explicit requests still replace earlier options and clear exclusion.
- Required requests survive exclusions; later optional replacements do not
  inherit earlier protection.
- An exclusion does not suppress a future request or an include-only fallback.
- Source groups and final locators retain their sorted order; decision streams,
  provenance, request values, and input groups remain unchanged.
- Wildcards keep their current source/kind matching, including nonmatches.
- Existing v4 validation, includes, acquisition locks, selection precedence,
  native output, and refusal behavior remain authoritative.

No user-observable behavior change is intended, so use a reasoned
`Spec-Impact: none` trailer rather than a spec delta.

## Evidence

Keep CPU/allocation benchmarks for the rebuilding loop and public `ParseV4`
consumer at several input sizes, with a wildcard control. Fixture generation
is outside timing; parsing is timed only in the public-consumer benchmark.
Neither benchmark includes disk I/O, Git, or network requests.

Add deterministic and structured property checks using an independent
per-locator interpretation of ordered decisions. Exercise the supported
parser/include consumer as well as direct rebuilding. Confirm that mutations
to required protection, reinclusion, and wildcard handling fail the checks.

Run focused config/consumer tests, race checks, selected validation, strict
spec checks, and the combined E2E/configuration-compatibility gate. Perform
host adversarial review through `open-code-review-delegate` over every changed
file, including default-excluded tests, documentation, and raw evidence.

This addresses only exact-exclusion lookup work in rebuilding. The ordered
request interpreter, wildcard scans, include-tree work, and aggregate
processing budgets remain separate; R53 is not globally closed.
