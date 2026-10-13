# resource-installation Specification

## Purpose
Defines named batch installation from Git and directory sources, including shared options, failure safety, and compatibility with existing installation modes.

## Requirements

### Requirement: Multiple named resources share one install operation

The install command SHALL accept one or more resource names from configured registries or a single `--from` Git or directory source. Shared scope, resource kind, ref, and path options MUST apply to every requested name. The operation MUST resolve all requested selections before publishing configuration and acquisition lock changes, and MUST synchronize the batch once. Repeated names MUST be selected and reported once, in first-request order.

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

### Requirement: Install reports associated implied prerequisites

After its existing summary, install SHALL report transitive implied resources reachable from the requested kind and name identities or expanded collection members as `also selected` with their kind, name and `required by <kind> "<name>"` provenance. When expansion is disabled, it SHALL instead report eligible transitive preview resources as `would also select` without selecting or writing those resources. Repeated implied identities SHALL be reported once. Unrelated selections and previews SHALL NOT appear in this summary. A direct selection with requirement provenance SHALL NOT be reported as implied. A prerequisite without an eligible candidate SHALL retain existing unmet warnings and SHALL NOT receive a selection summary. Existing install summaries, collection handling and unmet-warning filtering SHALL remain intact.

#### Scenario: Enabled install reports transitive implied prerequisites

- **WHEN** an install selects a resource whose eligible prerequisites form a transitive chain with expansion enabled
- **THEN** the existing install summary MUST be followed by each associated implied resource and its requiring resource
- **AND** configuration MUST contain only the requested selections, not implied selections

#### Scenario: Disabled install reports a transitive preview

- **WHEN** an install selects a resource with an eligible transitive prerequisite chain and expansion is disabled
- **THEN** install MUST report `would also select` for associated preview resources after its existing summary
- **AND** the preview resources MUST NOT be selected, configured or newly materialized

#### Scenario: Install summary omits direct and unrelated resources

- **WHEN** requested resources or collection members share prerequisites, a prerequisite is directly selected, and unrelated selected resources have their own prerequisites
- **THEN** install MUST report each reachable implied resource once and MUST NOT call the direct prerequisite implied
- **AND** unrelated prerequisite summaries and unmet warnings MUST be omitted

#### Scenario: Unavailable install prerequisite keeps its warning

- **WHEN** a requested resource declares an excluded, missing or unconfigured-source prerequisite
- **THEN** install MUST retain the existing remediable unmet warning and successful ordinary exit status
- **AND** it MUST NOT claim that unavailable prerequisite was selected or previewed
