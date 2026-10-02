## Why

Cache-backed link publication validates ownership and stages an output, then reopens the destination by path. A replaced parent can strand staging in the captured tree or make publication/cleanup affect a replacement tree. Root admission alone does not bind the final operation.

## What Changes

- Keep the admitted destination directory captured through cache-backed symlink staging, publication and cleanup.
- Revalidate parent and observed leaf generation before rename; refuse drift instead of replacing another object.
- Preserve ownership skips, byte-neutral repeat sync, supported root aliases and existing file-mode migration.

## Capabilities

### Modified Capabilities

- `materialization`: cache-backed link publication generation and refusal preservation.

## Impact

Unix materialization and shared filesystem-boundary capture. No new processing ceilings, acquisition formats, read budgets or credentials. Stale-entry pruning, source/support reads, portable manifest publication, aggregates, restore and durable ownership remain separate work.
