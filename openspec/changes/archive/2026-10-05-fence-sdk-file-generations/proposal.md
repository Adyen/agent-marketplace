## Why

SDK Materialize compares original output generations safely but discards their identities before publication. A replacement file introduced after staging is overwritten, and an originally absent destination that appears can also be overwritten.

## What Changes

- Carry original opened file identity and absence from the existing private comparator into SDK artifact planning.
- Derive preserved merge-file mode from that opened identity rather than another path Lstat.
- Revalidate full destination file state/absence below captured root before rename.

## Capabilities

### Modified Capabilities

- `materialization`: SDK original destination-file publication authority.

## Impact

R2 child of #544 in stack #478. Existing Identity/descriptor/publication operations; private comparator gains identity output, no new helper/type/public API/dependency/budget/source comments. Modes/no-op/staging-all-artifacts/dry/partial contracts preserved. Original staged-file generation, stale discovery/removal and final admitted-case audit remain separate; no whole-tree transaction or atomic uncooperative-writer exclusion.
