# resource-selection Specification

## Purpose
Defines how selected resources with the same installed name are reconciled
within each install scope while required machine selections prevent repository shadowing.

## Requirements

### Requirement: Name collisions resolve per install scope

When different sources select resources of the same kind and installed name,
whether directly or through a collection, Agent Marketplace SHALL resolve
competing selections separately for machine and repository install scopes.
Sources in different scopes MUST NOT displace one another or produce a
name-collision warning solely because they share an installed name, except
when a required machine-scope selection prevents a different non-required
repository source from shadowing its installed name. A repository selection
from the required source itself does not shadow the required resource, and
whether it remains installed MUST NOT depend on how the shadowing source
locations sort. Sources competing within one scope SHALL retain deterministic
precedence and a name-collision warning for different registry sources.
Within one install scope, a direct selection takes precedence over a
collection member with the same kind and installed name, except that a
required selection outranks a non-required one whether either is selected
directly or through a collection. A required collection member that displaces
a non-required selection in its install scope SHALL produce a name-collision
warning that identifies the required source as the winner and each displaced
source location as a runner-up, unless the displaced selection is from the
required member's own source location.

#### Scenario: Repository winner does not displace a user-scope selection

- **WHEN** user configuration selects a non-required skill from a Git source and repository configuration selects a different skill with the same installed name from another source
- **THEN** repository `sync` MUST retain the user skill in the machine harness target and install the repository skill in the repository harness target
- **AND** `sync --check` MUST succeed from both the repository and the user's home after either sync order
- **AND** the complementary selections MUST NOT produce a name-collision warning

#### Scenario: Collection member is not displaced by a selection in the other scope

- **WHEN** configuration in one install scope selects a collection whose member has the same kind and installed name as a resource that configuration in the other install scope selects from another source, directly or through another collection
- **THEN** repository `sync` MUST install each resource in its own install scope's harness target
- **AND** the machine harness target MUST contain the same resources whether sync runs from the repository or from the user's home
- **AND** `sync --check` MUST succeed from both the repository and the user's home after either sync order
- **AND** the complementary selections MUST NOT produce a name-collision warning

#### Scenario: Same-scope collision keeps one winner

- **WHEN** different registry sources select resources of the same kind and installed name within one install scope
- **THEN** only the winning source MUST materialize that installed name in that scope
- **AND** a name-collision warning MUST identify only sources competing within that scope
- **AND** a selection with the same installed name in the other scope MUST remain available

#### Scenario: Required user-scope selection suppresses a repository selection

- **WHEN** a required user or organization source selects a resource and a different, non-required repository source selects the same kind and installed name
- **THEN** only the required resource MUST materialize, in the machine harness target
- **AND** the repository harness target MUST NOT contain a resource under that installed name
- **AND** a name-collision warning MUST identify the required source as the winner and the repository source as a runner-up
- **AND** repository `sync` MUST leave the machine output unchanged

#### Scenario: Required selection through a collection suppresses a repository selection

- **WHEN** a required user or organization source and a different, non-required repository source select resources of the same kind and installed name, and at least one of them is selected through a collection
- **THEN** repository `sync` MUST install the required resource in the machine harness target
- **AND** the repository harness target MUST NOT contain the repository source's resource
- **AND** a name-collision warning MUST identify the required source as the winner and the repository source as a runner-up
- **AND** the machine harness target MUST contain the same resources whether sync runs from the repository or from the user's home

#### Scenario: Required suppression keeps the repository copy from the required source

- **WHEN** a required user or organization source selects a resource, directly or through a collection, and repository configuration selects the same kind and installed name both from that required source and from a different, non-required source
- **THEN** the repository harness target MUST contain the resource from the required source, whatever the names of the competing source locations
- **AND** a name-collision warning MUST identify the required source as the winner and only the different source as a runner-up

#### Scenario: Required collection member displaces an optional selection in the same scope

- **WHEN** a required user or organization source selects a collection whose member has the same kind and installed name as a resource that a different, non-required source selects directly in the same install scope
- **THEN** only the required collection member MUST materialize under that installed name in the machine harness target, whichever source is configured first
- **AND** a name-collision warning MUST identify the required source as the winner and the non-required source as a runner-up
- **AND** `sync --check` MUST succeed from both the repository and the user's home after sync
- **AND** a non-required collection member MUST NOT displace a direct selection in its install scope

### Requirement: Required selection misses stay hard across layers

When several configured entries for the same source location (URL, ref, and path) select a resource that the location does not provide, the miss SHALL be reported once and SHALL be required whenever any of those entries is required, regardless of layer order. A required miss SHALL remain an error rather than a warning in `sync`, `status`, and the TUI.

#### Scenario: Optional entry ordered before a required entry for the same missing resource

- **WHEN** an earlier configuration layer selects a missing resource from a source location without `required`, and a later layer selects the same resource from the same location with `required: true`
- **THEN** exactly one not-found diagnostic MUST be reported for the resource and it MUST be required
- **AND** the TUI MUST NOT assess drift as ready, MUST show the row as a hard unavailable row, and a reload of that snapshot MUST fail

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
