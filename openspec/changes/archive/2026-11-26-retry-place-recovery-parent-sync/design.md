# Design

## Context

See proposal.md — Why. Place recovery already records marked mutations and verifies destination generations. The change is limited to durability confirmation when recovery finds a mutation's intended result already visible.

## Goals / Non-Goals

**Goals:**

- Sync the verified effective destination parent before advancing marked publication or regular-file prune progress when the mutation is already visibly complete.
- Preserve the journal operation and completed count on sync failure so a retry can confirm durability without repeating the mutation.

**Non-Goals:**

- Change journal format, mutation ordering, generation checks, or recovery behavior outside the marked visible-result cases.
- Add sync requirements to genuine no-ops or read-only inspection.

## Decisions

- Use the existing parent-sync seam after validating the intended visible result and before advancing recovery progress. This keeps durability confirmation scoped to the already-marked operation; re-running publication or prune would risk repeating a completed mutation.
- Apply the same ordering to regular-file publication, ownership publication, and regular-file prune, while retaining each operation's existing identity and generation checks. A failed sync propagates as the recovery error and leaves the journal state pending.

## Risks / Trade-offs

- **A parent sync failure may keep recovery pending across retries** → Preserve the exact pending operation and completed count; advance only after a successful sync.
- **Syncing an unverified parent could confirm the wrong directory** → Perform the operation's existing destination and parent verification before syncing.

## Migration Plan

No migration is needed; the journal format and persisted operation records remain unchanged.
