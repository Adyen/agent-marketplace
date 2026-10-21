## ADDED Requirements

### Requirement: Retargeted resources report pending sync

After saved target harnesses change, a resource that is still selected SHALL
appear as enabled and pending sync until a sync completes, whether it is
missing from a new target, has a stale copy in a harness it no longer targets,
or both. A broken output SHALL still take precedence and show the resource as
broken.

#### Scenario: Retargeted resource is pending sync

- **WHEN** a user saves project target harnesses that replace the harness a
  selected resource is installed in
- **THEN** before syncing, that resource MUST show as enabled and pending sync
  for its new target, not as orphaned or broken
- **AND** syncing MUST install it into the new target and remove the copy from
  the former one

#### Scenario: Removed target harness is pending cleanup

- **WHEN** a selected resource is in sync in its remaining target harnesses
  and only a copy in a harness that is no longer targeted is left
- **THEN** before syncing, that resource MUST show as enabled and pending sync,
  not as orphaned or broken
