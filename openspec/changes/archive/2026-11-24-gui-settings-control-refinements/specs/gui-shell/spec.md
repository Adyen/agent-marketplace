## ADDED Requirements

### Requirement: Add source keyboard submission and cancellation

The Add source dialog SHALL allow Enter in a text input to submit a valid form. Enter with invalid input SHALL NOT submit. The Cancel button SHALL close the dialog without submitting and SHALL be unavailable while an add is pending.

#### Scenario: Cancel and Enter in Add source

- **WHEN** the user selects Cancel, or presses Enter in a text input while the form is invalid
- **THEN** no add request MUST be sent
- **AND** Cancel MUST close the dialog
- **WHEN** the user presses Enter in a text input with a valid form
- **THEN** the add request MUST be sent
