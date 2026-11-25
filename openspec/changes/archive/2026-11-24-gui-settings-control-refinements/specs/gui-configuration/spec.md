## ADDED Requirements

### Requirement: Controls dependent on Backups and Logging

When backup creation is turned off in the Backups draft, the retention inputs SHALL be disabled. When writing a log file is turned off in the Logging draft, the other Logging inputs SHALL be disabled. Disabling dependent controls SHALL NOT clear their values.

#### Scenario: Logging can be turned off without editing dependent fields

- **WHEN** the user turns off writing a log file
- **THEN** the other logging inputs MUST be disabled
- **AND** saving with no other edits MUST send only the change to the logging enabled flag

#### Scenario: Backup limits remain visible and disabled when backups are off

- **WHEN** the user turns off backup creation
- **THEN** both retention inputs MUST be disabled and retain their values
