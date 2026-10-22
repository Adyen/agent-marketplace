## MODIFIED Requirements

### Requirement: Toggle hints follow row eligibility

The detail pane's `space = toggle` hint and the list footer's `space toggle` hint SHALL be offered for a selected resource row exactly when that row's own guards allow a toggle, including an available row and an installable Marketplace discovery row, whose space opens the toggle modal from the list. A row refused for itself, such as a required, malformed, pending-verification, uninstallable Marketplace or collection-only Marketplace row, SHALL offer only its full-view hint in the detail pane, and the footer SHALL drop the toggle hint while it is selected. Transient states, such as an operation in progress or an invalid configuration, SHALL NOT remove the hints. A selected section header SHALL keep the footer toggle hint, because space toggles the section.

#### Scenario: Toggle hints follow row eligibility

- **WHEN** the selected resource row is required, collection-only, pending verification or an uninstallable Marketplace discovery row
- **THEN** the detail pane MUST NOT offer `space = toggle` and MUST still offer `enter = full view`, and the footer MUST NOT offer `space toggle`
- **AND** an eligible enabled row, an available row, an installable Marketplace discovery row and a selected section header MUST keep the toggle hints
