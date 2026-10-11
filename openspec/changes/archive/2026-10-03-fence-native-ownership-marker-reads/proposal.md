## Why

Native `_managed_by` ownership checks still open paths independently, import multiply linked marker claims and can block on FIFO payloads. Actual Factory command update treats a hardlinked installed marker as ownership and overwrites its native generation.

## What Changes

- Read native ownership marker payloads through captured regular/single-link/nonblocking opened-file authority at captured size, refusing final leaf aliases.
- Retain leading-frontmatter marker interpretation and the existing bool/collision preservation contract; close failures cannot prove ownership.
- Preserve valid native marker install/update/no-op behavior without new arbitrary size limits.

## Capabilities

### Modified Capabilities

- `materialization`: native marker payload read authority.

## Impact

Dependent R2 child of #490. Native command leaf publication/pruning generations, directory discovery, aggregates, restore/ownership and final integrated/case audit/GitHub comments remain open.
