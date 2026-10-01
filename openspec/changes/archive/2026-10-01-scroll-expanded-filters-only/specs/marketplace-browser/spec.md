## ADDED Requirements

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

## REMOVED Requirements

### Requirement: Sticky desktop filter rail remains reachable

**Reason**: Independent scrolling is no longer applied to the collapsed sticky rail.

**Migration**: Use natural page scrolling while collapsed and the expanded-only scrollport after activating **Show all**.

### Requirement: Pre-sticky desktop filter rail remains visible

**Reason**: The collapsed rail no longer owns an independent scrollport before sticky positioning.

**Migration**: Scroll the page naturally while filters are collapsed; activate **Show all** to create the bounded expanded scrollport.
