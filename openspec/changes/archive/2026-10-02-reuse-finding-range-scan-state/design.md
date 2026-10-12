## Context

SourceRangeAt decodes from the start of content for every finding. Ordered scanner matches can reuse the already-decoded prefix instead, preserving the same source-range authority without storing a global text cache.

## Decisions

1. Keep a cursor local to each affected scanner invocation. Forward positions advance from the last decoded rune; backward positions reset to retain existing byte-offset behavior. Invalid offsets still yield a file-only range.
2. Reuse that same decoder for standalone source-range conversion so rune/newline policy does not diverge. No shared mutable state, global text retention or detection-rule change.
3. Scope is four ordered scanner loops, not all source-range or graph-processing paths. Existing budgets and finding order remain unchanged.
4. Performance-only regression evidence is the existing prefix-rescan baseline and a bounded public dense-comment scaling benchmark; functional controls are expected to pass before and after. Avoid brittle wall-clock unit thresholds.

## Verification

Independently authored public scanner expectations preserve Unicode columns, count, file/analyzer and ordering. Check other affected scanner controls, malformed bytes and standalone offsets, Linux/macOS, packages/service gates, repeated races, strict specs, before/after scaling and complete adversarial review.
