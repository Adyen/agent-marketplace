# Spec Delta

## MODIFIED Requirements

### Requirement: Explicit and focus-triggered refresh

The GUI SHALL provide explicit refresh through Command-R without a visible refresh control, reload after its own successful mutation, and perform an ordinary reload when the window regains focus. Ordinary reload behavior SHALL follow `session-startup` and retain the current view until final assessment. The GUI SHALL NOT use file watching.

#### Scenario: Refresh and focus reload use ordinary snapshot behavior

- **WHEN** the user presses Command-R, the window regains focus, or the GUI completes one of its own mutations
- **THEN** the session SHALL reload using the ordinary reload behavior in `session-startup`
- **AND** an accepted resource list SHALL remain visible until the final assessed snapshot is accepted

#### Scenario: External file changes require a refresh trigger

- **WHEN** configuration changes outside the GUI while it is unfocused
- **THEN** the GUI SHALL load the change on an explicit refresh or when the window regains focus
- **AND** it SHALL NOT watch files for changes

### Requirement: Accessible keyboard-operated controls

Every GUI action except refresh SHALL be available through visible controls, and every action SHALL be operable using only a keyboard. Refresh SHALL be available through Command-R without a visible control. Controls SHALL have accessible names, and user-facing text SHALL be in English.

#### Scenario: All actions are keyboard reachable and named

- **WHEN** a user operates the GUI without a pointer
- **THEN** every action SHALL be reachable and operable by keyboard
- **AND** every control SHALL expose an accessible name in English
- **AND** refresh SHALL be available through Command-R without a visible refresh control
