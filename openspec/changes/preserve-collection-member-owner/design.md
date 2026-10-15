## Context

Collection resolution already chooses an owner and carries its catalog, source location, configuration layer, include-file precedence, required policy, and effective harness targets. Unqualified member expansion then performs another URL/ref/path lookup and replaces that metadata with the first matching entry. A physical registry location does not identify a configuration-layer owner.

## Decision

Remove the redundant lookup. An unqualified member uses the metadata already carried by its resolved collection. The existing effective member harness calculation inherits the parent's targets when no external owner is selected. Recursive expansion also carries the already-selected nested owner so an incompatible organization member retains its required error or default warning, even when its parent narrows the effective harness targets to empty.

Changing only the member's scope would leave required policy, include-file precedence, and harness selection vulnerable. Adding a second owner-selection algorithm would duplicate a decision already made and still require distinguishing same-location entries. Reusing the resolved artifact is smaller and preserves the complete decision.

Explicit registry references retain their current containment, ref/path pinning, required precedence, exclusions, and harness eligibility rules. Nested collection lookup also retains its current authority rules; after a nested definition is chosen, its unqualified members retain that definition's owner.

## Verification

Resolver regressions cover every materialized resource kind, repeated locations across layers, both install scopes, required members and misses, include-file precedence, root versus subtree locations, and nested definitions. Declarative E2E cases exercise supported v4 configurations, native output destinations, absence of unintended output, and read-only drift checks.

Measure the existing in-memory, no-disk, no-network cross-scope collection resolver benchmark before and after the change. Run affected package tests, selected consumer regressions, validation, and the combined E2E and strict specification gates. Review the complete diff before delivery.

## Results

All three new declarative E2E cases failed before implementation and passed afterward: repository-only output was incorrectly written to machine scope, dual-scope selection lost its repository copy, and required policy allowed repository shadowing. Focused resolver regressions also failed before implementation. The complete resolver, pipeline, and TUI short-mode packages, selected command collection/scope regressions, `make validate`, `make spec-check`, and `make validate-e2e` pass. The combined gate exercised 2,741 command invocations and all 120 command coverage requirements; all three required consumer-format witnesses passed.

On macOS arm64, five 2-second samples of `BenchmarkResolveCrossScopeCollectionMembers` measured median time 623,993 to 633,754 ns/op (+1.56%), bytes 1,356,608 to 1,356,633 B/op (+0.0018%), and unchanged 3,619 allocs/op. Benchstat reports no statistically significant time difference (p=0.222); the tiny byte increase has p=0.008 before correction. Samples ran on a shared host and five samples do not support a 95% confidence interval. These observations do not establish equivalence or causation.

OCR delegation preview and rule resolution covered the implementation diff against `62946de3`, with manual review of the default-excluded tests and Markdown artifacts. Review found that removing the lookup also removed nested incompatible organization diagnostics; a failing regression demonstrates both required errors and default warnings, and carrying the already-selected nested owner fixes both. Review traced the pipeline consumer, recursive expansion, exclusions, required failure severity, harness inheritance, destination scope, and regression non-vacuity; explicit registry binding and nested definition selection remain unchanged.

After fixing that finding, all eight implementation files were reviewed, none skipped (100% coverage), with no remaining actionable findings. The nested harness diagnostic regression also passes with the original resolver through a Go source overlay, proving this retains existing failure behavior rather than adding a new refusal.
