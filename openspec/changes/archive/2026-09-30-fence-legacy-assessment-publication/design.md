## Context

The legacy publication path is transactional but writes assessments before SetRegistryHead. That head setter accepts an already-current target commit before checking its expectation. A simple unlocked pre-read would still race another publisher.

## Goals / Non-Goals

- Hold the same registry-row fence used by modern publication while checking the effective head and committing legacy metadata.
- Reuse one SQL check across SQLite and PostgreSQL with existing transaction ownership.
- Preserve modern publication identities and safe retries without changing wire or database formats.

## Decisions

Add a metadata-store head assertion backed by a no-op update of the registry row, then compare the effective head within that transaction. Invoke it before any legacy metadata writes. Existing supported adapters implement it; metadata wrappers retain it through the shared interface. Blob uploads still precede metadata and may remain orphaned after refusal, as already documented.

A missing effective head accepts only an empty expectation. An existing head accepts only its actual commit before mutation. An outdated expectation may succeed only as a verified no-op: registry occurrence, requested resource revisions, occurrences, current assessments and collections must already match, and nothing is rewritten. This preserves the existing legacy collection retry contract without allowing stale assessment changes. PostgreSQL also retains the existing per-registry advisory head lock for initialization when no registry row exists; SQLite's no-op update holds its transaction's write fence. Modern identity-bound replay remains unchanged.

## Risks / Trade-offs

Validate an empty first-head case, stale nonempty and empty expectations, transaction rollback, same-commit retries and both supported adapters. Do not claim that a matching commit alone prevents all stale policy views: managed publication fences remain the authority for that separate contract.
