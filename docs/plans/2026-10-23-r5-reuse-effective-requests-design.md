# R5 Reuse Effective Requests During Runtime Adaptation

## Scope

This independent R53/F136 slice starts from main `4dfa9dae`, including #943.
It does not close R53/R5 or change the frozen ledger/source index. No new
limit, deadline, cache, index, or selection policy is introduced.

For Git/directory groups, runtime adaptation computes the effective request
slice to build includes. Exact-exclusion adaptation then interprets the
same group again to determine which exclusions were superseded. That
second interpretation rebuilds maps, sorts, and allocates another slice.

## Smallest Change

Retain the already-computed slice in the existing Git/directory branch and
pass it to `effectiveV4Exclusions`. Its new private parameter is nil when a
caller needs the original lazy behavior, or the complete effective request
slice for the same group.

The helper builds its active-locator map only for an exact exclusion, as
before. A nil argument computes requests at that point; a non-nil argument
is reused read-only. Empty-but-non-nil is a valid computed selection.

Marketplace adaptation explicitly passes nil. Its lazy timing, acquisition
validation, lock hydration, and refusal order are not refactored here.
Other request consumers, including intent selection, are unchanged.

There is no persistent state or cross-group sharing. The caller does not
mutate the group or request slice between request interpretation and
exclusion adaptation.

## Preservation

- Retain final includes, first-exclusion order, exact/wildcard/global rules,
  required protection, replacement, and repeated re-inclusion.
- Keep authored decisions, provenance, harnesses, source/directory identity,
  nil/empty distinctions, and rebuild-only fallback semantics.
- Do not change runtime-option/locator/exclusion diagnostics or their order.
- Do not mutate authored configuration, locks, files, or publication state.
- Keep independent replay/property oracles and test both lazy and supplied
  request modes against the same expected results.

## Measurement

Use the existing public Git adapter's 1/256/1,024 skill fixtures with zero,
half, or all excluded, each retaining an unrelated command. Add an equivalent
CPU-only directory benchmark with parsing/path setup outside timing.

Include the existing active-selection/unselected-exclusion cases, wildcard
controls, and Marketplace lock hydration. Add public runtime adaptation of
64/1,024 repeated exclusion/re-inclusion rounds. This measures expanded
decisions, not include-tree reads. All 25 cases exclude disk/network work.

Compile original and optimized variants with identical benchmark/public
fixture definitions. The original production source and original direct
helper tests are overlaid together because the private helper gains an
argument. Record both source identities rather than claiming those direct
test declarations are byte-identical. Public preservation fixtures remain
identical; the delivered direct tests additionally cover supplied requests.

Run six alternating before/after trials at one second per case. Keep raw
time/byte/allocation results and per-variant source/binary hashes. Validate
all 300 rows/case identities. Do not pool the initial three-sample probe or
claim a universal speed-up from a geomean.

## Gates And Review

Run both variants' preservation tests with 1,000 generated oracle cases,
negative controls for wrong/stale request snapshots and the lazy fallback,
focused races, selected config/consumer validation, strict specs, terminal
QA, and witnessed combined E2E. Record a justified `Spec-Impact: none`
trailer because observable behavior is unchanged.

Run host adversarial `open-code-review-delegate` across every own file,
including excluded tests, evidence, and docs, before every push/handoff.
Follow actionable CodeRabbit/Gemini feedback and CI. Do not enable
auto-merge, enqueue, or merge the PR.
