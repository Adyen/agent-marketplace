# R5 R48: Preserve Git Root Matching While Removing Repeated Scans

## Scope

R48 / F117 is the next independent slice of #434. The frozen obligation is
to verify nested `ProjectGitRoots` comparisons with equivalent freshness
and error results, not to impose a root, configuration, or acquisition cap.
The immutable ledger is published at commit
`2f55f85f58a3fe3caca4965095bd90f8a1d82518`; this branch starts from
`cb2bf890` on `main`, independently of PR #725. That PR merged during
measurement; final validation rebases onto `790a1570`, without overlap in
the matcher, its benchmarks, or pipeline matching controls.

## Existing Contract

- Process explicit harness requirements before implicit requirements,
  regardless of their authored order.
- An explicit requirement selects the first matching stored root.
- An implicit requirement selects the first already-selected root for its
  path, or the last matching stored root if none is selected.
- Preserve stored root order, complete root/resource metadata, and inputs.
- Normalize requirement paths exactly as before. Missing roots, mismatched
  harnesses, traversal, absolute paths, backslashes, and control bytes refuse
  without returning a partial projection.
- CLI reconciliation, CLI unselected-layer freshness, and read-only
  pipeline freshness use the same helper. No lock bytes, schema,
  acquisition decisions, status codes, or no-op metadata should change.

## Approach

Keeping scans leaves quadratic comparison work. Sorting copies would
require extra ordering/restoration logic and still cost O(n log n).
Invocation-local indexes can preserve the original choices with expected
linear work in roots plus requirements, in addition to path-byte processing.
They consume extra temporary memory; benchmark data must expose that cost
and any small-input slowdown, rather than presenting only the largest win.
Do not add a global cache, retained state, or resource-policy threshold.
The measured candidate does not benefit the one- and 32-root cases
consistently, so either dimension of at most 32 uses the original scans.
This algorithm dispatch is not a limit: larger products still match
completely through the indexes. Index construction follows the first
requirement's path validation, preserving cheap invalid-input refusal.
Each compatibility case exercises both algorithm branches.

## Verification

First run authored selection/refusal controls and the scan-oracle fuzz
seeds against the original implementation. Measure identical fixture code
before and after with `-benchtime 2s -count 5 -benchmem`; keep setup outside
`b.Loop`. Cover explicit, implicit, and paired-harness inputs, including the
single-root common case. Expand the existing CLI projection benchmark to
exercise its real requirement-conversion wrapper, without claiming disk or
network coverage.

After implementation, run bounded differential fuzzing, affected package
tests, CLI exact-lock/root-removal/no-op regressions, focused races, scoped
validation, spec checks, and the combined E2E gate. Perform adversarial OCR
delegation review before pushing. Publish measurements and a narrow R48
disposition; #434 and D1-D6 remain open.
