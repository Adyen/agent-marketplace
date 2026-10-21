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
The Landing, Docs, and Marketplace surfaces SHALL use the approved Adyen
variable Sans and Mono fonts and shared Agent Marketplace brand palette through
surface-owned semantic adapters. Their primary navigation SHALL present the
supplied logo at one stable size and optical alignment. Public Marketplace
navigation, browse content, and footer SHALL use the same responsive maximum
width and outer gutters as Landing and Docs. Marketplace availability, warning,
error, control, and inspection semantics SHALL remain distinct.

#### Scenario: Browser surfaces present accessible adaptive branding
- **WHEN** a user visits Landing, Docs, or Marketplace in light or dark appearance
- **THEN** every surface uses the approved fonts and shared brand palette
- **AND** each surface retains its framework-specific semantic adapter
- **AND** Marketplace browse content shares the Landing/Docs left and right edges
- **AND** the accessible logo and existing destinations remain unchanged

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
Landing and Marketplace SHALL reuse a logo-green `#00D16A` slash mark at
approved structural brand locations. Every mark SHALL retain the angle of the
slash in the shared horizontal logo. Landing SHALL show the motif before every
section eyebrow but SHALL NOT show it before the hero secondary line.
Marketplace SHALL show it before the instance title, between registry and
resource names in result rows, and before skill or command detail titles.
Resource rows SHALL retain their leading `type · registry / name` arrangement,
with the linked coordinate accessible as `registry/name`. Filters, Resources,
and other detail-title kinds SHALL remain unprefixed. Decorative prefix slashes
MUST be hidden from the accessibility tree.

#### Scenario: Browser surfaces carry the approved slash motif
- **WHEN** a user visits Landing or the Marketplace catalog
- **THEN** approved structural labels and the Marketplace instance title show the shared green slash
- **AND** Filters and Resources remain unprefixed
- **AND** accessible heading and label names remain unchanged

#### Scenario: Resource row highlights its meaningful coordinate separator
- **WHEN** the Marketplace displays a resource result row
- **THEN** resource type, registry, separator, and resource name remain on the leading line
- **AND** the resource name is primary while type and registry use regular muted styling
- **AND** the linked coordinate remains accessible as `registry/name` and retains its route
- **AND** hovering the linked coordinate shows one continuous straight underline

#### Scenario: Skill and command detail titles use the slash motif
- **WHEN** a user views a skill or command detail page
- **THEN** the visible title is prefixed by the shared slash mark
- **AND** the accessible heading name remains the resource name without the slash

#### Scenario: Other resource detail titles remain unprefixed
- **WHEN** a user views a detail page for any other resource kind
- **THEN** its visible and accessible title remains the unprefixed resource name

### Requirement: Public Landing visual hierarchy
Landing SHALL use the shared public-site typography, palette, spacing, and
geometry roles while preserving its existing section order, destinations,
product content, and controls. The hero SHALL keep its text and product-preview
arrangement on wide screens and stack responsively on narrower screens. Its two
headline phrases SHALL use the same size and weight with primary and muted
tones. Landing surfaces SHALL remain flat without decorative background grids,
glows, ornamental gradients, or drop shadows.

#### Scenario: Landing presents the shared flat visual system
- **WHEN** a user visits Landing in light or dark appearance
- **THEN** the hero uses the shared public-site type roles with equal-sized primary and muted headline phrases
- **AND** its page and product surfaces use the shared flat palette without decorative grids, glows, ornamental gradients, or drop shadows
- **AND** the existing sections, destinations, product content, and controls remain available

### Requirement: Landing motion remains accessible and resilient
Landing SHALL animate the hero once on page entry and decode section eyebrow
labels once when they first become visible. Real content MUST remain in the DOM
and accessible throughout; decorative duplicate or noise text MUST be hidden
from assistive technology. A live reduced-motion preference SHALL show final
content without arrival, label-roll, decode, or travelling-indicator movement
while preserving copy, selection, and keyboard functionality.

#### Scenario: Reduced motion preserves final Landing content and controls
- **WHEN** a user visits Landing with reduced motion enabled
- **THEN** hero and eyebrow content is immediately visible without displacement or decoding noise
- **AND** action, copy, and tab controls remain functional without movement

### Requirement: Command copying reports truthful feedback
After a Landing command is successfully copied, its control SHALL show
`Copied`, use the success fill, announce success politely, and restore `Copy`
after the feedback interval. A rejected clipboard write MUST NOT show success.
The copied command bytes MUST remain unchanged, and pending feedback work MUST
be cancelled when the component unmounts.

#### Scenario: Successful command copy is visible and announced
- **WHEN** a user activates Copy and the clipboard write succeeds
- **THEN** the control shows `Copied` with success presentation and a polite success announcement
- **AND** the clipboard receives the exact displayed command

### Requirement: Harness tabs provide one accessible selection model
Landing harness tabs SHALL expose tab and tabpanel relationships, one roving
tab stop, and automatic selection with wrapping Left, Right, Up, and Down arrow
navigation plus Home and End. A single decorative indicator MAY follow the
selected tab but MUST NOT create a second selection model.

#### Scenario: Harness tabs wrap keyboard selection
- **WHEN** a user navigates the harness tabs with arrows, Home, or End
- **THEN** focus and selection move together according to the wrapping tab order
- **AND** exactly one tab remains selected and keyboard-focusable

### Requirement: Landing scope choices remain comparable
Landing SHALL present user, repository, and personal-local installation scopes
as one informational comparison panel with every scope's CLI value,
configuration path, reach, and version-control outcome visible. The comparison
MUST NOT expose a selection model. Repository scope SHALL be identified as the
default, and Landing SHALL show a valid repository-scope example command.

#### Scenario: Scope comparison identifies the repository default
- **WHEN** a user reaches the Landing scope comparison
- **THEN** all scope outcomes and corresponding CLI values are visible without a scope selector
- **AND** repository scope is identified as the default
- **AND** the example command uses repository scope without performing an installation

### Requirement: Landing harness order leads with its default
Landing SHALL keep Claude Code as the initially selected harness depiction and
place it first in the visible harness tab order. Reordering the depiction MUST
NOT change the supported harness set or capability data.

#### Scenario: Default harness leads the tab order
- **WHEN** a user reaches the Landing harness depiction
- **THEN** Claude Code is the first and selected harness tab
- **AND** every previously listed harness and its capability data remains present

### Requirement: Marketplace catalog remains an efficient index
The catalog SHALL preserve search, URL-backed kind and registry filters,
active filters, result status, incremental loading, resource actions, and
responsive filter disclosure. Wide layouts SHALL keep the filter rail and
result controls available while browsing. Narrow layouts SHALL contain long
content without page-level horizontal scrolling.

#### Scenario: Catalog remains usable across viewport sizes
- **WHEN** a user browses or filters Marketplace at narrow, medium, or wide widths
- **THEN** search, filters, result status, and available row actions remain reachable
- **AND** wide layouts retain the filter rail while narrow layouts disclose it on demand
- **AND** the page does not introduce horizontal overflow

### Requirement: Marketplace browse typography remains cohesive
The Marketplace browse page SHALL use Adyen Sans for page headings,
introduction copy, resource identity and description, filters, search, actions,
metadata, counts, and availability labels. Adyen Mono SHALL be reserved for
actual installation commands and code blocks. Shared roles SHALL define a
32/40px weight-600 page title, 16/24px weight-400 introduction, 22/28px
weight-600 Resources heading, 16/24px resource coordinate with a weight-600
name and weight-400 context, 14/20px description and controls, 14/20px
weight-500 buttons, 12/18px metadata and statuses, and 12/18px weight-600
filter headings. Labels SHALL use sentence case without added letter spacing.

#### Scenario: Browse roles use the approved hierarchy
- **WHEN** a user browses resources in either appearance at wide or narrow widths
- **THEN** every browse role uses its shared Sans typography token
- **AND** the unchanged top-line identity emphasizes the resource name by weight and color
- **AND** descriptions remain less prominent than names
- **AND** long resource and registry names remain contained without page overflow
- **AND** installation-count placement, controls, and behavior remain unchanged
