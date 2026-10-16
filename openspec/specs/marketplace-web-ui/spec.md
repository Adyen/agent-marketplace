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
The Landing, Docs, and Marketplace surfaces SHALL present the agent-marketplace logo artwork at one consistent displayed width of 160 CSS pixels and optical vertical alignment in their primary navigation, select a contrasting supplied logo variant for light and dark appearance, and publish the supplied favicon at each surface's configured base path. Route-dependent navigation wrappers MAY retain different widths. The logo link MUST retain the accessible product name and existing home destination.

#### Scenario: Browser surfaces present accessible adaptive branding
- **WHEN** a user visits Landing, a Docs page with a sidebar, or Marketplace in light or dark appearance
- **THEN** the primary navigation shows the contrasting agent-marketplace logo artwork at a displayed width of 160 CSS pixels and the same optical vertical alignment on every surface, its home link is named `agent-marketplace`, and the document references the supplied favicon from that surface's configured base path

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

### Requirement: Marketplace header navigation
The Marketplace primary header SHALL use its accessible logo link as the Marketplace home action without also showing a `Marketplace` text link. It SHALL provide a visibly marked external Docs link to `https://agent-marketplace.io/docs` that bypasses Nuxt routing and opens in a separate browser tab with opener isolation.

#### Scenario: Marketplace header offers non-redundant external documentation navigation
- **WHEN** a user views the Marketplace primary header
- **THEN** the logo remains a Marketplace home link and no `Marketplace` text link is shown
- **AND** the Docs link has a visible external-link marker, resolves to `https://agent-marketplace.io/docs`, opens in a new tab, and prevents the opened page from accessing its opener

### Requirement: Shared slash brand motif
Landing and Marketplace SHALL reuse a literal weight-800 `/` in the logo green `#00D16A` at approved structural brand locations. Every approved slash SHALL attach directly to the following text without whitespace. Landing SHALL show it before every section eyebrow but SHALL NOT show it before the hero secondary line. Landing eyebrow labels SHALL use the primary text color so the green slash remains a distinct accent. Marketplace SHALL show it before the instance title. The functional Filters and Resources catalog headings SHALL NOT show the motif. Decorative slashes MUST be hidden from the accessibility tree so the existing heading and label names remain unchanged. Other content SHALL NOT gain the motif implicitly.

#### Scenario: Browser surfaces carry the approved slash motif
- **WHEN** a user visits Landing or the Marketplace catalog
- **THEN** a weight-800 `#00D16A` slash appears at each approved section-label and instance-title location
- **AND** every slash is attached directly to its following text without whitespace
- **AND** Landing eyebrow labels use the primary text color while the hero secondary line has no motif slash
- **AND** no motif slash appears in the Filters or Resources catalog heading
- **AND** accessible heading and label names remain unchanged

### Requirement: Selective resource-title slash motif
Marketplace resource detail titles SHALL show the shared weight-800 `#00D16A` slash immediately before the resource name when the resource kind is skill or command. The slash MUST be decorative and hidden from the accessibility tree, leaving the accessible heading name unchanged. Detail titles for every other resource kind MUST remain unprefixed.

#### Scenario: Skill and command detail titles use the slash motif
- **WHEN** a user views a skill or command detail page
- **THEN** the visible title is prefixed directly by the shared green slash
- **AND** the accessible heading name remains the resource name without the slash

#### Scenario: Other resource detail titles remain unprefixed
- **WHEN** a user views a detail page for any other resource kind
- **THEN** its visible and accessible title remains the unprefixed resource name
