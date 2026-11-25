## MODIFIED Requirements

### Requirement: Uninstall resource action

The GUI SHALL name the action that deselects an installed resource Uninstall: inspector control and dialog title "Uninstall resource", confirm control "Confirm uninstall", dialog text beginning "Uninstall <displayName>", and progress "Uninstalling <displayName>…". Every inspector resource action, including Exclude and Open resource, SHALL be a visibly labeled text button, never only a trash or delete icon.

#### Scenario: Installed resource offers a labeled Uninstall

- **WHEN** the user selects an installed resource that the user can deselect and activates its inspector action
- **THEN** the inspector MUST show a text button labeled Uninstall with the accessible name "Uninstall resource" and no trash icon
- **AND** the dialog MUST be titled "Uninstall resource", its text MUST begin "Uninstall <displayName>", and its confirm control MUST be "Confirm uninstall"
