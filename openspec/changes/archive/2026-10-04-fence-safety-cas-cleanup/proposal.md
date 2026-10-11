## Why

CAS refusal cleanup removes the safety backup and sidecar by public path rather than the actual producer generation. Replacing a real produced payload after staging makes Apply delete the replacement.

## What Changes

- Return private original parent/payload/published-sidecar identities from the actual safety writer and carry them in the in-memory journal without JSON changes.
- Validate both attempt members before captured generation-checked cleanup; preserve replacements and paired evidence while returning the original CAS refusal.
- Empty-directory cleanup uses captured original object and kernel empty-directory-only removal.

## Capabilities

### Modified Capabilities

- `owned-entry-reconciliation`: safety attempt cleanup authority.

## Impact

R2 child of #520. Existing recovery retains safety evidence and does not reconstruct private CAS cleanup authority. Original record/action leases, locks, remaining backup/restore and final audit/gates/comments stay open. No overall cleanup transaction, new budget or writer exclusion.
