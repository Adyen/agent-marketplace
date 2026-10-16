# resource-installation Specification

## Purpose
Defines named batch installation from Git and directory sources, including shared options, failure safety, and compatibility with existing installation modes.

## Requirements

### Requirement: Multiple named resources share one install operation

The install command SHALL accept one or more resource names from configured registries or a single `--from` Git or directory source. All existing consumer roots and include trees required by the operation MUST satisfy `consumer-config-format` before acquisition or new writes; an absent target root retains its v4 creation behavior under `config-mutation`. Shared scope, resource kind, ref, and path options MUST apply to every requested name. The operation MUST resolve all requested selections before publishing configuration and acquisition lock changes, and MUST synchronize the batch once. Repeated names MUST be selected and reported once, in first-request order.

#### Scenario: Install several names from one explicit source

- **WHEN** a user installs several resource names with `--from`, `--scope`, `--kind`, `--ref`, and `--path`
- **THEN** every named resource MUST be selected from that source and path at the requested ref and materialized in the requested scope
- **AND** unrelated selections MUST remain unchanged

#### Scenario: Install several names from configured sources

- **WHEN** a user installs several resource names without `--from`
- **THEN** every name MUST resolve using the configured registries and existing kind-disambiguation and write-scope rules
- **AND** the complete batch MUST be selected and synchronized in one operation

#### Scenario: Repeated names are deduplicated

- **WHEN** a user repeats resource names in a named batch
- **THEN** each resource MUST be selected and reported once in first-request order

### Requirement: Invalid named batches do not publish partial installation

A named batch containing a missing, malformed, ambiguous, or write-scope-ineligible selection MUST fail without publishing configuration or acquisition lock changes or materializing any portion of the batch. Existing configuration, acquisition locks, exclusions, and materialized resources MUST remain unchanged. This does not prohibit private acquisition cache writes required for discovery.

#### Scenario: Invalid later name preserves installation state

- **WHEN** a valid name is followed by a missing, malformed, ambiguous, or write-scope-ineligible name in a configured or explicit-source batch
- **THEN** the command MUST fail and leave existing configuration, acquisition locks, exclusions, and materialized resources unchanged
- **AND** an absent target configuration MUST remain absent

### Requirement: Existing install modes remain compatible

Named batches MUST preserve existing single-name selection semantics, collection expansion, explicit-name exclusion cleanup, and prerequisite-warning filtering. No-name `--from` inference and bulk selection, `--all`, and `--tag` behavior MUST remain unchanged. Marketplace resource locator acquisition and Marketplace endpoint registration MUST remain singleton operations. A multi-argument request containing such an argument, or using Marketplace-only flags, MUST be rejected before configuration, acquisition lock, or materialization writes. Named arguments MUST remain incompatible with `--all` and `--tag`.

#### Scenario: Collections and explicit resources share a batch

- **WHEN** a user installs a collection and another named resource in one batch
- **THEN** the collection members and explicit resource MUST be selected using existing collection and explicit-selection rules
- **AND** prerequisite warnings MUST concern only requested resources and expanded collection members

#### Scenario: Explicit batch names clear matching exclusions

- **WHEN** a user explicitly installs several resources that have matching removable personal-local exclusions
- **THEN** each requested resource MUST be selected and its matching exclusion removed
- **AND** unrelated exclusions MUST remain unchanged

#### Scenario: Singleton and no-name install behavior is retained

- **WHEN** a user installs one resource or collection, or uses `--from` without names
- **THEN** existing selection, collection expansion, inferred-name or bulk behavior, and output MUST remain unchanged

#### Scenario: Incompatible batch arguments are rejected before writes

- **WHEN** a user supplies multiple arguments containing a Marketplace locator or endpoint, supplies Marketplace-only flags with a batch, or combines named arguments with `--all` or `--tag`
- **THEN** the command MUST reject the request before configuration, acquisition lock, or materialization writes
