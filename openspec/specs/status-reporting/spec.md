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

### Requirement: Status inspects Marketplace selections from the verified cache

The `status` command SHALL include Marketplace selections from every configuration layer in its synced-resource and drift inspection, resolving them from the verified local cache so its verdict agrees with `sync --check` for the same tree. Inspecting Marketplace selections SHALL NOT download resource artifacts or write configuration or lock files. A selected Marketplace pin without usable local artifact evidence MUST stay attributable to its resource coordinate together with the `agent-marketplace sync` remedy, and its materialized outputs MUST NOT be reported as orphaned or pending removal. While any selected Marketplace pin lacks verified local cache evidence, `status` SHALL report DRIFT as unknown, naming the missing Marketplace cache evidence and the `agent-marketplace sync` remedy, and SHALL NOT report files to write or remove. A pin whose revision metadata was never acquired into the local cache SHALL remain a warning that does not by itself make `status` exit nonzero.

#### Scenario: Synced Marketplace selection appears in synced resources

- **WHEN** a Marketplace pin is installed and synchronized and `status` runs
- **THEN** the pinned resource MUST appear in `SYNCED RESOURCES`
- **AND** `status` MUST NOT report it as orphaned or pending removal, and its verdict MUST agree with `sync --check`

#### Scenario: Organization collection members are not orphaned without an organization lock

- **WHEN** an organization configuration selects a Marketplace collection, no organization lock is distributed, and the collection members are synchronized
- **THEN** `status` MUST NOT report the members as orphaned or pending removal
- **AND** it MUST NOT create an organization lock

#### Scenario: Unsynced Marketplace pin reports an attributable error without orphan rows

- **WHEN** a selected Marketplace pin has cached revision metadata but no verified local artifact and `status` runs
- **THEN** `status` MUST exit nonzero with an error that names the pin's resource coordinate and the `agent-marketplace sync` remedy
- **AND** it MUST NOT report the pin's materialized outputs as orphaned
- **AND** its DRIFT section MUST report unknown, naming the missing Marketplace cache evidence and the `agent-marketplace sync` remedy

#### Scenario: Missing Marketplace cache reports unknown drift without orphan rows

- **WHEN** a Marketplace collection has been installed and synchronized, the local Marketplace cache is then deleted, and `status` runs
- **THEN** `status` MUST exit 0 and MUST NOT report orphaned files or files to write or remove
- **AND** its DRIFT section MUST report unknown, naming the missing Marketplace cache evidence and the `agent-marketplace sync` remedy
- **AND** its warnings MUST name the pin's resource coordinate
- **AND** it MUST NOT download resource artifacts and MUST leave configuration and lock files byte-identical

#### Scenario: Never acquired Marketplace pin reports unknown drift

- **WHEN** a Marketplace pin is selected but was never acquired into the local cache and `status` runs
- **THEN** its DRIFT section MUST report unknown, naming the `agent-marketplace sync` remedy
- **AND** it MUST NOT report `In sync`

#### Scenario: Status counts drift when Marketplace cache evidence is complete

- **WHEN** a Marketplace pin is installed and synchronized, its verified cache evidence is present, one of its materialized outputs is deleted, and `status` runs
- **THEN** its DRIFT section MUST report the files to write
- **AND** it MUST NOT report unknown drift

#### Scenario: Status downloads no artifacts and writes no files

- **WHEN** `status` runs for a configuration with Marketplace selections
- **THEN** it MUST NOT download resource artifacts from the Marketplace endpoint
- **AND** it MUST leave configuration and lock files byte-identical
