# R5 / R53: Exact Exclusions In Effective Requests

## Scope

Start from main `e5f27f36`, which includes R53 PR #868 (merged as `22f16cff`).
That PR is already merged when the user requests queue admission; no new
enqueue or merge operation is performed.

Subsequent main `0e8b2ac2` contains only five additional TUI QA files relative
to that base; integration preserves the measured production and benchmark
blobs. Upstream terminal journeys are not own R53 changes.

The previous slice optimizes `rebuildRequests`. Its sibling ordered
interpreter, `effectiveV4Requests`, still scans its entire request map for
every exclusion. Git/directory runtime adaptation, Marketplace lock
adaptation, effective exclusion derivation, and explicit intent selections
consume this helper.

This is another partial R53/F136 repeated-work optimization, not a security
closure or a new accepted-input, size, deadline, truncation, or D1-D6 decision.
The frozen ledger and source index remain unchanged.

## Choice

Reuse the existing request map for selectors that are neither `*` nor suffixed
by `/*`. Mark only a present, non-required request excluded, then continue.
Keep the existing wildcard scan, exclusion-state map, ordering, and output
construction untouched.

This avoids expected selected-requests-times-exact-exclusions map scans,
without adding another index or persistent cache. String hashing and the
existing final sort remain. The previous `rebuildRequests` fast path uses the
same classification.

Alternatives are deliberately deferred:

- Sharing the two interpreters changes more code and risks their distinct
  include-only fallback and empty-result contracts.
- A wildcard index adds state and allocations, and needs separate measurement.
- Caching results across adapter calls risks stale decisions and changes caller
  structure; it is unnecessary for this local exact-lookup improvement.

## Proof

Pin exact, missing, required, replacement, reinclusion, future-request,
kind/registry boundaries, wildcard behavior, empty non-nil output, ignored
include-only fallbacks, and input immutability. Compare generated histories
against the existing independently authored per-locator oracle, omitting its
rebuild-only include fallbacks. Exercise public runtime and intent consumers;
retain existing Marketplace and effective-exclusion regressions.

Benchmark the helper and public `ToLegacyConfig` adapter at several bounded
sizes, with one-exclusion and wildcard controls. Each exact fixture keeps an
unexcluded command as a positive selection control. Reuse existing builders.
Exclude fixture parsing, disk/network I/O, and result assertions from timing.
Retain final results/errors for post-loop checks. Use identical benchmark
sources, immutable binaries, canonical physical overlay paths, distinct build
IDs, and a slow/fast smoke check before six alternating before/after pairs.
Record raw output, allocations, significance, and limits without pooling
different definitions or claiming global CLI speed-ups.

Run scoped validation, strict specs, focused race/property checks, semantic
mutation controls, and the combined E2E/configuration gate. Review every own
changed file, including tests and raw evidence, with host
`open-code-review-delegate` before publication or handoff.
