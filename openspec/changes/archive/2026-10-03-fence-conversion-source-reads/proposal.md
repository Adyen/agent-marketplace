## Why

Direct convert uses a path-backed DirFS for autodetection and ingestion, bypassing native captured primary-read authority. Foreign primary aliases are accepted and can replace prior converted output even in sync mode.

## What Changes

- Reuse one captured source view for direct autodetection and primary conversion ingestion.
- Propagate typed source refusals before output staging/publication rather than materializing an ordinary partial result.
- Preserve flag validation, source/output overlap and root generation checks, legitimate contained authored file aliases and normal partial syntax semantics.

## Capabilities

### Modified Capabilities

- `registry-symlink-containment`: direct conversion source payload authority.

## Impact

R2 source-reader child of #481. No new source/tree/work ceilings, whole-tree atomic snapshot, changes to downstream support copying or claim to close output publication/pruning/aggregates/backup/ownership.
