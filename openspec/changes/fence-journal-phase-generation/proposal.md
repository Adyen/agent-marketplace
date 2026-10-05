## Why

Apply retains a prepared journal identity for cleanup but phase publication freshly captures the public destination. Identical JSON at a replacement inode after native rename can be overwritten before state commits and Apply reports success.

## What Changes

- Pass the journal's existing private observation into the shared metadata writer.
- Validate original full file state/parent object before namespace preparation and again within existing staged publication validation.
- Keep initial journal creation, ordinary metadata writes and all journal phase/disarm callers on the existing publisher.

## Capabilities

### Modified Capabilities

- `owned-entry-reconciliation`: original journal phase-publication authority.

## Impact

R2 child of #530. No new public interface, JSON/source-comment/dependency/budget change or whole native/metadata transaction/uncooperative-writer exclusion. Native aggregate already published at this boundary stays published; original prepared recovery evidence remains useful. Final admitted-case audit and queued comments remain open.
