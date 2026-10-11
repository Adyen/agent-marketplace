## Why

Backup source discovery and symbolic-link metadata reads still reopen paths independently. An ordinary public Create with an intermediate directory alias imports foreign symbolic metadata without exercising the protected regular payload reader.

## What Changes

- Select existing ordinary/Codex paths through strict captured source metadata readers.
- Traverse selected trees via captured os.Root filesystem and read each entry metadata beneath strict parents.
- Read symbolic link text descriptor-relatively with before/after identity-state validation; do not follow its payload target.
- Normalize genuine optional absence before descriptor cleanup, while disappearing admitted traversal entries refuse.

## Capabilities

### Modified Capabilities

- `backup-recovery`: backup discovery/link metadata authority.

## Impact

R2 child of #504. Retain final symbolic entries, relative target text, optional absent paths and mapped-only missing-root semantics, with no arbitrary new budgets. Archive/manifest output publication and retention, symbolic member generations and ownership state/journal/evidence remain next.
