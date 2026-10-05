## Context

Logical `.git` names are skipped before traversal, but a contained directory alias can rename an excluded source subtree.

## Goals / Non-Goals

Keep real support copying and read-only checking aligned without changing file alias policy or introducing another filesystem lookup.

## Decisions

Apply the existing `.git` exclusion to resolved directory target components during shared classification. Return the standard directory-skip identity only after captured-directory cleanup succeeds. Both consumers skip that directory before creating a logical output node.

## Risks / Trade-offs

Only exact `.git` components are excluded; similarly named ordinary support directories remain usable.

## Migration Plan

No configuration or ownership-state changes.

## Open Questions

None.
