## ADDED Requirements

### Requirement: Repair uses verified acquisition authority

`doctor --repair` SHALL compute Git-backed desired entries from valid acquisition locks and verified exact snapshots. Missing or invalid Git acquisition evidence SHALL cause nonzero exit without reconciling that evidence or changing shared aggregate entries.

#### Scenario: Missing or invalid Git acquisition evidence refuses repair

- **WHEN** repair is requested for a Git-backed resource whose acquisition lock is missing or invalid
- **THEN** repair SHALL exit nonzero and leave the acquisition lock and shared aggregate unchanged

#### Scenario: Moving a Git ref does not change repair values

- **WHEN** a registry ref moves after its acquisition lock was created and a conflicting entry is confirmed for replacement
- **THEN** repair SHALL install the exact snapshot's value and leave the acquisition lock byte-identical

### Requirement: Repair refuses incomplete organization selection

When organization resource acquisition fails, repair SHALL report that failure and refuse mutation rather than interpret unavailable resources as no longer desired.

#### Scenario: Organization acquisition failure refuses repair

- **WHEN** the organization Marketplace cannot resolve its configured resources and no organization acquisition lock is deployed
- **THEN** repair SHALL report the acquisition failure, exit nonzero, and leave shared aggregate entries unchanged

### Requirement: Repair publication uses unchanged policy

Repair SHALL validate the configuration and acquisition-lock inputs authorizing its preview after confirmation and at the aggregate publication boundary. A changed input SHALL cause a generation conflict, preserve the aggregate and ownership state, discard uncommitted staging, and stop repair before prompting another target.

#### Scenario: Configuration changes during confirmation

- **WHEN** configuration changes while repair waits for confirmation
- **THEN** repair SHALL refuse the action with nonzero exit and preserve the aggregate

#### Scenario: Organization acquisition lock appears during confirmation

- **WHEN** an organization acquisition lock is deployed while repair waits for confirmation
- **THEN** repair SHALL refuse the action and preserve both the aggregate and the deployed lock

#### Scenario: Policy changes between confirmation validation and mutation

- **WHEN** policy changes after confirmation was validated but before its repair mutation begins
- **THEN** repair SHALL report a generation conflict without changing the aggregate

#### Scenario: Policy changes after repair staging

- **WHEN** policy changes after adoption, replacement, or orphan removal is staged but before publication
- **THEN** repair SHALL refuse publication, preserve aggregate and ownership bytes, and remove that attempt's staged file, journal, and backup

#### Scenario: Repair stops at the first policy conflict

- **WHEN** policy changes before a confirmed action in a multi-target repair
- **THEN** repair SHALL refuse that action and SHALL NOT prompt for subsequent targets

### Requirement: Repair confirmation does not hold mutation locks

Repair SHALL hold no shared-aggregate ownership lock while waiting for a user's confirmation.

#### Scenario: Ownership writers remain unblocked at confirmation prompts

- **WHEN** repair waits for confirmation before or after a completed action
- **THEN** another writer SHALL be able to acquire the aggregate's ownership lock
