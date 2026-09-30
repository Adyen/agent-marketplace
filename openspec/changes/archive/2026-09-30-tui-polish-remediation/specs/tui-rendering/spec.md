## ADDED Requirements

### Requirement: Consistent modal frame
Every modal SHALL render in one frame whose width is bounded by the terminal, with a complete border, a title, one focus indicator per option, each accelerator shown once, and a single hint row. Paths that do not fit SHALL be shortened in the middle so their file name stays visible.

#### Scenario: Modal at common sizes
- **WHEN** any modal opens at 50×12, 80×24 or 120×34 with long configuration paths
- **THEN** every modal line fits the terminal, the border is complete, and the file names of shortened paths remain visible

### Requirement: Editor handoff screen
While an external config editor runs, the TUI SHALL show a screen on the alternate screen that names the file being edited, rather than the terminal contents from before the TUI started. After the editor exits the TUI SHALL leave that screen and resume.

#### Scenario: Editor running
- **WHEN** a user opens a config layer in an external editor
- **THEN** the terminal shows the waiting screen naming the file until the editor exits, and the pre-launch screen is not shown
