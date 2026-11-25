## MODIFIED Requirements

### Requirement: Add-source errors stay in the Add source section

Validation and request errors from adding a source SHALL remain inside the Add source modal, not at the top of Settings. Field errors SHALL appear directly below their inputs with danger styling and accessible error associations. Errors without a field SHALL appear once below the form. Editing a field SHALL clear its error; a successful add SHALL clear all errors.

#### Scenario: Add-source request error renders in the section

- **WHEN** a user adds a source and the request fails without identifying a field
- **THEN** the error MUST show once below the form inside the Add source modal
- **AND** the top of Settings MUST NOT show that error

#### Scenario: Add-source error clears after a successful add

- **WHEN** an add-source error is showing and the user then adds a source successfully
- **THEN** the Add source modal MUST close and no longer show the error

#### Scenario: Editing a field clears its error

- **WHEN** a source input has a field error and the user edits that input
- **THEN** that field's error and invalid-input styling MUST clear
