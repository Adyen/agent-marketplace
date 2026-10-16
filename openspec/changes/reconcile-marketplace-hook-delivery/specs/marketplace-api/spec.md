## MODIFIED Requirements

### Requirement: Mirrored hooks carry integrity-bound support files

Mirror acquisition MUST package flat adjacent support files selected by the
source-layout hook policy, excluding native aggregate definitions, at their
plugin-relative `hooks/` paths. Their relative paths, bytes, and executable modes
MUST be bound into the canonical hook content revision. Consumers MUST verify
declaration membership and integrity before installation and on the captured
bytes used for materialization. Support changes MUST produce new content
revisions, without rewriting existing content. Nested source directories and
other plugin directories remain unsupported and MUST have an achievable remedy.

#### Scenario: Mirrored hook support installation

- **GIVEN** a mirrored hook references an adjacent plugin-root script
- **WHEN** a consumer resolves and materializes it at repository or machine scope
- **THEN** its support bytes and executable mode MUST be installed in the target's per-source, per-hook support directory using the configured native installation mode
- **AND** the command MUST refer to the installed script, and verified cached resolution MUST work offline

#### Scenario: Mirrored hook support integrity

- **GIVEN** a mirrored hook declares support files
- **WHEN** a declared file is missing, bytes or executable mode differ, an undeclared file is present, or a declaration names a native definition
- **THEN** resolution MUST refuse before staging or materializing the hook

#### Scenario: Canonical null extra is not a native extension

- **GIVEN** a canonical hook contains the serializer's null `Extra` field
- **WHEN** it is decoded for target compilation
- **THEN** that null field MUST NOT become unknown native metadata or cause an otherwise supported hook to be refused
- **AND** actual unknown fields MUST retain their existing preservation and target-refusal rules

#### Scenario: Mirrored hook support safe publication

- **GIVEN** adjacent hook support files in a registry repository
- **WHEN** publication reads those files
- **THEN** reads MUST remain bounded within the acquisition root and refuse symlinks, multiply linked files, special files, and oversized files without altering external bytes

#### Scenario: Mirrored hook support revision changes

- **GIVEN** a published hook with support files
- **WHEN** support bytes or executable mode change
- **THEN** publication MUST produce a new content revision without changing the old artifact

#### Scenario: Legacy mirrored script hook refusal

- **GIVEN** a legacy mirrored hook references an adjacent plugin-root script without packaged support
- **WHEN** a consumer resolves the hook
- **THEN** resolution MUST fail with `marketplace.hook.support-missing`
- **AND** support-free hooks without plugin-root asset references MUST remain usable

#### Scenario: Mirrored hook support materialization refusal

- **GIVEN** declared hook support is unavailable, tampered, names a native definition, or comes from index-only acquisition
- **WHEN** materialization consumes the resolved resource
- **THEN** it MUST refuse with `materialize.compile.error` and `materialize.hook.support-refused` before emitting the hook or support output, preserving the previous installation

#### Scenario: Mirrored hook support shell safety

- **GIVEN** an installed support path contains spaces or shell metacharacters
- **WHEN** its plugin-root reference is translated, including before, after, or inside nested command substitutions
- **THEN** the installed command MUST execute that literal script without executing path text as shell substitution

#### Scenario: Mirrored hook support unowned destination refusal

- **GIVEN** an installed hook support destination lacks both manifest ownership and durable native support proof
- **WHEN** the mirrored hook is materialized again
- **THEN** materialization MUST refuse with `materialize.hook.support-conflict` before updating its aggregate, even when support bytes are identical

#### Scenario: Mirrored hook support acquisition containment

- **GIVEN** a resource directory has a symlinked ancestor below the acquisition root
- **WHEN** publication reads declared support
- **THEN** it MUST refuse with `marketplace.ingest.hook-support-unsafe` and leave external bytes unchanged

#### Scenario: Mirrored hook support publication diagnostic

- **GIVEN** a hook references assets outside the flat adjacent-file policy
- **WHEN** the Marketplace publishes its revision
- **THEN** publication MUST report `marketplace.ingest.hook-support-unavailable`, while support-free hooks remain publishable without that diagnostic

#### Scenario: Mirrored hook support unsupported reference refusal

- **GIVEN** a mirrored command references a nested source directory, another plugin directory, or a dynamic plugin-root expression
- **WHEN** a consumer resolves the hook
- **THEN** it MUST refuse with `marketplace.hook.support-unsupported` and recommend retaining the Git source

#### Scenario: Mirrored hook support stable offline refusal

- **GIVEN** verified cached evidence produced a deterministic missing or unsupported support refusal online
- **WHEN** the consumer resolves it offline
- **THEN** it MUST retain the same refusal code without downloading the artifact again

#### Scenario: Mirrored hook support cache independent installation

- **GIVEN** a mirrored hook is installed with support files
- **WHEN** the Marketplace acquisition cache is deleted
- **THEN** its command MUST still execute from installed support on every target and scope
- **AND** deselection MUST remove its owned support files

#### Scenario: Mirrored hook support independent sources

- **GIVEN** two mirrored sources supply same-named hooks with different same-named scripts
- **WHEN** both hooks are materialized
- **THEN** each command MUST execute its own installed script without a collision

#### Scenario: Unsafe hook support does not block valid resources

- **GIVEN** a registry contains a valid skill and a hook with an unsafe adjacent file, even one not directly referenced by its command
- **WHEN** the Marketplace publishes the registry
- **THEN** it MUST skip the affected hook with `marketplace.ingest.hook-support-unavailable` and publish the valid skill without reading external bytes
- **AND** collections requiring the skipped hook MUST be withheld without blocking unrelated resources

#### Scenario: Mirrored hook support drift

- **GIVEN** an installed mirrored hook loses its owned support script
- **WHEN** `sync --check` runs
- **THEN** it MUST report drift with a nonzero exit without recreating the script
- **AND** a subsequent sync MUST restore the script and successful execution
