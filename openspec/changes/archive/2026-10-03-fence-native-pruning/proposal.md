## Why

Native symlink-mode skill and artifact stale pruning reopens paths after managed-link classification. Actual native Factory skill replacement is deleted during stale-link prune despite not being the classified managed link.

## What Changes

- Reuse captured removal authority in native skill-group and regular-artifact stale-link consumers.
- Recheck full classified leaf state, managed link target text and named parent before relative removal; preserve dry-run and user-owned link behavior.
- Route artifact empty-parent cleanup through captured empty-directory-only cleanup.

## Capabilities

### Modified Capabilities

- `materialization`: native managed-link prune authority.

## Impact

Dependent R2 child of #489. Marker-bearing regular-file native command publication/prune and metadata discovery remain separate next problems, as do aggregate/restore/ownership and final closure/comment pass.
