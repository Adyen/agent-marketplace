## MODIFIED Requirements

### Requirement: Multi-harness selection

Harness editing and initial configuration SHALL support selecting multiple Harness targets. Toggling a choice SHALL NOT write; only an explicit save SHALL write after non-empty validation. Saving an existing configuration SHALL preserve unknown selected Harness target IDs already present in that root. An explicit save SHALL NOT introduce an unsupported Harness target ID that was not present in the root. Initial configuration SHALL continue to require supported Harness targets.

#### Scenario: Toggling Harness targets does not write

- **WHEN** a user changes the selected Harness targets in an unsaved form and abandons the form
- **THEN** all configuration bytes SHALL stay unchanged

#### Scenario: Save selected harnesses

- **WHEN** a user explicitly saves a non-empty multi-selection that includes an unknown existing ID
- **THEN** the guarded write SHALL preserve that ID and update only the intended user-personal Harness target set
- **AND** the same selection through the existing TUI and GUI SHALL produce byte-identical configuration from identical starting roots

#### Scenario: Empty selection

- **WHEN** a user attempts to save an empty Harness target selection
- **THEN** the form SHALL refuse the save without modifying configuration bytes

#### Scenario: Newly introduced unsupported Harness target is refused

- **WHEN** an explicit Harness target save includes an unsupported identifier that is not already present in the selected root
- **THEN** the save SHALL be refused
- **AND** configuration bytes SHALL remain unchanged

#### Scenario: Initial configuration rejects unsupported Harness targets

- **WHEN** initial configuration is submitted with an unsupported Harness target identifier
- **THEN** initialization SHALL be refused without creating configuration or materialized Resource files
