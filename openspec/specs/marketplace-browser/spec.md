# marketplace-browser Specification

## Purpose
Defines browser behavior that keeps Marketplace resource discovery controls usable across supported viewport sizes and catalog sizes.

## Requirements

### Requirement: Collapsed registry filters surface expansion

The Marketplace browser SHALL limit the collapsed registry filter list to five initial registry options while keeping any selected registry visible.

#### Scenario: Expansion action follows five initial registries

- **WHEN** more than five registries are available and the registry filter is collapsed
- **THEN** the browser MUST show the first five registry options, any additionally selected registry, and the action that expands the complete registry list

### Requirement: Collapsed desktop filters follow page scrolling

The Marketplace browser SHALL keep the collapsed desktop filter rail in natural page flow without an independent vertical scrollport.

#### Scenario: Collapsed filter rail follows the resource list

- **WHEN** the desktop registry filter is collapsed and the page scrolls
- **THEN** the filters and resource list MUST move together until sticky positioning stops the filter rail

### Requirement: Expanded desktop filters scroll independently

The Marketplace browser SHALL render every registry after the expansion action is activated and SHALL bound the expanded desktop filter controls to an independently scrollable viewport region.

#### Scenario: Expanded filter rail scrolls to final registry

- **WHEN** the user activates the registry expansion action and the complete registry list exceeds the available desktop viewport height
- **THEN** every registry MUST be rendered and the user MUST be able to scroll the expanded filters to the final registry without scrolling the resource list
