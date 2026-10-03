## Why

Portable regular-leaf digests now validate opened files, but directory traversal still reopens paths after classification. Replacing a directory with a foreign alias after its digest header imports foreign bytes into the ownership digest.

## What Changes

- Enumerate and descend portable digest trees through captured directory descriptors and scoped native roots.
- Validate observed/captured directory state and named generations before and after enumeration, while keeping regular-file descriptor validation.
- Preserve legacy lexical ordering, paths/modes/bytes and structural symlink-text encoding; introduce no new processing budgets or whole-tree atomic snapshot claim.

## Capabilities

### Modified Capabilities

- `materialization`: captured portable tree digest traversal.

## Impact

R2 dependent child of #485. Publication/removal/pruning generations remain subsequent layers. Source comments remain unchanged.
