# R5 / R53: Slice-Free Wildcard Matching

Start from main `e1c844c3`, independently of PR #888. This is another
partial R53/F136 repeated-work investigation, not a new policy or closure.

`v4ExclusionMatches` splits every wildcard comparison's locator into a fresh
slice. Besides rebuilding and effective-request interpretation, callers in
the low-level v4 editor, layer editor, and Marketplace editor use the same
matcher for wildcard refusal and inherited suppression. All must retain
identical results and diagnostic/write behavior.

Measure before changing anything. The candidate replaces locator splitting
with two standard-library `strings.Cut` calls. Preserve two/three-component
recognition, empty components, rejection of extra slashes by wildcard
matching, global `*`, and final literal equality even for malformed private
inputs. Do not replace this with a permissive prefix test.

Keeping `Split` remains the alternative if measurements do not justify the
change. A wildcard index would add state and change much more code; defer it.
No new limits, deadlines, truncation, cache, dependency, or accepted-input
restriction is needed.

The initial locator-only version keeps qualified-selector concatenation.
Gemini review identifies its remaining allocation for long names. Measure
that case independently and refine qualified comparison with a third
standard-library `Cut`, not manual length/index arithmetic, preserving the
same raw-shape contract. Retain the intermediate dataset separately.

Pin raw matching results against the independently authored token-based
oracle, including malformed strings, and retain structured-history controls.
Verify public parsing, runtime adaptation, editor refusal/healthy-control
behavior, input bytes/metadata, and diagnostic identity. Use semantic
mutation controls, bounded fuzzing, and races.

Benchmark matching, public parsing, and public runtime adaptation separately.
Keep fixture construction, disk/network work, and assertions outside timing.
Retain exact/global healthy controls and an unexcluded command in public
kind-wildcard fixtures. Use immutable variants, canonical physical overlay
paths, distinct build IDs, smoke verification, and six alternating two-second
pairs. Record allocations and uncertainty; no global CLI or scan-bound claim.

Run selected validation, specs, combined E2E, terminal tests, and host
`open-code-review-delegate` across all changes before every push/handoff.
Publish and address actionable CodeRabbit/Gemini/CI feedback; ignore major-
release commentary and leave the clean PR open before taking another slice.

Before publication, integrate main `e8dcaba9`, including merged #888 and
upstream TUI/deployment-security work. Refresh both measured variants on this
base, keep the initial `e1c844c3` dataset separate, and do not pool results.
