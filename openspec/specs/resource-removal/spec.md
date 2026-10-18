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
