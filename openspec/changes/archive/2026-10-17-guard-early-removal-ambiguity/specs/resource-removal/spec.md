## MODIFIED Requirements

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
