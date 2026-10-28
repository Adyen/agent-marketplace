# session-interactions Specification

## Purpose
An interactive client is the TUI or the desktop GUI acting on the user's behalf. This capability defines surface-neutral Harness target selection, and guarded configuration writes.

## Requirements

### Requirement: Multi-harness selection

Harness editing and initial configuration SHALL support selecting multiple supported Harness target IDs. Toggling a choice SHALL NOT write; only an explicit save SHALL write after non-empty validation. The running CLI MUST understand every Harness target ID named in consumer configuration, including harness selections and per-target materialization settings. Unknown IDs SHALL remain configuration errors that gate configuration-dependent actions, not warnings or permission to use only the supported subset. Refusal SHALL NOT silently remove or rewrite unknown entries. An explicit save SHALL NOT introduce an unsupported Harness target ID that was not present in the root. Initial configuration SHALL continue to require supported Harness targets.

#### Scenario: Toggling Harness targets does not write

- **WHEN** a user changes the selected Harness targets in an unsaved form and abandons the form
- **THEN** all configuration bytes SHALL stay unchanged

#### Scenario: Save selected harnesses

- **WHEN** a user explicitly saves a non-empty multi-selection of supported Harness target IDs
- **THEN** the guarded write SHALL update only the intended user-personal Harness target set
- **AND** the same selection through the existing TUI and GUI SHALL produce byte-identical configuration from identical starting roots

#### Scenario: Unknown Harness target IDs refuse configuration

- **WHEN** otherwise valid consumer configuration names an unknown Harness target ID, including alongside supported IDs or in per-target materialization settings
- **THEN** the interactive client SHALL show a configuration error and block synchronization instead of treating the unknown entry as a warning or using only supported targets
- **AND** the refused new operation SHALL preserve authored configuration, acquisition locks, and existing installed resources without installing pending resources
- **AND** after the user explicitly repairs the configuration, reload and synchronization SHALL work with supported targets

#### Scenario: Empty selection

- **WHEN** a user attempts to save an empty Harness target selection
- **THEN** the form SHALL refuse the save without modifying configuration bytes

#### Scenario: Newly introduced unsupported Harness target is refused

- **WHEN** an explicit Harness target save includes an unsupported identifier that is not already present in the selected root
- **THEN** the save SHALL be refused
- **AND** configuration bytes SHALL remain unchanged

#### Scenario: Initial configuration rejects unsupported Harness targets

- **WHEN** initial configuration is submitted with an unsupported Harness target identifier
- **THEN** initialization SHALL be refused without creating configuration or materialized Resource files

### Requirement: Guarded mutations

Every configuration-writing confirmation SHALL use the serialized guarded operation and an authoritative post-write reload. Completing a form alone SHALL NOT claim that a write succeeded. Sync-and-quit and quit confirmations SHALL keep affirmative, negative, and cancel paths. An interactive client SHALL NOT offer a consumer migration or automatic conversion action; unsupported consumer configuration SHALL block configuration-writing confirmations.

#### Scenario: Cancel or stale completion

- **WHEN** a user cancels a confirmation or a late write completion arrives for an inactive operation
- **THEN** no unintended write SHALL occur and the active operation and diagnostics SHALL remain unchanged

#### Scenario: Guarded submission

- **WHEN** a user submits a mutation while another operation is active or prerequisites are invalid
- **THEN** the guarded operation SHALL block the new write without claiming success
- **AND** a successful write SHALL complete only after an authoritative post-write reload

#### Scenario: Confirmation retains all user choices

- **WHEN** a user responds to sync-and-quit or quit confirmation
- **THEN** affirmative, negative, and cancel paths SHALL remain available
- **AND** only the applicable affirmative guarded action SHALL perform its operation
