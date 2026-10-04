## Why

Apply's CAS checks only the ownership JSON generation number. Replacing an originally loaded record with identical JSON after staging still authorizes native publication and state overwrite.

## What Changes

- Return original opened record/parent identity privately alongside loading, without changing public Record/JSON or absence diagnostics.
- Revalidate original file generation/parent/absence after the CAS policy callback and before native rename.
- Use the shared metadata publisher's final validation callback to keep original record authority through state staging/publication.

## Capabilities

### Modified Capabilities

- `owned-entry-reconciliation`: original state generation authority during Apply.

## Impact

R2 child of #521. Late state refusal can occur after native publication and keeps durable recovery evidence. Restart recovery/action leases, locks and remaining backup/restore audit remain separate. No persisted inode promise across restarts, new budget or overall transaction/exclusion.
