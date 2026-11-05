# Design

## Context

Binding a Marketplace layer raises `v4.marketplace-lock-required` both when the layer has no lock document and when the document is present but unusable (for example a v1 lock). Malformed, unreadable, untrusted, and wrong-scope organization locks fail earlier with `organization.trust.lock.*` codes; stale and mismatched locks fail with `lock.stale` or `v4.marketplace-lock-mismatch`. The hint catalog keys guidance by code and reads scope, locator, lock file, and variant from the error.

## Goals / Non-Goals

**Goals:** tell the absent organization lock apart from every present-but-unusable lock in the hint, without changing the error code or refusal.

**Non-Goals:** see proposal.md; no new error code, no change to acquisition or lock writing.

## Decisions

- Mark the absent-document refusal with a diagnostic variant on the existing error rather than a new code. A new code would change error identity, JSON output, and the stable code catalog; a variant only selects guidance.
- Select the new hint only when the scope is organization and the variant marks an absent lock. Every other organization case falls through to the existing administrator guidance.

## Risks / Trade-offs

- [A future path raises the absent-lock variant for a present lock] → regression tests cover each present-but-unusable organization lock and assert the administrator hint.
