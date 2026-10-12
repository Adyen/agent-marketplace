# resource-removal Specification

## Purpose
Defines how removing an optional resource that remains required affects its selection, effective exclusions, and dependent prerequisite warnings without cascading removal.

## Requirements

### Requirement: Removing a still-required optional resource is non-cascading

When expansion is enabled, `remove` of an optional resource that remains required by a selected resource SHALL succeed when the existing scope and required-policy rules authorize the mutation. It SHALL remove the direct selection when one exists and write an effective explicit exclusion through the existing serialized configuration mutation. Every requiring resource MUST remain selected, and each resulting unmet prerequisite MUST be reported. Removal MUST NOT cascade to requiring resources or add a new flag. When expansion is disabled, removal MUST NOT add a requirement-specific exclusion.

#### Scenario: Enabled removal excludes an optional prerequisite

- **WHEN** expansion is enabled and `remove` targets an optional prerequisite that remains required by a selected resource
- **THEN** removal MUST remove its direct selection if present and write an effective explicit exclusion
- **AND** each requiring resource MUST remain selected and report its unmet prerequisite
- **AND** removal MUST NOT cascade to the requiring resource

#### Scenario: Advisory removal does not add a requirement exclusion

- **WHEN** expansion is disabled and `remove` targets a directly selected optional prerequisite of a selected resource
- **THEN** removal MUST remove the direct selection
- **AND** it MUST NOT write an exclusion solely because of the prerequisite

#### Scenario: Requirement removal preserves required policy and scope authorization

- **WHEN** existing required-selection policy or scope authorization would prevent a remove mutation or make the selected resource belong to another scope
- **THEN** requirement handling MUST NOT bypass that policy or authorization
- **AND** it MUST NOT write an exclusion in one scope that changes the other scope's selection
