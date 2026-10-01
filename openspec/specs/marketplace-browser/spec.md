# marketplace-browser Specification

## Purpose
Defines browser behavior that keeps Marketplace resource discovery controls usable across supported viewport sizes and catalog sizes.

## Requirements

### Requirement: Sticky desktop filter rail remains reachable

The Marketplace browser SHALL keep every desktop filter control reachable once an over-height filter rail reaches its fixed sticky offset below the primary header.

#### Scenario: Over-height desktop filter rail scrolls

- **WHEN** the desktop filter rail contains more controls than fit below the primary header and reaches its sticky offset
- **THEN** the rail MUST remain bounded within the viewport and allow the user to scroll to its final control
