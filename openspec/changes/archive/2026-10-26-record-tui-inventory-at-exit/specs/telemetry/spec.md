## ADDED Requirements

### Requirement: Interactive sessions record one final inventory

An interactive TUI session SHALL record effective-configuration inventory at most once, when the session ends, using the latest accepted complete snapshot of that session. Snapshots superseded during the session, including those reloaded after toggles, edits, sync, or update, SHALL NOT be recorded, and operations the session runs SHALL NOT record their own inventory. When the latest accepted snapshot has hard resolution failures, the session SHALL record no inventory. A session terminated without running its exit path MAY record nothing; a later command records the current configuration.

#### Scenario: Interactive edits record only the final configuration

- **WHEN** a detailed-telemetry TUI session accepts several successive snapshots with different configurations
- **THEN** no inventory MUST be recorded before the session ends and exactly one snapshot, for the last accepted configuration, MUST be recorded when it ends

#### Scenario: Session operations do not record inventory

- **WHEN** an operation started by the TUI session resolves configuration
- **THEN** that operation MUST NOT record inventory

#### Scenario: Failed final snapshot records nothing

- **WHEN** the last accepted snapshot of a TUI session has hard resolution failures
- **THEN** the session end MUST record no inventory
