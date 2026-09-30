## Context

Remote resource binding needs verified acquisition evidence, while organization deployments may intentionally omit a lock. See proposal.md and specs/deployed-acquisition-locks/spec.md.

## Goals / Non-Goals

**Goals:** Separate deployed evidence inspection from online acquisition and preserve fail-closed validation of existing locks.

**Non-Goals:** Make malformed deployed locks recoverable by replacing them or change lockfile format.

## Decisions

- Inspect deployed locks as evidence and acquire only when the operation permits it. A synthetic placeholder must never masquerade as deployed authority.
- Distinguish missing organization evidence from stale present evidence. Replacing stale locks automatically would erase an administrator's policy boundary.
- Keep writable layers subject to their exact lock requirements during read-only inspection.

## Risks / Trade-offs

- An online consumer may need to acquire resource evidence when no organization lock is deployed. The request still passes existing policy validation before use.
