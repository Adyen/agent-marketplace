## REMOVED Requirements

### Requirement: Top-level collection lookup preserves required selection

**Reason**: Legacy top-level collection syntax and implicit required-catalog selection are not accepted consumer configuration.

**Migration**: Use source-bound `collection/<name>` selectors in v4 `resources` groups and explicit `all: true` to select an entire Git or directory source. Convert unsupported writable configurations before upgrading, using an older released migrator subject to its conversion limits; deploy organization configuration externally.

## ADDED Requirements

### Requirement: Source-bound collection requests preserve required selection

Consumer collection selections SHALL be authored as `collection/<name>` selectors in v4 `resources` groups bound to declared sources, not as legacy top-level collection requests. Selecting a collection MUST NOT disable another group's required selections. A required Git or directory group with `all: true` SHALL retain its catalog selection; an explicitly authored required subset SHALL remain a subset. `required: true` alone MUST NOT select any resource or implicitly select the catalog.

#### Scenario: Collection keeps required all selection

- **GIVEN** an organization v4 resource group selects its source catalog with `all: true` and `required: true`
- **WHEN** a repository v4 resource group selects `collection/shared` from its declared source
- **THEN** all selected organization resources MUST remain required and materialized in machine scope
- **AND** the requested collection MUST expand in repository scope without a spurious missing-collection diagnostic

#### Scenario: Collection preserves authored required subset

- **GIVEN** an organization v4 resource group explicitly selects a required subset of its source catalog
- **WHEN** a repository v4 resource group selects a collection from its declared source
- **THEN** the explicitly selected required resources and collection members MUST remain selected in their respective scopes
- **AND** unrelated organization resources MUST NOT become selected solely because the collection is selected

#### Scenario: Required policy alone selects no resources

- **WHEN** a v4 Git or directory resource group declares `required: true` without `include`, `tags`, or `all: true`
- **THEN** that group MUST select no resources
- **AND** selecting a collection from another group MUST NOT activate that unselected catalog
