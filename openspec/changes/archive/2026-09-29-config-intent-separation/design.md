## Context

Layer parsing previously mixed authored selection, lock reads, and acquisition. See proposal.md and specs/configuration-intent/spec.md.

## Goals / Non-Goals

**Goals:** Capture validated config bytes and provenance once, then bind exact remote resources only for operations requiring them.

**Non-Goals:** Change config precedence or lockfile schema.

## Decisions

- Represent validated authored intent separately from acquired resources. Binding during parsing would make harmless config reads depend on network or lock state.
- Capture the canonical identity of included files at parse time. Re-reading after validation could bind different bytes or a retargeted symlink.
- Preserve existing error precedence during later binding; parallel acquisition could reorder failures.

## Risks / Trade-offs

- Captured content consumes memory while a command runs. Bound size limits on config inputs keep this bounded.
