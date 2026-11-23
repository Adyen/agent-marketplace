## ADDED Requirements

### Requirement: Status bar keyboard hints appear on keyboard use

The status bar SHALL NOT show its keyboard hints until the user presses an arrow key, Enter, or Tab in the session. Once shown, the hints SHALL stay visible for the rest of the session. Pointer activity alone SHALL NOT show the hints.

#### Scenario: Keyboard hints appear after first keyboard navigation

- **WHEN** the user has only used the pointer in the session
- **THEN** the status bar MUST NOT show the keyboard hints
- **WHEN** the user then presses an arrow key, Enter, or Tab
- **THEN** the status bar MUST show the keyboard hints and keep them visible for the rest of the session
