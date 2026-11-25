## ADDED Requirements

### Requirement: Quit confirms only unsynced or running work

Quitting the GUI from its quit control or a native window close request SHALL exit without a confirmation when there are no pending changes and no operation is running or being requested. When pending changes exist or an operation is running or being requested, the GUI SHALL show the quit confirmation, which keeps its affirmative, negative, and cancel paths.

#### Scenario: Quit with nothing pending exits immediately

- **WHEN** a user quits with zero pending changes and no running or requested operation
- **THEN** the GUI SHALL exit without showing a confirmation
- **AND** no sync SHALL be started

#### Scenario: Quit with pending or running work confirms

- **WHEN** a user quits while pending changes exist or an operation is running or being requested
- **THEN** the GUI SHALL show the quit confirmation before exiting
