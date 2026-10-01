# marketplace-browser Specification

## Purpose
Defines browser behavior that keeps Marketplace resource discovery controls usable across supported viewport sizes and catalog sizes.

## Requirements

### Requirement: Desktop filter rail remains reachable

The Marketplace browser SHALL keep every desktop filter control reachable when the filter rail is taller than the available viewport. The rail MUST remain within the visible viewport both before and after it reaches its sticky offset.

#### Scenario: Over-height desktop filter rail scrolls

- **WHEN** the desktop filter rail contains more controls than fit in the available viewport height
- **THEN** the rail MUST remain bounded within the viewport and allow the user to scroll to its final control

#### Scenario: Filter rail stays visible before becoming sticky

- **WHEN** the desktop catalog is at a page position where the filter rail has not reached its sticky offset
- **THEN** the rail MUST use only the height available below its current viewport position and MUST NOT extend behind the viewport bottom
