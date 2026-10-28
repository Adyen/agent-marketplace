## Context

CodeRabbit identified that the declared-file loop skipped every failed `Lstat`.
An inaccessible path or a directory-link cycle is not evidence of absence.

## Decision

Skip only genuine missing paths and paths below non-directory components.
Convert other inspection errors through the existing per-file diagnostic helper
and continue processing safe siblings. Retain the captured read/write authority
and shared-permission behavior from the preceding boundary fixes.

## Proof

A directory-link cycle reproduces the silent failure without the fix. The same
test proves normal missing/non-directory cases stay quiet and a safe sibling's
stale owned entry is still pruned.
