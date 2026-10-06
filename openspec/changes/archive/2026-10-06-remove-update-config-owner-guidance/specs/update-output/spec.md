## ADDED Requirements

### Requirement: Organization-managed pin reporting omits config owner guidance

The `update` command SHALL report organization-managed Marketplace pins without instructing consumers to ask their organization config owner to update those pins and then run `agent-marketplace sync`.

#### Scenario: Default organization pin output omits owner guidance

- **WHEN** `update` reports organization-managed Marketplace pins without `--verbose`
- **THEN** it SHALL report their count and state that newer revisions were not checked and pins remain unchanged
- **AND** it SHALL retain the hint for listing those pins with `update --verbose`
- **AND** it SHALL NOT print the organization config owner instruction

#### Scenario: Verbose organization pin output omits owner guidance

- **WHEN** `update --verbose` reports organization-managed Marketplace pins
- **THEN** it SHALL retain the organization-managed pin summary and list each pin as not checked
- **AND** it SHALL NOT print the organization config owner instruction
