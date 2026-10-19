## ADDED Requirements

### Requirement: Organization Marketplace opt-out checks catalog-selected collisions

Unqualified named removal of an organization Marketplace resource MUST include
all resolved matching selections in kind, source, and scope disambiguation before
editing configuration. This includes repository resources selected by `all`,
tags, or collection membership, not only explicit resource includes. Inferring
user scope MUST NOT bypass ambiguity. Existing endpoint-qualified removal and
early removal of undiscoverable authored selections MUST remain supported.

#### Scenario: Catalog-selected repository resources require scope disambiguation

- **GIVEN** an optional organization Marketplace resource shares a name with a repository resource selected by `all`, tags, or a collection
- **WHEN** removal omits the scope needed to distinguish them
- **THEN** removal MUST refuse and preserve configuration, acquisition locks, and installed output
- **AND** an explicit user-scope opt-out MUST write the exact organization-resource exclusion, remove its output, and preserve organization siblings and repository configuration/output

### Requirement: Optional organization collection refusal has a stable identity

Removal of an optional organization Marketplace collection MUST refuse with
`marketplace.remove-organization-collection` while retaining per-member user
opt-out guidance. Required-content refusals MUST remain unchanged.

#### Scenario: Optional organization collection refusal is identifiable

- **WHEN** removal targets an optional organization Marketplace collection
- **THEN** its refusal MUST carry `marketplace.remove-organization-collection`
- **AND** required collection refusal MUST NOT be classified as that optional-content refusal
