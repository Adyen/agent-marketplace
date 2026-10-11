## ADDED Requirements

### Requirement: Collapsed registry filters fill available space

The Marketplace browser SHALL limit the collapsed registry filter list to seven initial registry options while keeping any selected registry visible.

#### Scenario: Expansion action follows seven initial registries

- **WHEN** more than seven registries are available and the registry filter is collapsed
- **THEN** the browser MUST show the first seven registry options, any additionally selected registry, and the action that expands the complete registry list

## MODIFIED Requirements

### Requirement: Expanded desktop filters scroll independently

The Marketplace browser SHALL render every registry after the expansion action is activated and SHALL bound the expanded desktop filter controls to an independently scrollable region that fills the available sticky viewport area. Its scrollbar SHALL be thin, with a thumb that is transparent at rest and visible while the expanded filters are hovered or contain keyboard focus.

#### Scenario: Expanded filter rail scrolls to final registry

- **WHEN** the user activates the registry expansion action and the complete registry list exceeds the available desktop viewport height
- **THEN** every registry MUST be rendered and the user MUST be able to scroll the expanded filters to the final registry without scrolling the resource list

#### Scenario: Expanded scrollbar appears on interaction

- **WHEN** the expanded desktop filters are at rest
- **THEN** the scrollbar thumb MUST be transparent
- **AND WHEN** the user hovers the expanded filters or moves keyboard focus within them
- **THEN** the scrollbar thumb MUST become visible while remaining thin

## REMOVED Requirements

### Requirement: Collapsed registry filters surface expansion

**Reason**: The accepted desktop layout uses seven initial registry options instead of five to fill the available collapsed filter area.

**Migration**: Use the replacement collapsed-filter requirement, which preserves selected-option visibility and the expansion action after seven initial registries.
