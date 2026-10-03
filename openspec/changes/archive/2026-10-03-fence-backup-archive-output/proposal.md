## Why

Exclusive archive creation still writes/removes through independent paths. Retargeting its parent after creation causes source-refusal cleanup to delete a foreign replacement archive; a successful write can also be incorrectly reported under a replaced parent.

## What Changes

- Share descriptor-relative exclusive0600 archive output handling between ordinary and Codex staged archive generation.
- Validate completed writer and named archive identity/state plus admitted parent before reporting successful generation.
- On refusal remove only the attempt-created inode below the captured parent and propagate close/cleanup errors.

## Capabilities

### Modified Capabilities

- `backup-recovery`: captured archive output and refusal cleanup.

## Impact

R2 child of #505. Preserve existing initial parent/root alias admission, exclusivity/collision behavior, private creation, format and close-only durability contract. Backup namespace directory preparation, Codex staged-to-final/manifest publication, retention and ownership state/journal/evidence remain next. No transaction/fsync/new budgets or writer-exclusion claim.
