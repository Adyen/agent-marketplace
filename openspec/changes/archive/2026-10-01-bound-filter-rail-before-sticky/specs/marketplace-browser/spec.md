## ADDED Requirements

### Requirement: Pre-sticky desktop filter rail remains visible

The Marketplace browser SHALL keep the desktop filter rail scrollport within the visible viewport before the rail reaches its sticky offset.

#### Scenario: Filter rail stays visible before becoming sticky

- **WHEN** the desktop catalog remains at its initial page position and the user scrolls only the over-height filter rail
- **THEN** the rail MUST remain above the viewport bottom and allow its final control to become visible without scrolling the resource list
