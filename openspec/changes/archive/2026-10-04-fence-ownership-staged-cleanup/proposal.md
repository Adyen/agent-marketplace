## Why

Recovery trusts a journal's arbitrary StagedPath and removes it by path. A valid prepared-crash journal can nominate an inert outside-root file and cause public Recover to delete it; changed staged bytes are also removed without checking the recorded desired digest.

## What Changes

- Require absolute stage paths in the same aggregate parent with the exact generation-specific aggregate staged prefix.
- Keep aggregate containment and captured-parent authority through nonblocking regular single-link staged payload validation/removal.
- Require opened staged bytes to match the recorded desired digest, then revalidate the named parent and remove descriptor-relatively.
- Preserve valid genuine stage absence, collision-disarmed empty paths and actual crash rollback/roll-forward behavior.

## Capabilities

### Modified Capabilities

- `owned-entry-reconciliation`: staged cleanup authority in recovery/CAS.

## Impact

R2 child of #515. Prior record leases, recovery safety cleanup, safety evidence/namespace and remaining backups/case audit/review comments remain open. No new byte/work budget, journal format or transaction/writer-exclusion promise.
