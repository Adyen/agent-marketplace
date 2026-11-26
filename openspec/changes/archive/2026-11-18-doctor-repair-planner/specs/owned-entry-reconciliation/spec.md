## MODIFIED Requirements

### Requirement: Repair uses verified acquisition authority

`doctor --repair` SHALL compute Git-backed desired entries from valid acquisition locks and verified exact snapshots. Missing or stale writable-scope acquisition locks SHALL be offered as confirmed safe lock steps without pin upgrades. Shared-aggregate reconciliation SHALL remain blocked until those steps succeed and the resulting acquisition authority is verified. Preview and declined runs SHALL exit nonzero and leave missing locks absent and existing locks and shared aggregates unchanged. Invalid acquisition evidence SHALL remain a refusal unless its specific diagnostic has an explicitly supported repair.

#### Scenario: Missing or invalid Git acquisition evidence refuses repair

- **WHEN** repair previews or declines repair for a Git-backed resource whose acquisition lock is missing or invalid
- **THEN** repair SHALL exit nonzero and leave the acquisition lock and shared aggregate unchanged

#### Scenario: Confirmed missing or stale writable lock precedes aggregate repair

- **WHEN** a safe repair of a missing or stale writable acquisition lock is confirmed
- **THEN** repair SHALL reconcile the lock without pin upgrades and verify its exact snapshots before permitting aggregate reconciliation
- **AND** failure of that step SHALL leave shared aggregate entries unchanged

#### Scenario: Moving a Git ref does not change repair values

- **WHEN** a registry ref moves after its acquisition lock was created and a conflicting entry is confirmed for replacement
- **THEN** repair SHALL install the exact snapshot's value and leave the acquisition lock byte-identical

### Requirement: Repair refuses incomplete organization selection

When organization resource acquisition fails, repair SHALL report that failure and block every selection-dependent repair step rather than interpret unavailable resources as no longer desired. Repair steps that do not depend on the selection SHALL remain runnable.

#### Scenario: Organization acquisition failure refuses repair

- **WHEN** the organization Marketplace cannot resolve its configured resources and no organization acquisition lock is deployed
- **THEN** repair SHALL report the acquisition failure, list every selection-dependent step as blocked by it, exit nonzero, and leave shared aggregate entries unchanged
