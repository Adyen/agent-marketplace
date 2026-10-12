## ADDED Requirements

### Requirement: Desktop catalog controls remain visible while browsing resources

The Marketplace browser SHALL keep the desktop search, resource heading and count, active-filter controls, and metadata warning together at the same sticky offset as the filter rail after the introduction scrolls away. Resource rows SHALL continue through normal document scrolling beneath the sticky controls.

#### Scenario: Catalog controls stick while resource rows scroll

- **WHEN** a desktop user scrolls a resource list beyond the Marketplace introduction
- **THEN** the introduction MUST scroll out of view
- **AND** the catalog controls and filter rail MUST remain aligned below the primary navigation
- **AND** subsequent page scrolling MUST move resource rows while those controls remain stationary
