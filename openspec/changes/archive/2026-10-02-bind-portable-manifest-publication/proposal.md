## Why

The admitted portable manifest reader is descriptor-bound, but save/remove still resolve the destination by path later. A valid native file-to-symlink migration can delete or overwrite a manifest generation installed after its ownership claims were loaded. Identical manifest saves also rewrite bytes and lose user permissions.

## What Changes

- Retain the admitted manifest bytes, full file identity and scope/parent identities for generation checks before publication/removal.
- Capture scoped parents and direct regular destinations for descriptor-relative staging, rename, removal and attempt cleanup.
- Recheck observed namespace authority after staging; reject changed scope/parent/leaf generations with the existing manifest diagnostic.
- Preserve unchanged manifest identity/mtime and existing permissions on real updates; retain valid contained aliases and empty-manifest removal.

## Capabilities

### Modified Capabilities

- `materialization`: portable ownership-manifest publication/removal authority and faithful no-ops.

## Impact

R2 delivery under #215/#431 after merged #474. This is manifest namespace protection, not a whole-installation transaction: native output may already have legitimately changed before a later manifest refusal. Portable output hashing/pruning, primary readers, native aggregates, restore and durable ownership remain separate. No new absolute input or work budget.
