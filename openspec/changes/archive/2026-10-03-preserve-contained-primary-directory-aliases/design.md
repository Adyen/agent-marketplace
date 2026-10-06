## Context

Directory opening classifies a directory alias as a payload, while primary payload capture refuses aliased ancestors. Native Factory workspace discovery and command loading therefore fail even for contained source aliases.

## Goals / Non-Goals

- Resolve source directory aliases through descriptor-relative captured scope authority.
- Retain existing regular singly-linked file snapshot reads and typed refusal recording.
- Do not relax destination alias policy, add byte/work budgets, or support Windows.

## Decisions

Add a strict captured-directory entry point that accepts contained aliased components without filesystem-path symlink evaluation outside the source scope. Capture the resolved parent before a payload snapshot and verify that the snapshot parent matches the opened directory identity. Keep existing file alias generation and captured-size reads. A valid alias with a missing optional descendant reports absence; a dangling alias target remains an unsafe-path refusal.

## Risks / Trade-offs

Directory aliases are captured per operation, not as a whole mutable-tree snapshot. A renamed configured root cannot redirect an already captured source. Publication continues to propagate source refusals, while native loading retains its existing fatal-directory-error versus valid-sibling payload-diagnostic behavior.

## Migration Plan

No configuration or lockfile migration.

## Open Questions

None.
