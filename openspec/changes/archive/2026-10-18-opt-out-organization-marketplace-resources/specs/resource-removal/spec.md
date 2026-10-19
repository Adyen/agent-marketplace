## ADDED Requirements

### Requirement: Optional organization Marketplace resources support user opt-out

Named removal of an optional organization Marketplace resource MUST write an
exact registry-slug/kind/canonical-name exclusion bound to the same Marketplace
endpoint in the user configuration, whether selected directly or through a
collection. Omitted scope MUST infer user scope; explicit user scope and a
matching `--from` endpoint MUST support the same opt-out. A missing user root
MUST be created as v4. The edit MUST reuse an unambiguous existing source for
the endpoint regardless of its Marketplace alias, preserve unrelated selections,
and synchronize removal of only the excluded resource. Organization configuration
and acquisition locks MUST remain unchanged, including an absent organization
acquisition lock remaining absent. Required content MUST remain immune.

#### Scenario: Remove an optional organization Marketplace collection member

- **GIVEN** an optional organization Marketplace collection with multiple members and no deployed organization acquisition lock
- **WHEN** a member is removed with omitted scope, explicit user scope, or explicit user scope and the matching endpoint
- **THEN** removal MUST exit 0, record an exact user exclusion, and remove that member's materialized output
- **AND** sibling members MUST remain installed and subsequent synchronization MUST retain the exclusion

#### Scenario: Remove an optional direct organization Marketplace resource

- **WHEN** a directly selected optional organization Marketplace resource is removed at user scope
- **THEN** removal MUST write its exact user exclusion and remove its output without changing organization configuration

#### Scenario: Organization Marketplace opt-out preserves required and scope boundaries

- **WHEN** removal targets a required organization Marketplace resource or targets an organization resource with explicit repo or local scope
- **THEN** removal MUST refuse without changing configuration, acquisition locks, or installed output
- **AND** a scope-boundary refusal MUST direct the user to `--scope user`

#### Scenario: Organization collection removal explains per-member opt-out

- **WHEN** removal targets an optional organization Marketplace collection itself
- **THEN** removal MUST refuse without changing configuration or installed output
- **AND** the refusal MUST explain that optional members can be excluded individually at user scope
- **AND** whole-collection Marketplace exclusions MUST remain unsupported

#### Scenario: Organization member opt-out disambiguates kind and scope

- **GIVEN** an optional organization Marketplace collection member shares its name with another resource kind or a repository selection
- **WHEN** removal lacks the kind or scope needed to distinguish the selections
- **THEN** removal MUST refuse without changing configuration or installed output and request the applicable disambiguation
- **AND** a disambiguated removal MUST exclude only the intended organization member
