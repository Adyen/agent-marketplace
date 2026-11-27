# Design

## Context

Latest resource detail computes resolved prerequisite presentation from current heads. Exact revision detail currently returns revision metadata without prerequisites, although `ResourceRevision.Requires` is stored per content digest.

## Goals / Non-Goals

**Goals:** Deliver the exact declaration for eligible, opted-in Place package revisions, and refuse acquisition when it is absent or `null`.

**Non-Goals:** Change latest-detail resolution, `required_by`, storage, locks, graph identity, or historical revisions.

## Decisions

- Exact Place revision detail converts the stored array to kind/name-only declarations. It performs no per-entry lookups and emits neither hints nor `resolved` data. Only canonical same-target, non-self declarations are eligible.
- Eligibility follows existing availability policy: available or operator-exposed, and not revoked. Other exact resource responses remain unchanged.
- Online and cache-only acquisition require an indexed array before archive access. `[]` is known-empty; absent and `null` are unknown and refuse. Lock-evidence mismatch keeps its existing refusal priority.
- Historical exact requests use the requested content digest and its stored declaration, not the current head.

## Risks / Trade-offs

- **Old cached Place metadata has no prerequisite evidence** -> Refuse it without reading the archive and direct the user to refresh online; do not infer `[]`.
- **Malformed stored declarations could create a misleading graph** -> Refuse the exact response instead of returning partial or resolved data.

## Migration Plan

No migration is required. Existing cache metadata remains readable, but unknown Place prerequisites are refused until refreshed from an eligible exact revision endpoint.
