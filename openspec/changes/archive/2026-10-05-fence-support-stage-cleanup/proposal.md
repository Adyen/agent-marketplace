## Why

Producer support staging cleanup deletes through the public pathname and ignores errors. Replacing its directory after support publication deletes the replacement and still permits successful native installation.

## What Changes

- Retain the admitted producer parent descriptor and directory identity.
- Check the current leaf inode before descriptor-relative recursive cleanup.
- Propagate cleanup refusal through the existing error/diagnostic boundary before native link installation.

## Capabilities

### Modified Capabilities

- `materialization`: producer staging-directory cleanup authority.

## Impact

R2 child of #534 in stack #478. Reuse existing captured directories and native removal; no new public API/helper/dependency/budget/source comments. Namespace creation and staged writes remain the next layer. Cache compilation/support publication may already have happened; no whole-tree transaction or atomic uncooperative-writer exclusion.
