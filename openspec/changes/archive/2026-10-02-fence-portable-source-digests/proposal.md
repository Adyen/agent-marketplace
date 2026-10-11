## Why

Read-only portable source hashing still enumerates and reads skill support by path, unlike the already-admitted mutating support copy. Foreign aliases can therefore contribute bytes to check-time ownership decisions.

## What Changes

- Reuse the captured source directory enumerator and regular singly-linked snapshot reader for virtual portable support hashing.
- Keep contained relative file aliases, executable modes, compiled-file and VCS exclusions, virtual ordering and existing optional source behavior.
- Refuse unsafe support payloads through the actual file-mode checking consumer with no output/state mutation.

## Capabilities

### Modified Capabilities

- `materialization`: portable source-digest read authority.

## Impact

Next R2 hash-reader layer under #215/#431, stacked on #475. No new absolute size/work budget. Primary ingestion readers, portable destination digests/mutation/pruning and other R2 consumers remain separately tracked.
