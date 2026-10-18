# R5 V4 Adaptation: R52 / R53

Baseline: `06a6fdbd` on main, after PR #730. Frozen ledger and source index
remain unchanged. No D1-D6 policy or new input/result budget is introduced.

## Confirmed Candidates

- R52: direct Marketplace request lookup is already indexed. Collection
  reachability still repeatedly scans resources until a fixed point, making
  a valid child-before-parent chain quadratic.
- R53: effective exclusions replay the complete ordered request stream for
  each distinct exact exclusion, including sorting the same final requests.

## Approach And Alternatives

Use a reverse-edge worklist for R52's reachability set, visiting admitted
edges and reachable resources once. Preserve collection identity/revision
checks, direct seeds and output/diagnostic order. This does not deduplicate
collection expansion: only the existing Boolean reachability set changes
implementation; selected-by output remains unchanged.

For R53, lazily compute effective requests once, retaining a locator set for
exact reinclusion checks. Empty/wildcard-only exclusions need no snapshot.
Keep the original decision interpreter and sorting unchanged.

Retaining scans leaves the measured repeated work. Sorting could support
some lookups but does not remove fixed-point graph passes or repeated
decision replay. Invocation-local worklists/reuse are the smaller change;
measure their additional allocations rather than introduce a global cache.

## Verification

Before editing production code, run authored reachability/refusal and
selection compatibility controls against the original algorithms, plus
identical-fixture public-adapter benchmarks. Verify bounded fixtures encode
below existing file limits and exercise the real decoder/file loader.
Use baseline algorithm oracles and bounded structured Rapid properties
for decision and reachability semantics.

After implementation, rerun preservation controls, public adapters,
config/pipeline/lockfile consumers, focused races, scoped validation, strict
spec checks and combined E2E. Compare raw samples including allocation
costs. Exercise negative controls, run host OCR-delegation adversarial
review over every changed file including default exclusions, and commit
with a justified `Spec-Impact: none` trailer if behavior stays unchanged.

R52/R53 have broader obligations than these two loops. Report residual
normalization, wildcard interpretation and graph/budget work explicitly;
do not equate a measured improvement with a global processing bound.
