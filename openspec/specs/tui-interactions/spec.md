# tui-interactions Specification

## Purpose
Defines modal keyboard interactions and guarded configuration writes so selection, cancellation and scope changes cannot silently mutate user data.

## Requirements

### Requirement: Multi-harness selection
Harness editing and initial configuration SHALL support selecting multiple harnesses. Switching global and project scopes SHALL load that scope's selection without writing; Space SHALL toggle the focused selection without writing; only explicit Enter SHALL save after non-empty validation. Saving an existing configuration SHALL preserve unknown selected harness IDs.

#### Scenario: Scope and selection without write
- **WHEN** a user switches scopes with g or p, toggles a harness with Space, and cancels
- **THEN** each scope's selections remain available and all configuration bytes stay unchanged

#### Scenario: Save selected harnesses
- **WHEN** a user explicitly submits a non-empty multi-selection that includes an unknown existing ID
- **THEN** the guarded write preserves that ID and updates only the intended scope

#### Scenario: Empty selection
- **WHEN** a user attempts to save an empty harness selection
- **THEN** the form refuses the save without modifying configuration bytes

### Requirement: Explicit modal intent
Toggle and config-editor accelerators SHALL retain their documented actions; migration, sync-and-quit, and quit modals SHALL retain affirmative, negative and cancel paths. q, Esc and Ctrl+C SHALL be routed before a form may submit. Modal submission SHALL use the existing serialized guarded operation and authoritative post-write reload; no form completion alone SHALL claim a write succeeded.

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
