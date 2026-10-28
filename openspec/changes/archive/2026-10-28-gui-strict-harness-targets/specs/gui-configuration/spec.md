## REMOVED Requirements

### Requirement: User-personal Harness target form

**Reason**: Its unknown-identifier preservation scenario contradicts ADR 0010; replaced by "User-personal supported Harness target form".

**Migration**: None. Unknown Harness target IDs are configuration errors.

## ADDED Requirements

### Requirement: User-personal supported Harness target form

The Harness target form SHALL edit only the user-personal target Harness set using the `session-interactions` selection semantics. Organization Harness targets SHALL be shown read-only.

#### Scenario: Save user Harness targets

- **WHEN** a user changes and explicitly saves a non-empty selection of supported Harness target IDs
- **THEN** only the user-personal target set SHALL be updated
- **AND** the Consumer organization configuration SHALL remain unchanged

#### Scenario: Unknown Harness target IDs refuse GUI configuration

- **WHEN** effective consumer configuration names a Harness target ID the running client does not support, including alongside supported IDs or only unsupported IDs
- **THEN** the GUI SHALL show a configuration error naming the configuration file and SHALL refuse Harness target editing and synchronization
- **AND** the refusal SHALL leave authored configuration, acquisition locks, and installed resources unchanged
- **AND** after the user repairs the configuration outside the GUI, reload SHALL restore supported target editing and synchronization

#### Scenario: Organization Harness targets are read-only

- **WHEN** a user views Harness targets supplied by Consumer organization configuration
- **THEN** those targets SHALL be visible without any GUI action to modify them
