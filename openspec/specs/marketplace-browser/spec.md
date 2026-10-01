# marketplace-browser Specification

## Purpose
Defines browser behavior that keeps Marketplace resource discovery controls usable across supported viewport sizes and catalog sizes.

## Requirements

### Requirement: Sticky desktop filter rail remains reachable

The Marketplace browser SHALL keep every desktop filter control reachable once an over-height filter rail reaches its fixed sticky offset below the primary header.

#### Scenario: Over-height desktop filter rail scrolls

- **WHEN** the desktop filter rail contains more controls than fit below the primary header and reaches its sticky offset
- **THEN** the rail MUST remain bounded within the viewport and allow the user to scroll to its final control

### Requirement: Collapsed registry filters surface expansion

The Marketplace browser SHALL limit the collapsed registry filter list to five initial registry options while keeping any selected registry visible.

#### Scenario: Expansion action follows five initial registries

- **WHEN** more than five registries are available and the registry filter is collapsed
- **THEN** the browser MUST show the first five registry options, any additionally selected registry, and the action that expands the complete registry list

### Requirement: Pre-sticky desktop filter rail remains visible

The Marketplace browser SHALL keep the desktop filter rail scrollport within the visible viewport before the rail reaches its sticky offset.

#### Scenario: Filter rail stays visible before becoming sticky

- **WHEN** the desktop catalog remains at its initial page position and the user scrolls only the over-height filter rail
- **THEN** the rail MUST remain above the viewport bottom and allow its final control to become visible without scrolling the resource list
