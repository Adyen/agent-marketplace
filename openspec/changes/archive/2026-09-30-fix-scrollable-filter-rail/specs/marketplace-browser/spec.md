## Purpose

Defines browser behavior that keeps Marketplace resource discovery controls usable across supported viewport sizes and catalog sizes.

## ADDED Requirements

### Requirement: Desktop filter rail remains reachable

The Marketplace browser SHALL keep every desktop filter control reachable when the filter rail is taller than the available viewport.

#### Scenario: Over-height desktop filter rail scrolls

- **WHEN** the desktop filter rail contains more controls than fit in the available viewport height
- **THEN** the rail MUST remain bounded within the viewport and allow the user to scroll to its final control
