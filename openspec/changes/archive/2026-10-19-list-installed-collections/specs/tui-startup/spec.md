## ADDED Requirements

### Requirement: Installed collections render as installed rows

The provisional and verified startup stages SHALL render a selected collection, whether a Git or local collection or a direct Marketplace collection pin, as an installed row with its scope badges. They SHALL NOT render it as available. Disabling an installed collection SHALL remove its collection selection through the existing config write path.

#### Scenario: verified-selected-collection-row-is-installed

- **WHEN** verified inventory contains a selected collection
- **THEN** its row MUST be enabled with the scope of the selecting layer
- **AND** the row MUST NOT appear again as an available row

#### Scenario: provisional-selected-collection-row-is-installed

- **WHEN** the validated configuration selects a collection
- **THEN** the provisional stage MUST render that collection as a verification-pending installed row
- **AND** a non-direct nested collection pin MUST NOT render a row

#### Scenario: disabling-installed-collection-removes-selection

- **WHEN** the user disables an installed collection row in its selecting scope
- **THEN** the collection selection MUST be removed from that scope's configuration
- **AND** other selections in that configuration MUST be preserved
