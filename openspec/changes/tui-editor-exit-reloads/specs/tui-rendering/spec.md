## MODIFIED Requirements

### Requirement: Editor handoff screen
While an external config editor runs, the TUI SHALL show a screen on the alternate screen that names the file being edited, rather than the terminal contents from before the TUI started. After the editor exits the TUI SHALL leave that screen and resume. When the editor exits successfully the TUI SHALL reload the configuration without further input; when it fails the TUI SHALL report the failure with editor recovery guidance. Either way, later configuration writes in the same session SHALL be accepted.

#### Scenario: Editor running
- **WHEN** a user opens a config layer in an external editor
- **THEN** the terminal shows the waiting screen naming the file until the editor exits, and the pre-launch screen is not shown

#### Scenario: Editor exit reloads and frees writes
- **WHEN** the external editor saves a configuration change and exits, or exits with a failure
- **THEN** a successful exit MUST show the edited configuration without the user pressing reload, a failed exit MUST be reported as an error with editor recovery guidance and leave the file unchanged
- **AND** a configuration write the user makes afterwards MUST be applied rather than refused
