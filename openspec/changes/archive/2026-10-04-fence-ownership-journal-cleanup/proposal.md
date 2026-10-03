## Why

Journal cleanup removes its pathname without retaining the generation produced or loaded by the current mutation/recovery. Replacing the journal after actual state commit makes Apply delete foreign replacement metadata.

## What Changes

- Carry private parent/file identities from opened metadata reads and actual published producer inodes without changing JSON.
- Pass the observed journal to cleanup in Apply, CAS refusal and all recovery actions.
- Revalidate parent and complete journal state before shared captured RemoveLeaf; never normalize a replaced/missing journal into successful cleanup.
- Reuse shared strict metadata parent capture and verification; keep public absence and failed-cleanup injection semantics.

## Capabilities

### Modified Capabilities

- `owned-entry-reconciliation`: observed journal cleanup authority.

## Impact

R2 child of #513. Recovery staged/safety cleanup, record/prior-generation publication leases, ownership namespace/evidence and remaining backups remain open. Cleanup is not an overall mutation transaction; state/native aggregate may already have committed when journal refusal occurs.
