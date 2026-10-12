## ADDED Requirements

### Requirement: Bounded selectable grouping and bulk modals

Source and Kind sections and their nesting SHALL render within supported dimensions without changing existing scope-prefixed registry-qualified resource labels. Section labels, filters, and modal content SHALL sanitize terminal control data and preserve printable Unicode. View settings and bulk toggle SHALL use the existing bounded modal frame and keep necessary choices, confirmation, cancellation, and hard-quit controls reachable at 50×12, 80×24, and 120×34.

#### Scenario: Groupings and modals remain safe at supported sizes

- **WHEN** each grouping and either new modal renders at minimum and common terminal sizes with long Unicode or hostile source labels, paths, and filters
- **THEN** rendered output SHALL fit physical bounds, hostile controls SHALL be escaped, nested sections and resource scope columns SHALL remain readable, and required modal actions SHALL remain reachable
