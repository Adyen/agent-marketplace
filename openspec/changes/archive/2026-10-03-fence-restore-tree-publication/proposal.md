## Why

After capturing an extraction root, directory and symbolic-leaf restore still resolve mutation paths independently. Retargeting that root during compressed input creates entries in a foreign directory.

## What Changes

- Check named extraction-root generation before directory/symbolic namespace mutation.
- Prepare directory entries and symbolic-leaf parents beneath captured root descriptors.
- Use captured os.Root removal/link operations for symbolic namespace restoration rather than reopened absolute paths.

## Capabilities

### Modified Capabilities

- `backup-recovery`: captured restore directory and symbolic namespace authority.

## Impact

R2 child of #502. Namespace/ancestor boundary only: recursive member-generation cleanup, atomic symbolic replacement, source archive creation, archive/manifest publication and rotation/state/journal/evidence remain separate. Preserve current overwrite and contained symbolic-target policy; no new budgets, fsync or all-output transaction.
