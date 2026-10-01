## Purpose

Defines the Marketplace web interface's persistent chrome and primary resource-detail actions.

## ADDED Requirements

### Requirement: Persistent Marketplace header
The Marketplace SHALL keep its primary header visible at the top of the viewport while users scroll, without removing the header from normal document flow.

#### Scenario: Header remains visible while scrolling
- **WHEN** a user scrolls a Marketplace page at a desktop or mobile viewport size
- **THEN** the primary header remains aligned with the top of the viewport

### Requirement: Marketplace footer identity
The Marketplace SHALL show its running version, documentation link, and Adyen attribution in the page footer.

#### Scenario: Footer identifies the running Marketplace
- **WHEN** a user views the Marketplace homepage
- **THEN** the footer shows the running version, Docs link, and Adyen attribution

### Requirement: Resource installation presentation
The Marketplace SHALL present compact revision metadata and actionable installation controls for installable resources. The revision copy action SHALL copy the full digest, and command copying SHALL exclude the decorative prompt.

#### Scenario: Installable resource exposes compact actions
- **WHEN** a user views an installable resource
- **THEN** revision identity, scan date, setup documentation, command, and copy actions are available
- **AND** copy actions use the full revision and command values

### Requirement: Compact unavailable installation notice
When installation is unavailable, the Marketplace SHALL replace setup and command controls and duplicate warnings with one concise reason-specific notice. A security-policy notice SHALL offer an action that activates Security details.

#### Scenario: Policy-blocked resource links to security details
- **WHEN** a resource cannot be installed because of the Marketplace security policy
- **THEN** one Not available notice explains the reason without setup, command, copy, or duplicate warning controls
- **AND** activating View security details selects and displays the Security tab

#### Scenario: Other unavailable reasons remain concise
- **WHEN** a resource is revoked or otherwise unavailable for installation
- **THEN** one Not available notice gives the applicable concise reason without setup, command, copy, duplicate warning controls, or a security-details action
