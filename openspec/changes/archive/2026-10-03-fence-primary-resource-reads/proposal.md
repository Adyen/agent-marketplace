## Why

Disk-backed catalog ingestion and bundle detection use os.DirFS, allowing an aliased primary resource to contribute foreign payload bytes and permitting special payload opens to block. Current support materialization cannot undo that earlier import.

## What Changes

- Provide a lazy captured-root fs.FS view whose payload reads use the existing strict destination/snapshot boundary, not a pathname reopen.
- Route catalog ingestion and disk bundle/root detection through that view, keeping valid sibling listing and contained relative primary aliases.
- Reject foreign/special/multiply linked payloads without new absolute byte/tree/work ceilings; capture growth bound comes from opened file size.

## Capabilities

### Modified Capabilities

- `registry-symlink-containment`: disk primary payload admission and checking.

## Impact

R2 reader foundation stacked on #477. Source-revision hashing, collection/legacy primary helpers and remaining output/aggregate/restore/ownership consumers remain separate until audited and delivered. No whole-source immutable-tree or global-limit claim.
