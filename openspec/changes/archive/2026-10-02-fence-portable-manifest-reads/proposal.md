## Why

Portable materialization manifests contribute ownership claims and target work. Path-based loading follows foreign aliases before parsing, so a manifest alias can authorize a native file-mode update using foreign claims.

## What Changes

- Capture the repository/HOME manifest scope and contained parent, then open the manifest without following a leaf alias or blocking on special files.
- Read one regular singly-linked admitted generation with captured-size streaming/growth and identity validation.
- Preserve the stable manifest diagnostic, absent-manifest compatibility, contained parent/scope aliases and valid native ownership updates.

## Capabilities

### Modified Capabilities

- `materialization`: portable manifest payload admission before ownership claims.

## Impact

Scoped manifest reader in the R2 delivery queue under #215/#431. Manifest publication/removal, portable destination digests/pruning, primary/hash readers, aggregate/restore/ownership consumers remain separate. No arbitrary absolute input or work budget.
