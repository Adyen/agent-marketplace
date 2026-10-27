# resource-removal Specification

## Purpose
Defines removal of explicit selections whose resources are missing or malformed,
including source, kind, scope, and required-resource boundaries.

## Requirements

### Requirement: Explicit undiscoverable selections remain removable

Named removal MUST accept an explicit selection from a validated v4 Git or
directory source when the selected resource is absent from the discovered
catalog or failed catalog validation. Removal MUST use the configured resource
kind, source identity, and layer, including when `--from` is supplied without
`--kind`. The subsequent sync MUST stop reporting that selection as missing or
malformed and MUST preserve unrelated selections.

#### Scenario: Remove a selection after its resource disappears

- **GIVEN** an explicitly selected resource was successfully synchronized
- **WHEN** its resource disappears from the source and the user removes its name
- **THEN** removal MUST succeed and subsequent sync MUST no longer report the stale selection
- **AND** unrelated selections and configuration layers MUST remain unchanged

#### Scenario: Remove a malformed explicit selection

- **GIVEN** an explicitly selected resource fails catalog validation
- **WHEN** the user removes its name, with or without a matching `--from`
- **THEN** removal MUST use its configured kind and clear the selection without editing source files

### Requirement: Undiscoverable removal preserves selection boundaries

Undiscoverable candidates MUST participate in existing kind, source, and scope
disambiguation before collection or Marketplace early removal can edit
configuration. An explicit `--scope` MUST NOT silently edit another layer.
Required selections MUST remain protected, included files MUST remain unchanged,
and automatically selected resources and collection members MUST retain their
existing removal behavior. Ambiguity admission for a single collection selection
MUST NOT require successful catalog discovery.

#### Scenario: Ambiguous undiscoverable selections require disambiguation

- **WHEN** a name matches an undiscoverable explicit selection and another selection of a different kind or source
- **THEN** removal MUST refuse without configuration changes and request the applicable `--kind`, `--scope`, or `--from` disambiguation
- **AND** a disambiguated removal MUST preserve the other selection

#### Scenario: Undiscoverable removal respects scope and required policy

- **WHEN** an undiscoverable selection is removed with an explicit scope or is required
- **THEN** removal MUST preserve existing scope-boundary and required-resource refusals
- **AND** successful removal MUST modify only the applicable writable layer, preserving included files

#### Scenario: Disambiguate collections before removal

- **GIVEN** an explicit collection and an undiscoverable explicit resource share a name
- **WHEN** removal targets that name with or without a matching `--from`
- **THEN** unqualified removal MUST refuse before editing configuration or pruning active collection output
- **AND** an explicit `--kind` MUST remove only the matching selection and preserve unrelated output

#### Scenario: Disambiguate Marketplace pins before removal

- **GIVEN** an explicit Marketplace pin and an undiscoverable explicit registry resource share a name
- **WHEN** removal targets that name without enough kind, source, or scope disambiguation
- **THEN** removal MUST refuse before editing configuration or acquisition locks
- **AND** a disambiguated removal MUST preserve the other selection

### Requirement: Marketplace selections are removable by endpoint

Named removal with `--from` MUST accept the URL of a configured Marketplace
endpoint and remove only that endpoint's matching direct pin or collection,
updating configuration and the acquisition lock together. Without `--scope`,
it MUST target repository scope like other `--from` removals; an explicit scope
MUST edit only that layer. The ambiguity remediation for a Marketplace
selection MUST name its endpoint with `--from` and MUST NOT include `--ref` or
`--path`. Git and directory `--from` removal MUST remain unchanged. Refusals
for an unsupported qualifier, no matching direct selection, or a URL that also
identifies a Git or directory source MUST carry the stable diagnostic codes
named below.

#### Scenario: Remove a Marketplace pin by its endpoint

- **GIVEN** a direct Marketplace pin and another explicit selection share a kind and name in one scope
- **WHEN** removal refuses for source ambiguity and the user runs the remediation suggested for the pin
- **THEN** only that pin MUST be removed from configuration and the acquisition lock
- **AND** the other selection MUST remain, whether or not its resource is discoverable
- **AND** endpoint removal without `--scope` MUST NOT edit another layer

#### Scenario: Refuse ambiguous or unsupported Marketplace endpoint removal

- **WHEN** `--from` names a configured Marketplace endpoint together with `--ref` or `--path`, names no matching direct selection of the requested kind, or also matches a configured Git or directory source
- **THEN** removal MUST refuse without changing configuration or acquisition locks
- **AND** the refusal MUST report `marketplace.remove-unsupported-qualifier`, `marketplace.remove-not-selected`, or `marketplace.remove-ambiguous-source` respectively

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

### Requirement: Web URL removal matches the HTTPS fallback

`remove --from` with a GitHub or GitLab web URL SHALL target a configured source recorded in the web URL's SSH form or in its HTTPS form `https://<host>/<project>.git`, so the URL used to install a source removes it. Only entries matching the requested `--path` and `--ref` SHALL count, and entries in the target scope, including entries from files it includes, SHALL decide before entries in other layers. When both forms match at the same level, the SSH form MUST be targeted.

#### Scenario: Web URL removes a source stored in HTTPS form

- **WHEN** only the HTTPS form of a web URL is configured and the user runs `remove --from` with that web URL
- **THEN** removal MUST target the HTTPS source

#### Scenario: Web URL prefers a source stored in SSH form

- **WHEN** the SSH form of a web URL is configured, alone or together with its HTTPS form
- **THEN** removal MUST target the SSH source

#### Scenario: Web URL removal follows the entry matching the request

- **WHEN** the target scope, directly or through an included file, or the requested `--path` or `--ref` matches only the HTTPS form, while an SSH form exists in another scope or with other qualifiers
- **THEN** removal MUST target the matching HTTPS source
