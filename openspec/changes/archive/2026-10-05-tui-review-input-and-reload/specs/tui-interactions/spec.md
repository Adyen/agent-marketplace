## ADDED Requirements

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
