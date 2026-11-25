## ADDED Requirements

### Requirement: Native directory source location picker

The Add source form in the native desktop app SHALL offer a keyboard-operable Browse control beside Location for directory registries only. Choosing a folder SHALL populate Location with its absolute path and use the same validation as typed input. Browse SHALL NOT submit the form or write configuration.

#### Scenario: Choosing a folder fills Location without adding a source
- **WHEN** a user activates Browse for folder and selects a directory
- **THEN** Location SHALL contain the selected absolute path and follow the normal Location validation and error-clearing behavior
- **AND** no source SHALL be added until the user activates Add source

#### Scenario: The dialog starts at an existing Location or home
- **WHEN** Browse opens the native folder dialog
- **THEN** it SHALL start at the current Location if it is an existing directory, or at the user's home directory otherwise

#### Scenario: Cancelling the dialog preserves Location
- **WHEN** the user dismisses the folder dialog without choosing a directory
- **THEN** Location SHALL remain unchanged and no source SHALL be added

#### Scenario: Browse is absent outside native directory source forms
- **WHEN** the selected kind is Git or Marketplace, or the form does not have a native directory picker binding
- **THEN** Browse SHALL be absent and typed Location entry SHALL remain available
