## ADDED Requirements

### Requirement: Add-source errors stay in the Add source section

Validation and request errors from adding a source in Settings SHALL render inside the Add source section and SHALL NOT render at the top of Settings. They SHALL clear after the next successful add.

#### Scenario: Add-source request error renders in the section

- **WHEN** a user adds a source and the request to add it fails
- **THEN** the error MUST show inside the Add source section
- **AND** the top of Settings MUST NOT show that error

#### Scenario: Add-source error clears after a successful add

- **WHEN** an add-source error is showing and the user then adds a source successfully
- **THEN** the Add source section MUST no longer show the error
