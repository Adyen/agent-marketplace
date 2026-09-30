# resource-selection Specification

## Purpose
Defines how selected resources with the same installed name are reconciled
within each install scope while required machine selections prevent repository shadowing.
## Requirements
### Requirement: Name collisions resolve per install scope

When different sources select resources of the same kind and installed name,
Agent Marketplace SHALL resolve competing selections separately for machine
and repository install scopes. Sources in different scopes MUST NOT displace
one another or produce a name-collision warning solely because they share an
installed name, except when a required machine-scope selection prevents a
different non-required repository source from shadowing its installed name.
Sources competing within one scope SHALL retain deterministic precedence and
a name-collision warning for different registry sources.

#### Scenario: Repository winner does not displace a user-scope selection

- **WHEN** user configuration selects a non-required skill from a Git source and repository configuration selects a different skill with the same installed name from another source
- **THEN** repository `sync` MUST retain the user skill in the machine harness target and install the repository skill in the repository harness target
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
