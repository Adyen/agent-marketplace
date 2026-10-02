# materialization Specification

## Purpose
Defines how selected resources appear in harness targets, including stable
commands in shared hook files across repository and machine install scopes.

## Requirements

### Requirement: Machine-scope hook commands are location-independent

Agent Marketplace SHALL expand `${DROID_PLUGIN_ROOT}` in a machine-scope
Factory hook command to the absolute machine harness target root. Its
materialized command bytes MUST NOT depend on whether sync runs from the
user's home or a repository. Repository-scope Factory hook commands SHALL use
the relative repository harness target root.

#### Scenario: Machine-scope hook command uses the absolute machine root

- **WHEN** a user-selected Factory hook command contains `${DROID_PLUGIN_ROOT}` and sync runs from the user's home or from a repository
- **THEN** the machine-scope hook file MUST contain the same command with the absolute machine root in both cases
- **AND** `sync --check` MUST succeed from either location after synchronization

#### Scenario: Repository-scope hook command stays repository-relative

- **WHEN** a repository-selected Factory hook command contains `${DROID_PLUGIN_ROOT}`
- **THEN** the repository-scope hook file MUST contain a command rooted at the relative `.factory` path

### Requirement: Harness target roots remain within their authorized scope

Agent Marketplace SHALL admit every selected harness target root before materialization writes below that root. Repository roots MUST remain within the repository scope. Explicit user-owned machine directory aliases MAY retain their existing relocation behavior. An explicitly configured absolute CODEX_HOME MAY relocate the Codex configuration partition without relocating its skills partition. Repository and HOME scope anchors MAY be filesystem aliases. A contained repository directory-root alias MAY be honored, but an escaping repository root, dangling or cyclic root alias, or non-directory root MUST be refused without changing the root object or any external target bytes, permissions or modification time. Refusal MUST carry `materialize.destination.error`. Independent safe non-Codex targets MAY continue; Codex destination preflight MUST refuse before destination mutation when any active partition root is unsafe.

#### Scenario: Unsafe harness root preserves external state

- **WHEN** a selected repository harness target root is an escaping link or a selected harness root is a dangling link, cyclic link or non-directory object
- **THEN** materialization MUST report `materialize.destination.error` and preserve the root object and external state

#### Scenario: Contained root and scope aliases remain usable

- **WHEN** a repository or HOME is a filesystem alias and a harness root is a real directory or a contained directory alias
- **THEN** materialization MUST install selected resources at their native paths in both supported materialization modes without replacing the aliases

### Requirement: Output-directory ancestors below a harness root are real directories

Agent Marketplace MUST reject unsafe symlinked or non-directory output-directory ancestors below an admitted harness target root before writing that target's planned output. The established Factory `skills -> ../.agents/skills` legacy migration MAY replace that exact link without following or changing its target. Missing output directories MAY be created under the authorized root. Refusal MUST report `materialize.destination.error`, preserve the unsafe ancestor and external state, and MUST NOT leave attempt-owned staging residue. Read-only checking MUST validate these ancestors without creating output directories.

#### Scenario: Unsafe output ancestor refuses before target writes

- **WHEN** a selected resource's output-directory ancestor below its harness root is a link or non-directory object
- **THEN** materialization MUST report `materialize.destination.error` before target output changes and preserve the unsafe ancestor and external state

#### Scenario: Read-only root admission creates no output

- **WHEN** read-only materialization checks a safe but missing harness root or output-directory ancestor
- **THEN** it MUST report drift without creating output directories

### Requirement: Diagnostic root summaries remain available on boundary refusal

Doctor and status native destination summaries MUST render a typed filesystem-boundary refusal as an unresolvable root while retaining independent target information. Diagnostic-only doctor MUST NOT read a non-directory root as aggregate payload or change it. Structural configuration failures such as relative CODEX_HOME MUST retain their existing hard-error behavior, and repair MUST still refuse unsafe destination roots before aggregate work.

#### Scenario: Doctor continues diagnosing an unsafe native root

- **WHEN** diagnostic-only doctor encounters a non-directory Codex root
- **THEN** it MUST report an unresolvable root, retain other target information, and preserve the root's bytes and metadata without displaying its payload

### Requirement: Cache-backed link publication retains destination authority

Agent Marketplace MUST bind cache-backed symlink staging, publication and attempt-owned cleanup to the admitted destination directory. A parent or leaf whose observed generation changes before publication MUST be refused without replacing the new object or leaving attempt-owned staging in the captured or replacement tree. Refusal MUST report `materialize.write.error`. Existing user-owned collisions MUST remain preserved, and an unchanged managed link MUST remain a no-op. Existing file-mode output MAY migrate back to a cache-backed link only under its established ownership policy.

#### Scenario: Cache link publication refuses a replaced parent

- **WHEN** a cache-backed link's destination parent is replaced after staging and before publication
- **THEN** publication MUST refuse, preserve replacement content and remove only its own staged entry from the captured directory

#### Scenario: Cache link publication refuses a replaced leaf

- **WHEN** a cache-backed link's destination leaf is replaced after staging and before publication
- **THEN** publication MUST refuse and preserve the replacement leaf and the cache target

#### Scenario: Managed cache links update and repeat without mutation

- **WHEN** a managed cache link requires a new target and later repeats the same desired target
- **THEN** the update MUST publish at the native destination without changing cache payload bytes, and the repeat MUST preserve link identity and modification time

### Requirement: Marker alias payload reads retain scoped authority

Agent Marketplace MUST admit marker-merge JSON aliases before reading payload bytes, for mutating and read-only operations. An effective target outside repository/HOME scope, the admitted machine output directory and the trusted cache root MUST be refused without copying foreign bytes into output or changing the alias or target. Accepted targets MUST be regular and singly linked, and their data and permissions MUST describe one unchanged captured generation. Dangling, cyclic, special or multiply linked targets MUST refuse. Contained aliases and legacy cache migrations MAY detach into regular native output while preserving handwritten content and original target state. Refusal MUST report `materialize.write.error`. This contract does not add an absolute input-size or aggregate-work budget.

#### Scenario: Marker merge refuses an external alias before reading

- **WHEN** a repository marker aggregate aliases a foreign regular JSON file outside admitted scope and cache
- **THEN** materialization MUST refuse before importing its payload and preserve the alias and original target bytes and metadata

#### Scenario: Contained marker aliases preserve native content and no-ops

- **WHEN** an accepted contained marker alias contributes handwritten JSON to a desired native MCP aggregate
- **THEN** the desired and handwritten entries and existing permissions MUST be preserved in a regular output, the original target MUST remain unchanged, and repeat sync MUST preserve output identity and modification time

#### Scenario: Read-only marker merge refuses an external alias

- **WHEN** read-only materialization encounters an external marker aggregate alias
- **THEN** checking MUST refuse without reading its payload or changing the alias

### Requirement: Skill support payload reads stay within captured source authority

Agent Marketplace MUST enumerate skill cache support sources through captured directory handles and authorize each opened source payload against its captured source root before reading. Payloads MUST be regular singly-linked snapshots with unchanged identity and size, supplying bytes and permissions from the same generation. Contained relative file aliases MUST remain usable. Escaping aliases and unsafe payload shapes MUST refuse without importing external bytes. Applicable captured-size growth checks do not introduce a new absolute source or aggregate-work budget.

#### Scenario: Support copy refuses a source link outside its root

- **WHEN** a native skill source support file is an alias outside the authorized source root
- **THEN** cache preparation MUST report its stable write diagnostic before native skill publication and preserve external bytes and identity/mode/mtime

#### Scenario: Support copy keeps contained relative alias content and mode

- **WHEN** a native skill source has a relative file alias to regular executable support data within its captured root
- **THEN** actual native installation and repeat sync MUST retain intended support bytes/mode and leave the original source unchanged

#### Scenario: Support source refusal preserves an installed cache generation

- **WHEN** an installed native skill update changes compiled content but its support source refuses admission
- **THEN** cache preparation MUST preserve installed compiled/support bytes and publish no refused support payload, cleaning only attempt-owned staging
