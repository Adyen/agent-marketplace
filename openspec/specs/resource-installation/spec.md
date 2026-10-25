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
- **THEN** existing selection, collection expansion, inferred-name or bulk behavior, and output MUST remain unchanged apart from the target scope
- **AND** the target scope MUST follow `--scope` when given and otherwise the user-scope install default

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

### Requirement: Install defaults to user scope

Without `--scope`, the install command SHALL write resource selections to the user configuration layer and materialize them at the machine destination for named, collection, `--tag`, `--all`, `--from`, and Marketplace locator installs. Marketplace endpoint registration MUST continue to default to user scope. Explicit `--scope repo` and `--scope local` MUST retain their existing behavior. The default MUST NOT relax the user-scope boundary, and `remove` MUST retain its existing scope inference.

#### Scenario: Install without scope writes user configuration

- **WHEN** a user installs a configured Git resource without `--scope` from inside a repository
- **THEN** the user configuration MUST select the resource and the resource MUST materialize at the machine destination
- **AND** the repository and personal-local configuration MUST remain unchanged, and an absent repository root MUST remain absent

#### Scenario: Relative directory source without scope is refused

- **WHEN** a user runs `install --from` with a repository-relative directory source and no `--scope`
- **THEN** the command MUST refuse with the existing scope-boundary error
- **AND** existing configuration, acquisition locks, and materialized resources MUST remain unchanged and an absent user root MUST remain absent

### Requirement: Web URL sources fall back to HTTPS

`install --from` and `list --from` SHALL keep the SSH form of a GitHub or GitLab web URL as the preferred transport. When `git ls-remote` cannot reach that SSH form and can reach the HTTPS form `https://<host>/<project>.git`, the command SHALL use the HTTPS form for acquisition, SHALL report the switch in a warning that names both URLs, and, for install, SHALL record the HTTPS form as the source URL. The probe uses no Git credential helper, so a repository that requires authentication falls back only when HTTPS can authenticate otherwise, for example through `GIT_ASKPASS` or `~/.netrc`. When the merged configuration already records the SSH or HTTPS form for the same path and ref, install SHALL reuse that form without probing, preferring the target scope's entries, so it never adds a second source for the same repository. The HTTPS form MUST satisfy `allowed_registries` before it is probed or used; a refused HTTPS form MUST leave the SSH form in use without probing either form. When neither form can be reached, the command MUST keep the SSH form and its existing diagnostic. Git URLs, URLs ending in `.git`, and directory sources MUST NOT be probed.

#### Scenario: Unreachable SSH web URL uses HTTPS

- **WHEN** `install --from` or `list --from` receives a web URL whose SSH form cannot be reached and whose HTTPS form can
- **THEN** the command MUST acquire the registry from the HTTPS form and print a warning naming both URLs
- **AND** a source written by install MUST record the HTTPS form

#### Scenario: Install reuses a configured form of a web URL

- **WHEN** `install --from` receives a web URL whose SSH or HTTPS form the merged configuration already records for the same path and ref
- **THEN** install MUST reuse the recorded form without probing either form
- **AND** it MUST NOT add a second source for the repository

#### Scenario: Reachable SSH web URL keeps SSH

- **WHEN** the SSH form of a web URL can be reached
- **THEN** the command MUST use the SSH form and MUST NOT probe the HTTPS form

#### Scenario: Unreachable web URL keeps the SSH diagnostic

- **WHEN** neither the SSH nor the HTTPS form of a web URL can be reached
- **THEN** the command MUST keep the SSH form so acquisition reports the existing SSH diagnostic

#### Scenario: Blocked HTTPS fallback is not probed

- **WHEN** `allowed_registries` refuses the HTTPS form of a web URL
- **THEN** the command MUST keep the SSH form without probing either form

#### Scenario: Fallback probe ignores credential helpers

- **WHEN** a Git credential helper, including one from the current directory's repository configuration, could authenticate the HTTPS form
- **THEN** the reachability probe MUST NOT use it

#### Scenario: Non-web sources are not probed

- **WHEN** `--from` names a Git URL, a URL ending in `.git`, or a directory
- **THEN** the command MUST use it unchanged without a reachability probe
