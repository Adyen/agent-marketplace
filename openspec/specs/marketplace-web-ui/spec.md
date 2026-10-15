# marketplace-web-ui Specification

## Purpose
Defines the Marketplace web interface's persistent chrome and primary resource-detail actions.

## Requirements

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
The Marketplace SHALL present compact revision metadata and actionable installation controls for installable resources. The header metadata row SHALL show revision identity and SHALL NOT show the scan date, which remains available in Security details. The revision copy action SHALL copy the full digest, and command copying SHALL exclude the decorative prompt.

#### Scenario: Installable resource exposes compact actions
- **WHEN** a user views an installable resource
- **THEN** revision identity, setup documentation, command, and copy actions are available, and the scan date is available in Security details rather than the header metadata row
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

### Requirement: Shared browser brand identity
The Landing, Docs, and Marketplace surfaces SHALL present the agent-marketplace logo artwork at one consistent displayed width and optical vertical alignment in their primary navigation, select a contrasting supplied logo variant for light and dark appearance, and publish the supplied favicon at each surface's configured base path. Route-dependent navigation wrappers MAY retain different widths. The logo link MUST retain the accessible product name and existing home destination.

#### Scenario: Browser surfaces present accessible adaptive branding
- **WHEN** a user visits Landing, a Docs page with a sidebar, or Marketplace in light or dark appearance
- **THEN** the primary navigation shows the contrasting agent-marketplace logo artwork at the same displayed width and optical vertical alignment on every surface, its home link is named `agent-marketplace`, and the document references the supplied favicon from that surface's configured base path

### Requirement: Resource source origin link
When a resource revision reports source provenance, the detail page header SHALL show the source repository and resource path as its first metadata item, linked to the original resource in a new tab. For GitHub, GitLab, and bitbucket.org hosts the link SHALL address the resource file at the exact occurrence commit; for other hosts it SHALL address the repository. When no source provenance is reported, the header SHALL omit the source item.

#### Scenario: Forge-hosted source links to the pinned resource file
- **WHEN** a user views a resource whose source repository is hosted on GitHub, GitLab, or bitbucket.org
- **THEN** the first header metadata item names the repository and path and opens the resource file at the occurrence commit in a new tab

#### Scenario: Other-host source links to the repository
- **WHEN** a user views a resource whose source repository is hosted elsewhere
- **THEN** the source item links to the repository URL without a `.git` suffix and still shows the resource path

#### Scenario: Resource without source provenance omits the origin
- **WHEN** a user views a resource that reports no source provenance
- **THEN** the header shows no source item
