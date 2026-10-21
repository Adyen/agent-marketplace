## MODIFIED Requirements

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

## ADDED Requirements

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
