## Why

Repository hook files are shared, but ownership state is private to each
installation and checkout. Removing a selection can currently report success
while an unmanaged native hook remains active. Deleting it based on matching
content would violate the fail-closed ownership contract.

## What Changes

- Refuse hook deselection through `remove` when a matching active native entry
  would remain without clean ownership proof.
- Identify the affected harness target, resource and file, explain that the hook
  remains active, and preserve configuration, acquisition locks and hook files.
- Retain successful same-owner removal and selections whose native entry is
  still needed by another selected hook.
- Exercise independent checkouts across the seven native-aggregate targets, handwritten
  matches, absent/corrupt ownership state and healthy owned removal.
- Keep file-based hook targets, such as OpenCode, on their existing ownership
  and removal path rather than treating a missing aggregate layout as residue.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `hook-resources`: explicit refusal of removal that would leave an unowned
  native hook active.

## Impact

The removal pipeline gains a read-only preflight before configuration
publication. Native hook inspection reuses source projection and ownership
classification. No portable ownership proof, adoption, state-format change or
automatic deletion of unmanaged entries is introduced.
