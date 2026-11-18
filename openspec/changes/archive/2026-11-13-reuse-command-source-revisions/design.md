## Context

The lock operation builds a source revision and catalog during acquisition. After exact Git snapshot validation, snapshot binding builds again to compare with the locked projection. The first result can be passed within the same operation; snapshot validation and the locked comparison remain required. See proposal.md for motivation and scope.

## Goals / Non-Goals

**Goals:**

- Reuse the acquisition result in snapshot binding when it matches the binding request.
- Keep the comparison with the locked projection after exact snapshot cache revalidation.
- Preserve the existing `Build` fallback for requests without a matching result.

**Non-Goals:**

- Store payload bytes or add a cache that survives the lock operation.
- Reuse results across independent commands.
- Change lock or revision serialization.

## Decisions

### Pass built results within the lock operation

Carry the source revision and catalog built during acquisition into snapshot binding. Keep them operation-local so parallel or later commands cannot reuse stale results. A process-wide cache would add invalidation and synchronization without helping this single-operation case.

### Revalidate, then compare the locked projection

Keep exact Git snapshot cache revalidation before comparing the available build with the locked projection. Use the same projection comparison for a reused result and for the existing `Build` fallback. This avoids changing comparison semantics; skipping revalidation would weaken source verification.

### Retain `Build` when no matching result exists

Use an acquisition result only when it matches the snapshot binding request. Otherwise, keep the existing `Build` path, including historical and plugin contexts that do not carry a matching acquisition result. A mandatory result parameter would break those paths.

## Risks / Trade-offs

- A mismatched acquisition result could be compared with the wrong locked projection → require a match for the binding request and test both reuse and fallback.
- A changed comparison could alter lock results → route both paths through the common projection comparison and test unchanged lock bytes.
- Reuse could be mistaken for permission to skip source checks → keep exact snapshot revalidation and fresh source verification ahead of comparison.

## Migration Plan

No data migration is needed. The operation-local result is transient, and lock and revision formats remain unchanged. Reverting the code restores the existing repeated `Build` path.
