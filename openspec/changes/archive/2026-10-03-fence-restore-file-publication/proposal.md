## Why

Ordinary regular-file restore uses os.Create after path checks and truncates foreign hardlink bytes. Mapped native restore stages and publishes via independent paths, allowing replacement destination generations to be overwritten.

## What Changes

- Capture extraction-root authority and prepare regular-file parents descriptor-relatively.
- Share captured target/staged-file publication between ordinary regular restore and explicit native mappings, with inode-bound staging cleanup and original archive-mode semantics.
- Revalidate opened/named destination, parent and staged generation before relative rename without adding per-file fsync or a durability policy.

## Capabilities

### Modified Capabilities

- `backup-recovery`: regular restore publication authority.

## Impact

R2 child of #501. Directory/symlink restore, source archive creation, manifests/rotation and ownership state/journal/evidence consumers remain next. Not all-output transactional extraction; no arbitrary expanded-work budgets or writer-exclusion claim.
