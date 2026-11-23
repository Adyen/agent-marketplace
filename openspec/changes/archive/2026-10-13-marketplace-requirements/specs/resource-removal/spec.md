## ADDED Requirements

### Requirement: Removing a still-required Marketplace prerequisite

`remove` of a Marketplace resource that a selected resource still requires SHALL succeed when the existing scope and required-policy rules authorize the mutation, including when only acquisition-lock requires edges hold it. It SHALL remove any direct selection, and remove the resource's lock record held by requires edges along with those edges. When expansion is enabled, it SHALL write the exact Marketplace exclusion `registry/kind/name` through the existing serialized configuration mutation, in a configuration layer where Marketplace exclusions take effect. Each requiring resource MUST remain selected, and its unmet prerequisite MUST be reported. Removal MUST NOT cascade, MUST NOT report the resource as held by a collection, and MUST NOT write a requires edge. When expansion is disabled, removal MUST NOT write an exclusion solely because of the prerequisite.

#### Scenario: Enabled removal of a requires-held Marketplace prerequisite

- **WHEN** expansion is enabled and `remove` targets a Marketplace resource held only by requires edges of a selected resource
- **THEN** removal MUST succeed, remove its lock record and requires edges, and write the exact Marketplace exclusion
- **AND** the requiring resource MUST remain selected and report its unmet prerequisite

#### Scenario: Advisory removal of a requires-held Marketplace prerequisite

- **WHEN** expansion is disabled and `remove` targets a Marketplace resource held only by requires edges
- **THEN** removal MUST succeed and remove its lock record and requires edges
- **AND** it MUST NOT write an exclusion

#### Scenario: Requires-held removal is not reported as a collection member

- **WHEN** `remove` targets a Marketplace resource held only by requires edges
- **THEN** removal MUST NOT refuse it as selected only through a collection
