## MODIFIED Requirements

### Requirement: Recoverable source addition failure

Adding a directory registry source whose path does not exist SHALL show an inline Location error inside the Add source section, SHALL NOT write configuration, and SHALL leave the workspace usable. The Location input SHALL have a danger border, `aria-invalid="true"`, and `aria-describedby` referencing the error.

#### Scenario: Nonexistent directory source is refused in the form

- **WHEN** a user adds a directory registry source whose path does not exist
- **THEN** an error MUST show directly below Location with a danger border and accessible error association on that input
- **AND** the user-personal root MUST remain unchanged
- **AND** resource rows, the toolbar, and Settings MUST still respond

### Requirement: Add-source errors stay in the Add source section

Validation and request errors from adding a source in Settings SHALL remain inside the Add source section, not at the top of Settings. Field errors SHALL appear directly below their inputs with danger styling and accessible error associations. Errors without a field SHALL appear once below the form. Editing a field SHALL clear its error; a successful add SHALL clear all errors.

#### Scenario: Add-source request error renders in the section

- **WHEN** a user adds a source and the request fails without identifying a field
- **THEN** the error MUST show once below the form inside the Add source section
- **AND** the top of Settings MUST NOT show that error

#### Scenario: Add-source error clears after a successful add

- **WHEN** an add-source error is showing and the user then adds a source successfully
- **THEN** the Add source section MUST no longer show the error

#### Scenario: Editing a field clears its error

- **WHEN** a source input has a field error and the user edits that input
- **THEN** that field's error and invalid-input styling MUST clear

## ADDED Requirements

### Requirement: Source form input validity

The shared source form SHALL disable Add source until a directory location is non-blank, a Git location is accepted by the existing GUI add-source API/config location admission rules with a non-blank ref, or a Marketplace location is an absolute HTTP(S) URL. Client checks SHALL NOT reject locations accepted by that admission boundary, and SHALL NOT loosen existing server safety rules. Directory existence SHALL remain a server check. Field hints SHALL appear only after blur or an attempted submit.

#### Scenario: Invalid source input disables addition

- **WHEN** a directory location is blank or whitespace, a Git location is rejected by the existing GUI add-source API/config location admission rules or its ref is blank, or a Marketplace location is not an absolute HTTP(S) URL
- **THEN** Add source MUST remain disabled
- **AND** an untouched field MUST NOT show a validation hint while its first characters are typed
- **AND** an invalid field MUST show its hint after blur or an attempted submit

#### Scenario: Valid source input enables addition

- **WHEN** the user supplies valid input for a source kind and the form is otherwise editable
- **THEN** Add source MUST be enabled
- **AND** the client MUST accept Git locations accepted by the existing GUI add-source API/config location admission rules
- **AND** directory existence MUST NOT be checked by the client
