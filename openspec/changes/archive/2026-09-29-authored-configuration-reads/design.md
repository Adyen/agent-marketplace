## Context

Ad-hoc browsing and prune-stale labels only need authored policy and selection names. See proposal.md and specs/authored-configuration-reads/spec.md.

## Goals / Non-Goals

**Goals:** Avoid unrelated remote acquisition while retaining allowlist precedence and fail-closed browsing validation.

**Non-Goals:** Change bound search, lock reconciliation, doctor diagnostics, or prune-stale deletion authority.

## Decisions

- Read validated intent for the allowlist and selection labels. Binding remote sources to print these values would fail on unrelated missing locks.
- Use the highest-precedence authored allowlist even if its layer cannot bind; falling back to a lower layer would silently change policy.
- Keep URL validation before any ad-hoc registry fetch and leave pruning decisions with the existing bound path.

## Risks / Trade-offs

- Informational labels can include selections from a layer whose remote resources are unavailable; no deletion authority is inferred from those labels.
