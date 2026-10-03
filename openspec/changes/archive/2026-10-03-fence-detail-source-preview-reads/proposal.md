## Why

Detail source capture and selected filesystem bundle previews still open paths directly. O_NOFOLLOW blocks final symlinks but FIFO opens can block before regular-file checks, and hard links can supply unintended payloads. Both consumers already have bounded truncation policies which must remain intact.

## What Changes

- Reuse captured directory/destination streaming reads for both source capture and selected bundle previews.
- Refuse nonregular/multiply linked payloads before byte reads, validate opened state, and retain the existing final-symlink refusal.
- Preserve per-source and aggregate capture limits, truncation markers, archive-backed previews and rendering semantics without whole-body buffering or new budgets.

## Capabilities

### Modified Capabilities

- `registry-symlink-containment`: descriptor-bound filesystem source preview reads.

## Impact

R2 reader child of #483. Source capture roots remain the resource containing directory; selected bundle reads use their explicit resource root. No whole-source atomic snapshot or output/prune/aggregate/restore/ownership closure.
