# tui-interactions Specification

## Purpose
Defines modal keyboard interactions and guarded configuration writes so selection, cancellation and scope changes cannot silently mutate user data.

## Requirements

### Requirement: Explicit modal intent

q, Esc, and Ctrl+C SHALL be routed before a form may submit. Toggle and config-editor accelerators SHALL retain their documented TUI actions. Modal submission SHALL use the `session-interactions` guarded mutation contract.

#### Scenario: Cancel or stale completion

- **WHEN** a user cancels a modal or a late write completion arrives for an inactive operation
- **THEN** no unintended write occurs and the active operation and diagnostics remain unchanged

#### Scenario: Guarded submission

- **WHEN** a user submits a mutation while another operation is active or prerequisites are invalid
- **THEN** the same guard used by the keyboard path blocks the new write without claiming success

### Requirement: Fixed-choice modal input ignores search filtering

The toggle and config-editor choice dialogs SHALL keep their fixed options when the user presses `/`; the key SHALL NOT enter text filtering, change the selected choice, or cause a configuration write. Arrow navigation and Escape cancellation SHALL remain available afterward.

#### Scenario: Slash leaves fixed-choice dialogs unchanged

- **WHEN** a user presses `/` in the toggle or config-editor choice dialog
- **THEN** the dialog SHALL retain its current choice without filtering or writing, and the user SHALL still be able to navigate or cancel it

### Requirement: Harness choices return after resizing

The initial harness-selection and target-harness dialogs SHALL restore the complete option list when the terminal expands after a compact layout. Resizing SHALL preserve the cursor and selected harness IDs, including an unknown selected ID already present in configuration.

#### Scenario: Harness options return after compact resize

- **WHEN** a user opens either harness dialog at a supported full size, selects an unknown existing harness ID, shrinks the terminal to a compact size, then expands it to the original size
- **THEN** all harness choices, including the selected unknown ID, SHALL be visible with the same cursor and selected IDs

### Requirement: Multi-harness key bindings

The TUI SHALL use g and p to select the edited scope without writing, Space to toggle the focused Harness target without writing, and Enter to submit the focused multi-selection through the guarded save. Surface-neutral selection and validation rules follow `session-interactions`.

#### Scenario: Scope and selection without write

- **WHEN** a user switches scopes with g or p, toggles a Harness target with Space, and cancels
- **THEN** each scope's selections remain available and all configuration bytes stay unchanged

#### Scenario: Enter saves selected Harness targets

- **WHEN** a user has a non-empty Harness target selection in the focused form and presses Enter
- **THEN** the TUI SHALL submit that selection through the guarded write

### Requirement: Legacy user configuration migration offer excludes non-user roots

The TUI SHALL offer startup migration only for the legacy user-personal root and SHALL never migrate a repository or personal-local root through that offer.

#### Scenario: Accepting the offer migrates only the user-personal root

- **WHEN** startup finds legacy user-personal, repository, and personal-local roots and the user accepts migration
- **THEN** only the user-personal root SHALL be migrated to v4
- **AND** the repository and personal-local roots SHALL remain unchanged

#### Scenario: Declining the offer continues through the legacy reader

- **WHEN** startup finds legacy user-personal, repository, and personal-local roots and the user declines migration
- **THEN** the TUI SHALL continue through the legacy reader
- **AND** no user-personal, repository, or personal-local configuration SHALL be written
