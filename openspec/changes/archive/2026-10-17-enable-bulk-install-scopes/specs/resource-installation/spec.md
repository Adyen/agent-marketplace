## ADDED Requirements

### Requirement: Configured bulk installs support every writable scope

The install command SHALL accept `--tag` and `--all` with user, repository, and personal-local scope. It MUST write selections into the requested configuration layer, create an absent writable root as v4, reconcile that layer's acquisition lock, and materialize resources at the corresponding machine or repository destination. Existing consumer roots and includes MUST satisfy `consumer-config-format` before new acquisition or publication. Source declarations and healthy acquisition locks in other layers MUST remain unchanged.

Bulk installation MUST retain existing selection semantics: unqualified tags remain tag selectors, kind-qualified tags select matching explicit locators, and `--all` selects the discovered snapshot rather than future resources. Resource-kind and registry filters, collision refusal, exclusions, collection expansion, and exact acquisition evidence MUST retain their existing behavior. A registry declared in several layers MUST prefer the requested target layer when `--registry` identifies one physical source.

#### Scenario: Bulk selectors install into each writable layer

- **WHEN** a user runs `install --tag` or `install --all`, optionally with `--kind` or an applicable `--registry` filter, for configured Git resources in user, repository, or personal-local scope
- **THEN** the requested layer MUST contain the corresponding tag selector or explicit resource locators and its acquisition lock MUST record the selected exact source
- **AND** the selected resources MUST materialize in that scope while source configuration and healthy locks in other layers remain unchanged
- **AND** an absent target root MUST be created as v4

#### Scenario: Bulk local selection honors the current subdirectory

- **WHEN** a user runs a bulk install with `--scope local` from a subdirectory inheriting a repository configuration with a relative directory source
- **THEN** the personal-local selection MUST be written in that subdirectory and its resources MUST materialize there using the original directory source
- **AND** the parent configuration and its materialized resources MUST remain unchanged

#### Scenario: Bulk user selection refuses repository-relative sources

- **WHEN** a bulk install in user scope would copy a repository-relative directory source into user configuration
- **THEN** the command MUST refuse with the existing scope-boundary error
- **AND** existing configuration, acquisition locks, and materialized resources MUST remain unchanged and an absent target root MUST remain absent

## MODIFIED Requirements

### Requirement: Existing install modes remain compatible

Named batches MUST preserve existing single-name selection semantics, collection expansion, explicit-name exclusion cleanup, and prerequisite-warning filtering. No-name `--from` inference and bulk selection MUST remain unchanged. The selection semantics of `--all` and `--tag` MUST remain unchanged across writable scopes. Marketplace resource locator acquisition and Marketplace endpoint registration MUST remain singleton operations. A multi-argument request containing such an argument, or using Marketplace-only flags, MUST be rejected before configuration, acquisition lock, or materialization writes. Named arguments MUST remain incompatible with `--all` and `--tag`.

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
