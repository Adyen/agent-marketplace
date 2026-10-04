## Why

Regular restore refuses multiply linked destination files, but symbolic restore bypasses that admission and recursively removes the leaf before installing the archive's link.

## What Changes

- Admit existing structural destination shape beneath the captured parent before symbolic restore removal.
- Reuse the opened regular/single-link boundary and compare it with classified metadata for regular leaves; reject special leaves.
- Preserve existing directory/symbolic/absent destination behavior and exact archived target text.

## Capabilities

### Modified Capabilities

- `backup-recovery`: symbolic restore destination admission.

## Impact

R2 child of #526. Actual Create/Restore regression and genuine restore control. Initial archive output/parent aliases remain explicitly supported by existing normative contract; no blanket ban or new budget. Later removal-generation leases/tree transaction and remaining audit stay separate.
