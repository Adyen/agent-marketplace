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
The Landing and Docs public-site surfaces SHALL use the approved Adyen variable
Sans and Mono fonts and the shared Agent Marketplace Green `#00D16A`, custom
Black `#121A18`, and white palette through common semantic roles in light and
dark appearance. Their shared navigation and footer SHALL use those same roles.
The Landing, Docs, and Marketplace surfaces SHALL present the
agent-marketplace logo artwork at one consistent displayed width of 160 CSS
pixels and optical vertical alignment in their primary navigation, select a
contrasting supplied logo variant for light and dark appearance, and publish
the supplied favicon at each surface's configured base path. Route-dependent
navigation wrappers MAY retain different widths. The logo link MUST retain the
accessible product name and existing home destination. Marketplace application
typography and surface colors SHALL remain owned by its separate application
theme.

#### Scenario: Browser surfaces present accessible adaptive branding
- **WHEN** a user visits Landing, a Docs page with a sidebar, or Marketplace in light or dark appearance
- **THEN** Landing and Docs use the approved Adyen variable fonts and shared Agent Marketplace palette
- **AND** their navigation and footer use the same public-site semantic roles
- **AND** the Marketplace application retains its application-owned typography and surfaces
- **AND** the primary navigation shows the contrasting agent-marketplace logo artwork at a displayed width of 160 CSS pixels and the same optical vertical alignment on every surface, its home link is named `agent-marketplace`, and the document references the supplied favicon from that surface's configured base path

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

The Marketplace primary header SHALL use its accessible logo link as the Marketplace home action without also showing a `Marketplace` text link. It SHALL provide a visibly marked Docs link that bypasses Nuxt routing and opens in a separate browser tab with opener isolation. Documentation links in the header, catalog footer, and CLI setup action SHALL resolve to `https://agent-marketplace.io/docs` by default. With the runtime boolean `NUXT_PUBLIC_DOCS_ENABLED=true`, they SHALL instead resolve to origin-root `/docs/`, including when the web interface uses a non-root base path. An explicit false value SHALL retain public documentation.

#### Scenario: Marketplace header offers non-redundant external documentation navigation

- **WHEN** a user views the Marketplace primary header with local documentation omitted or false
- **THEN** the logo remains a Marketplace home link and no `Marketplace` text link is shown
- **AND** the Docs link has a visible external-link marker, resolves to `https://agent-marketplace.io/docs`, opens in a new tab, and prevents the opened page from accessing its opener
- **AND** catalog footer and CLI setup documentation links resolve to that same public documentation

#### Scenario: Marketplace links to locally hosted documentation when enabled

- **WHEN** local documentation is enabled and a user follows a Marketplace documentation link
- **THEN** the header, catalog footer, and CLI setup action MUST link to origin-root `/docs/` rather than public documentation or a path beneath the web-interface base
- **AND** primary-header documentation navigation MUST open the local documentation in a new tab with opener isolation

### Requirement: Shared slash brand motif
Landing and Marketplace SHALL reuse a logo-green `#00D16A` slash mark at approved structural brand locations. Every mark SHALL retain the angle of the slash in the shared horizontal logo, with dimensions scaling from the surrounding font size and a shared negative eight-hundredths-em vertical alignment that optically balances it against adjacent text. Ordinary title prefixes and coordinate separators SHALL use a one-fifth-em spacing rhythm, applied after title prefixes and on both sides of separators. Section-eyebrow prefixes SHALL use a three-eighth-em trailing interval as the intentional exception for letter-spaced uppercase typography. Resource-coordinate separators SHALL use an optically reduced stroke beside medium-weight text while retaining the shared angle and font-relative height. Landing SHALL show the motif before every section eyebrow but SHALL NOT show it before the hero secondary line. Marketplace SHALL show it before the instance title, between registry and resource names in result rows, and before skill or command detail titles. Other detail-title kinds, the Filters heading, and the Resources heading SHALL remain unprefixed. Decorative prefix slashes MUST be hidden from the accessibility tree. Coordinate separators SHALL preserve the accessible `registry/name` value, route, and truncation behavior, and coordinate hover SHALL show one continuous straight underline. Other content SHALL NOT gain the motif implicitly.

#### Scenario: Browser surfaces carry the approved slash motif
- **WHEN** a user visits Landing or the Marketplace catalog
- **THEN** a `#00D16A` slash matching the shared logo geometry appears at each approved section-label and instance-title location
- **AND** every slash uses the shared font-relative optical baseline offset
- **AND** titles use one-fifth-em trailing spacing while eyebrows use three-eighth-em trailing spacing
- **AND** Landing eyebrow labels use the primary text color while the hero secondary line has no motif slash
- **AND** no motif slash appears in the Filters or Resources catalog heading
- **AND** accessible heading and label names remain unchanged

#### Scenario: Resource row highlights its meaningful coordinate separator
- **WHEN** the Marketplace displays a resource result row
- **THEN** its coordinate remains accessible as `registry/name`
- **AND** the delimiter retains the shared slash angle, height, and optical baseline offset with a lighter optical stroke
- **AND** it carries one-fifth-em spacing on each side
- **AND** hovering the coordinate shows one continuous straight underline across its full width

#### Scenario: Skill and command detail titles use the slash motif
- **WHEN** a user views a skill or command detail page
- **THEN** the visible title is prefixed by the shared slash mark with the shared optical baseline offset and one-fifth-em trailing space
- **AND** the accessible heading name remains the resource name without the slash

#### Scenario: Other resource detail titles remain unprefixed
- **WHEN** a user views a detail page for any other resource kind
- **THEN** its visible and accessible title remains the unprefixed resource name
