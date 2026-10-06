## ADDED Requirements

### Requirement: Whole-source menu targets one verified source

The TUI SHALL open a whole-source menu with `x` or its command-palette action from a resource or a status section containing one source, including when folded. Both entry points SHALL share mutation guards. Mixed-source sections, malformed resources, discovery-only Marketplace rows, and required-only sources SHALL NOT authorize source mutations. Unverified inventory SHALL NOT open or submit a source mutation.

#### Scenario: Resource or single-source section opens the menu

- **WHEN** the user presses `x` on a verified resource or a section containing only its source
- **THEN** the menu SHALL show that source and its scope actions

#### Scenario: Mixed or protected sources are refused

- **WHEN** the user presses `x` on a mixed-source section or a required-only source
- **THEN** the TUI SHALL show a refusal and SHALL NOT start a mutation

#### Scenario: Unverified source inventory cannot mutate

- **WHEN** a resource of the target source is still verification-pending
- **THEN** the TUI SHALL refuse whole-source mutation while allowing the verified control to open its menu

### Requirement: Whole-source scope selection preserves boundaries

The `g`, `p`, and `l` menu actions SHALL select or deselect the Git or directory source in the chosen writable layer. Selection SHALL use `all: true` for v4 and `default: true` for existing legacy roots. Required resources SHALL remain protected. Local actions SHALL use the validated workspace local root. Source editing SHALL retain unrelated refs and source declarations.

#### Scenario: Whole-source scope selection round trips

- **WHEN** the user enables then disables a Git or directory source in a writable scope
- **THEN** the source selection SHALL change in that scope and its declaration SHALL remain browsable

#### Scenario: Nested workspace uses its local root

- **WHEN** whole-source selection and exclusion run in a nested workspace
- **THEN** they SHALL change its personal-local root and SHALL NOT create the repository-root personal-local file

#### Scenario: A sibling ref stays unchanged

- **WHEN** a legacy root contains the same registry at two refs and one is selected or excluded
- **THEN** the other ref's selections and exclusions SHALL remain unchanged

#### Scenario: Source layouts retain separate menu identities

- **WHEN** resources share a registry URL, ref, and path but declare different `source_harness` values
- **THEN** the menu SHALL count and act on only the selected source layout, and SHALL refuse a section containing both layouts

### Requirement: Whole-source exclusion can be restored

The `e` menu action SHALL toggle a personal-local whole-source exclusion for Git and directory sources without changing the repository config or losing prior selections. Required resources and selections owned by organization or user layers SHALL remain protected. For optional organization Marketplace resources, `e` SHALL toggle exact user overrides instead.

#### Scenario: Local whole-source exclusion restores selections

- **WHEN** the user excludes then restores a Git or directory source
- **THEN** the prior selections SHALL become effective again and unrelated sources SHALL remain unchanged

#### Scenario: Organization Marketplace source uses user overrides

- **WHEN** the user excludes then restores an optional organization Marketplace source
- **THEN** exact user exclusions SHALL be added then removed without changing organization policy

### Requirement: Whole-source writes retain current configuration rules

Missing writable roots SHALL be created as v4. Existing legacy roots SHALL remain readable and writable without implicit migration. Included exclusions or selections that prevent the requested change SHALL be refused without rewriting the root.

#### Scenario: New whole-source configuration roots use v4

- **WHEN** whole-source selection or exclusion creates a missing configuration root
- **THEN** the new root SHALL declare the v4 API version

#### Scenario: Inherited selection cannot be silently removed

- **WHEN** deselecting a source would leave an included file selecting it
- **THEN** the editor SHALL refuse and SHALL preserve the root bytes
