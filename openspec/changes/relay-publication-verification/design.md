## Context

A queue-driven delivery exercises the full review-and-publish path on a
disposable branch. The branch content must be harmless to merge and must
identify itself as a verification artifact.

## Decisions

- The verification artifact is a single Markdown note under `docs/` so the
  branch carries a real, reviewable diff without touching product surfaces.
- The note states plainly that the branch is a disposable verification of
  reviewed branch publication and not a product change, so reviewers and
  future readers cannot misread it as feature work.
