## ADDED Requirements

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
disambiguation. An explicit `--scope` MUST NOT silently edit another layer.
Required selections MUST remain protected, included files MUST remain unchanged,
and automatically selected resources and collection members MUST retain their
existing removal behavior.

#### Scenario: Ambiguous undiscoverable selections require disambiguation

- **WHEN** a name matches an undiscoverable explicit selection and another selection of a different kind or source
- **THEN** removal MUST refuse without configuration changes and request the applicable `--kind`, `--scope`, or `--from` disambiguation
- **AND** a disambiguated removal MUST preserve the other selection

#### Scenario: Undiscoverable removal respects scope and required policy

- **WHEN** an undiscoverable selection is removed with an explicit scope or is required
- **THEN** removal MUST preserve existing scope-boundary and required-resource refusals
- **AND** successful removal MUST modify only the applicable writable layer, preserving included files
