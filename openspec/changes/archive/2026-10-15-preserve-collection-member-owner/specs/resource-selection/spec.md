## ADDED Requirements

### Requirement: Unqualified collection members retain the resolved owner

A collection member without an explicit registry SHALL retain the source location, configuration layer, required policy, and effective harness targets of the resolved collection definition that contributes it. Another configured entry for the same registry location MUST NOT rebind that member. Existing higher-authority exclusions and independent nested collection definition selection SHALL retain their semantics.

#### Scenario: Unqualified member retains the collection owner

- **GIVEN** several consumer configuration entries refer to the same registry location
- **WHEN** one entry selects a collection with a member that has no explicit registry
- **THEN** the member MUST materialize only in the selected collection owner's install scope and effective harness targets
- **AND** include-file precedence and root versus subtree locations MUST NOT cause the member to inherit an unrelated owner's policy
- **AND** `sync --check` MUST succeed after synchronization

#### Scenario: Same collection remains selected in both install scopes

- **GIVEN** machine and repository configuration select the same collection from the same registry location
- **WHEN** its unqualified members are synchronized
- **THEN** each install scope MUST retain its own member copies
- **AND** those copies MUST NOT produce a name-collision warning solely because they share an installed name
- **AND** `sync --check` MUST succeed after synchronization

#### Scenario: Unqualified required member retains required policy

- **GIVEN** a required collection definition and an optional entry share a registry location
- **WHEN** the required collection contributes a member without an explicit registry
- **THEN** the member MUST retain required policy and the resolved definition's install scope and effective harness targets
- **AND** personal exclusions MUST NOT suppress that member
- **AND** a missing or malformed member MUST retain a required diagnostic

#### Scenario: Nested organization member retains harness diagnostics

- **GIVEN** a collection includes a nested definition owned by organization configuration
- **WHEN** the containing collection narrows that definition's effective harness targets so an unqualified member has no compatible target
- **THEN** the member MUST NOT materialize through that collection
- **AND** its existing required error or organization default warning MUST remain visible
