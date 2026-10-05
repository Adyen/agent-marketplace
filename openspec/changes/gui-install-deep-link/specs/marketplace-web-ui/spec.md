## MODIFIED Requirements

### Requirement: Resource installation presentation

The Marketplace SHALL present compact revision metadata and actionable installation controls for installable resources. Installable resources SHALL show an “Open in app” action next to the command, regardless of whether the GUI is detected as installed. The revision copy action SHALL copy the full digest, and command copying SHALL exclude the decorative prompt.

#### Scenario: Installable resource exposes compact actions

- **WHEN** a user views an installable resource with a displayed Marketplace endpoint URL, Resource coordinate, and Resource revision digest
- **THEN** revision identity, scan date, setup documentation, command, and copy actions are available
- **AND** an “Open in app” action is shown next to the command and targets an install link in the `gui-install-deep-link` format carrying that Marketplace endpoint URL, Resource coordinate, and displayed Resource revision digest
- **AND** copy actions use the full revision and command values

### Requirement: Compact unavailable installation notice

When installation is unavailable, the Marketplace SHALL replace setup and command controls, the “Open in app” action, and duplicate warnings with one concise reason-specific notice. A security-policy notice SHALL offer an action that activates Security details.

#### Scenario: Policy-blocked resource links to security details

- **WHEN** a resource cannot be installed because of the Marketplace security policy
- **THEN** one Not available notice explains the reason without setup, command, copy, “Open in app”, or duplicate warning controls
- **AND** activating View security details selects and displays the Security tab

#### Scenario: Other unavailable reasons remain concise

- **WHEN** a resource is revoked or otherwise unavailable for installation
- **THEN** one Not available notice gives the applicable concise reason without setup, command, copy, “Open in app”, duplicate warning controls, or a security-details action
