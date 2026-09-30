## ADDED Requirements

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
