# Status reporting Specification

## Purpose
Defines how the status command identifies selected resources whose materialization failed without assigning an ambiguous error to an unrelated resource.

## Requirements

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

### Requirement: Status reports drift at the effective output root

The `status` command SHALL inspect selected resources at the same output root as `sync --check` when a nested personal-local configuration is active. It MUST NOT report unrelated parent-root artifacts as pending removals or orphaned files. Status MUST remain read-only and retain exact-lock error reporting.

#### Scenario: Nested output already reconciled

- **WHEN** a parent repository configuration and nested personal-local configuration select resources, the nested directory has been synchronized, and unrelated parent-root artifacts exist
- **THEN** `status` MUST report no pending writes or removals when `sync --check` finds no drift
- **AND** `status` MUST NOT list the parent-root artifacts as orphaned

#### Scenario: Nested output drifts

- **WHEN** a nested personal-local configuration has been synchronized and a nested output is removed or modified
- **THEN** `status` MUST report the pending operation for the nested output, consistent with `sync --check`
- **AND** it MUST NOT report unrelated parent-root removals

#### Scenario: Root inspection and exact locks remain safe

- **WHEN** `status` inspects a root-level configuration or encounters a failed exact-lock read
- **THEN** it MUST retain root-level drift behavior and report the exact-lock failure
- **AND** it MUST NOT modify output files or locks
