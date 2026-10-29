## Decisions

- An explicit declaration selects captured regular-file I/O. Recursive legacy
  discovery retains its previous read and publication behavior. Do not infer
  strict eligibility for every JSON file before checking its marker.
- Resolve prospective physical paths with the existing ownership path resolver.
  Reject literal `.git` components and descendants of Git administrative paths
  obtained from the environment or repository/worktree metadata. Discover these
  paths without spawning Git during destination resolution.
- An ancestor containing Git metadata (for example HOME) remains eligible; only
  roots at or inside the protected metadata are refused.
- Keep scope-specific skip decisions local while sharing the pure relative-path
  predicate with the root resolver.
- A component with no desired artifacts and a non-directory destination needs
  no cleanup plan. Keep collision refusal when desired artifacts need it.

## Proof

Unmarked hard-linked JSON reproduces the legacy regression in sync and check,
with a real owned sibling as a positive cleanup control. Administrative-root
fixtures cover literal, configured, common, linked-worktree and aliased paths.
A public materialization witness proves refusal creates no Claude root while
an independent Factory selection still installs.
