## ADDED Requirements

### Requirement: Sync and update feedback starts at the click

When the user starts Sync or Update, the GUI SHALL show the action's progress toast and a busy indicator on the pressed toolbar button from the moment the request is sent until the operation settles or fails to start, including before the session reports the running operation.

#### Scenario: Update shows feedback before the operation is reported

- **WHEN** the user presses Update and the action request has not yet returned and no snapshot reports the operation
- **THEN** the progress toast MUST read "Updating…" and the Update button MUST show a busy indicator
- **AND** both MUST disappear when the operation settles

#### Scenario: Failed start clears the feedback

- **WHEN** the user presses Sync and the action request fails or is refused as busy
- **THEN** the progress toast and the Sync button's busy indicator MUST disappear
