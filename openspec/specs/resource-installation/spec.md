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

### Requirement: Explicit-source installs can be bound to a previewed commit

An install from an explicit Git source MAY carry the commit a preview
resolved. When the source's ref no longer resolves to that commit at install
time, the install MUST fail before publishing configuration or acquisition
lock changes or materializing anything, and MUST report both commits. When it
matches, the acquisition lock MUST record that commit while configuration
keeps the requested ref. An expected commit for a directory source MUST be
refused before any write. An explicit-source install MAY select resources by
kind and name; resources of different kinds that share a name MUST then be
selected unambiguously, and collections MUST be selectable this way.

#### Scenario: Moved source refuses an expected-commit install

- **WHEN** an explicit-source install carries an expected commit and the
  source's ref has advanced to a different commit
- **THEN** the install MUST fail naming both commits and MUST leave
  configuration, acquisition locks, and materialized resources unchanged

#### Scenario: Matching expected commit installs at the requested ref

- **WHEN** an explicit-source install carries the commit the source's ref
  currently resolves to
- **THEN** configuration MUST record the requested ref and the acquisition
  lock MUST record that commit

#### Scenario: Expected commit for a directory source is refused

- **WHEN** an explicit-source install from a directory carries an expected
  commit
- **THEN** the install MUST fail before any write

#### Scenario: Kind-qualified picks select resources that share a name

- **WHEN** an explicit-source install selects a skill and a command that share
  a name, and a collection, by kind and name
- **THEN** exactly those resources MUST be selected from that source and
  materialized, without an ambiguity error

### Requirement: Source previews reuse a current cache

A preview of an unconfigured Git source without a ref MUST look up the
remote's default branch and the commit it advertises in one remote request.
When the cached checkout of that branch is already at the advertised commit
and contains the requested path, the preview MUST NOT fetch and MUST report
the advertised commit; otherwise it MUST refresh the cache as before. A
preview whose ref is a full commit SHA MUST NOT access the network when the
cached checkout of that ref is already at that commit and contains the
requested path. Reusing the cache MUST NOT change the preview's resources,
warnings, or reported commit compared with a refreshed preview of the same
commit.

#### Scenario: Default-branch preview skips the fetch when the cache is current

- **WHEN** a source is previewed without a ref and the cached checkout of its
  default branch is at the commit the remote advertises
- **THEN** the preview MUST NOT fetch and MUST report that commit

#### Scenario: Default-branch preview refreshes a stale cache

- **WHEN** a source is previewed without a ref after its default branch
  advanced past the cached commit
- **THEN** the preview MUST fetch and MUST report the advanced commit and its
  resources

#### Scenario: Commit-pinned preview works from the cache offline

- **WHEN** a source is previewed with a full commit SHA as the ref, the cached
  checkout is at that commit, and the remote is unreachable
- **THEN** the preview MUST succeed with that commit and its resources

### Requirement: Lock-pinned sources are not reported as moved

When an explicit-source install carries an expected commit, and the target
layer's existing acquisition lock already pins the source's URL and ref at a
different commit that the install keeps, the install MUST fail before
publishing configuration or acquisition lock changes or materializing
anything. The refusal MUST be distinguishable from a moved source, MUST NOT
report that the source moved, and MUST name the target scope, the locked
commit, the expected commit, and an `update` command that advances the
source in that scope.

#### Scenario: Lock-pinned source refuses an expected-commit install with update remediation

- **WHEN** an explicit-source install into a layer whose acquisition lock
  pins the source's ref at an older commit carries the commit the ref
  currently resolves to as its expected commit
- **THEN** the install MUST fail naming the scope, both commits, and the
  `update` command, MUST NOT report that the source moved, and MUST leave
  configuration, acquisition locks, and materialized resources unchanged
