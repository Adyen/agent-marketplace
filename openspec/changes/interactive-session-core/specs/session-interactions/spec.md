## Purpose

An interactive client is the TUI or the desktop GUI acting on the user's behalf. This capability defines surface-neutral Harness target selection, guarded configuration writes, and the legacy user configuration migration offer.

## ADDED Requirements

### Requirement: Multi-harness selection

Harness editing and initial configuration SHALL support selecting multiple Harness targets. Toggling a choice SHALL NOT write; only an explicit save SHALL write after non-empty validation. Saving an existing configuration SHALL preserve unknown selected Harness target IDs.

#### Scenario: Toggling Harness targets does not write

- **WHEN** a user changes the selected Harness targets in an unsaved form and abandons the form
- **THEN** all configuration bytes SHALL stay unchanged

#### Scenario: Save selected harnesses

- **WHEN** a user explicitly saves a non-empty multi-selection that includes an unknown existing ID
- **THEN** the guarded write SHALL preserve that ID and update only the intended user-personal Harness target set

#### Scenario: Empty selection

- **WHEN** a user attempts to save an empty Harness target selection
- **THEN** the form SHALL refuse the save without modifying configuration bytes

### Requirement: Guarded mutations

Every configuration-writing confirmation SHALL use the serialized guarded operation and an authoritative post-write reload. Completing a form alone SHALL NOT claim that a write succeeded. Migration, sync-and-quit, and quit confirmations SHALL keep affirmative, negative, and cancel paths.

#### Scenario: Cancel or stale completion

- **WHEN** a user cancels a confirmation or a late write completion arrives for an inactive operation
- **THEN** no unintended write SHALL occur and the active operation and diagnostics SHALL remain unchanged

#### Scenario: Guarded submission

- **WHEN** a user submits a mutation while another operation is active or prerequisites are invalid
- **THEN** the guarded operation SHALL block the new write without claiming success
- **AND** a successful write SHALL complete only after an authoritative post-write reload

#### Scenario: Confirmation retains all user choices

- **WHEN** a user responds to migration, sync-and-quit, or quit confirmation
- **THEN** affirmative, negative, and cancel paths SHALL remain available
- **AND** only the applicable affirmative guarded action SHALL perform its operation

### Requirement: Legacy user configuration migration offer

At startup, when the user-personal root uses a legacy schema, an interactive client SHALL offer to migrate that root to v4. Accepting SHALL migrate the user-personal root; declining SHALL continue through the legacy reader without writing.

#### Scenario: Legacy user configuration is offered for migration

- **WHEN** startup finds a legacy user-personal root
- **THEN** the interactive client SHALL offer to migrate that root to v4

#### Scenario: Accepting the offer migrates the user-personal root

- **WHEN** the user accepts migration of the legacy user-personal root
- **THEN** only that user-personal root SHALL be migrated to v4

#### Scenario: Declining the offer continues through the legacy reader

- **WHEN** startup finds a legacy user-personal root and the user declines migration
- **THEN** the client SHALL continue through the legacy reader
- **AND** the user-personal configuration SHALL remain unchanged
