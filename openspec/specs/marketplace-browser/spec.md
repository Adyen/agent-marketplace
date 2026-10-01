# marketplace-browser Specification

## Purpose
Defines browser behavior that keeps Marketplace resource discovery controls usable across supported viewport sizes and catalog sizes.

## Requirements

### Requirement: Collapsed desktop filters follow page scrolling

The Marketplace browser SHALL keep the collapsed desktop filter rail in natural page flow without an independent vertical scrollport.

#### Scenario: Collapsed filter rail follows the resource list

- **WHEN** the desktop registry filter is collapsed and the page scrolls
- **THEN** the filters and resource list MUST move together until sticky positioning stops the filter rail

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

### Requirement: Collapsed registry filters fill available space

The Marketplace browser SHALL limit the collapsed registry filter list to seven initial registry options while keeping any selected registry visible.

#### Scenario: Expansion action follows seven initial registries

- **WHEN** more than seven registries are available and the registry filter is collapsed
- **THEN** the browser MUST show the first seven registry options, any additionally selected registry, and the action that expands the complete registry list

### Requirement: Desktop catalog controls remain visible while browsing resources

The Marketplace browser SHALL keep the desktop search, resource heading and count, active-filter controls, and metadata warning together at the same sticky offset as the filter rail after the introduction scrolls away. Resource rows SHALL continue through normal document scrolling beneath the sticky controls.

#### Scenario: Catalog controls stick while resource rows scroll

- **WHEN** a desktop user scrolls a resource list beyond the Marketplace introduction
- **THEN** the introduction MUST scroll out of view
- **AND** the catalog controls and filter rail MUST remain aligned below the primary navigation
- **AND** subsequent page scrolling MUST move resource rows while those controls remain stationary
