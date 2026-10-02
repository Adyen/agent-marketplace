## MODIFIED Requirements

### Requirement: Mirrored hooks carry integrity-bound support files

Mirror acquisition MUST package the adjacent support files selected by the
source-layout hook materialization policy, excluding native aggregate
definitions. Their relative paths, bytes, and executable modes MUST be bound
into the canonical hook content revision. Consumers MUST verify declaration
membership and integrity before staging and on the bytes used for materialization.
Existing published archives MUST NOT be repaired in place. The adjacent-file
policy selects flat siblings, not nested directories or other plugin directories;
unsupported expressions MUST have a diagnostic and an achievable remedy.

#### Scenario: Mirrored hook support installation

- **GIVEN** a mirrored hook whose command references a plugin-root script with adjacent support files
- **WHEN** a consumer resolves and materializes it at repository or machine scope
- **THEN** the support bytes and executable mode MUST be installed as regular files in the target's declared per-source, per-hook support directory
- **AND** the command MUST refer to that script through the compiler's support-base API, and verified cached resolution MUST work without network access

#### Scenario: Mirrored hook support integrity

- **GIVEN** a mirrored hook with declared support files
- **WHEN** a declared file is missing, its bytes or executable mode differ, an undeclared support file is present, or a declaration names a native definition
- **THEN** resolution MUST refuse before staging or materializing the hook

#### Scenario: Mirrored hook support safe publication

- **GIVEN** hook support files in a registry repository
- **WHEN** publication reads those files
- **THEN** it MUST use bounded descriptor-based reads anchored at the acquisition root and refuse symlinks, multiply linked files, special files, and oversized files without altering external bytes

#### Scenario: Mirrored hook support revision changes

- **GIVEN** a published hook with support files
- **WHEN** a support file's bytes or executable mode change
- **THEN** publication MUST produce a new content revision without changing the old artifact

#### Scenario: Legacy mirrored script hook refusal

- **GIVEN** a legacy mirrored hook referencing an adjacent plugin-root script without packaged support files
- **WHEN** a consumer resolves the hook
- **THEN** resolution MUST fail with `marketplace.hook.support-missing` instead of reporting a healthy hook
- **AND** legacy support-free hooks without plugin-root asset references MUST remain usable

#### Scenario: Mirrored hook support materialization refusal

- **GIVEN** declared hook support is unavailable, unsafe, tampered, names a native definition, or comes from index-only acquisition
- **WHEN** materialization consumes the hook
- **THEN** it MUST refuse with `materialize.support-source.missing` before emitting the hook or support output, preserving the previous installation

#### Scenario: Mirrored hook support shell safety

- **GIVEN** an installed support path contains spaces or shell metacharacters
- **WHEN** the compiler replaces an unquoted or double-quoted plugin-root reference
- **THEN** the command MUST execute the installed script without executing path text as shell code

#### Scenario: Mirrored hook support unowned destination refusal

- **GIVEN** an installed hook support destination lacks ownership proof
- **WHEN** the same mirrored hook is selected again
- **THEN** materialization MUST refuse with `hook.support-path-unrepresentable` before updating its aggregate, even when support bytes are identical

#### Scenario: Mirrored hook support acquisition containment

- **GIVEN** a resource directory traverses a symlink beneath the acquisition root
- **WHEN** publication reads declared support
- **THEN** it MUST refuse with `marketplace.ingest.hook-support-unsafe` and leave external bytes unchanged

#### Scenario: Mirrored hook support publication diagnostic

- **GIVEN** a mirrored hook references an asset the adjacent-file policy cannot package
- **WHEN** its registry is published
- **THEN** publication MUST report `marketplace.ingest.hook-support-unavailable`, while support-free hooks remain publishable without that diagnostic

#### Scenario: Mirrored hook support unsupported reference refusal

- **GIVEN** a mirrored command references a nested directory, another plugin directory, or a dynamic plugin-root expression
- **WHEN** a consumer resolves the hook
- **THEN** it MUST refuse with `marketplace.hook.support-unsupported` and recommend retaining the Git source rather than refreshing an unsatisfiable revision

#### Scenario: Mirrored hook support stable offline refusal

- **GIVEN** verified cached archive evidence produced a deterministic missing or unsupported support refusal online
- **WHEN** the consumer resolves it offline
- **THEN** it MUST retain the same refusal code without downloading the artifact again

#### Scenario: Mirrored hook support cache independent installation

- **GIVEN** a mirrored hook has been installed with support files
- **WHEN** the Marketplace cache is deleted
- **THEN** its command MUST still execute from installed regular support files on every target and scope
- **AND** deselection MUST remove its owned support files

#### Scenario: Mirrored hook support independent sources

- **GIVEN** two mirrored sources supply hooks with the same name and different same-named support scripts
- **WHEN** both hooks are materialized
- **THEN** each command MUST execute its own installed script without a support-file collision
