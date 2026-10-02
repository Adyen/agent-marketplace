## Why

Skill cache preparation currently copies source support files through path-based reads. A foreign link can import external bytes, and a later source refusal can already have replaced compiled bytes in an installed cache.

## What Changes

- Capture one authorized source root and enumerate through captured directory handles.
- Authorize regular, singly linked source snapshots before reading bytes/mode; keep contained relative aliases.
- Stage support payloads before changing the installed compiled/support cache, preserving its generation on source refusal.

## Capabilities

### Modified Capabilities

- `materialization`: scoped skill support payload reads and source-refusal preservation.

## Impact

Single R2 skill cache support-reader problem under #215/#431, dependent on #466. No new absolute read or tree-work threshold. Hook, primary, portable/dry-run digest readers, cache destination publication/pruning and native ownership remain separate work.
