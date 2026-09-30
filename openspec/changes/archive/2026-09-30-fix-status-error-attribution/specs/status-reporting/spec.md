## ADDED Requirements

### Requirement: Status attributes materialization errors to the failed resource

The `status` command SHALL mark a resource as `error: not materialized` only when the failure can be attributed to its resource kind, installed name, install scope, and harness target. Errors that cannot be attributed to exactly one resource MUST remain visible in the errors section without marking an unrelated resource.

#### Scenario: Status marks only the failed resource

- **WHEN** two resources have the same installed name and harness target in different install scopes, and only one fails to materialize
- **THEN** `status` MUST mark only the failed resource as `error: not materialized`
- **AND** it MUST retain the other resource's target path

#### Scenario: Same-named resource of another kind is not marked

- **WHEN** resources of different kinds have the same installed name and harness target, and only one fails to materialize
- **THEN** `status` MUST mark only the failed resource as `error: not materialized`
- **AND** it MUST retain the other resource's target path
