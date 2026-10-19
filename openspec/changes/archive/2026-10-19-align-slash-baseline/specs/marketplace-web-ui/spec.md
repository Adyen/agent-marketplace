## MODIFIED Requirements

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
