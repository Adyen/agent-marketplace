## ADDED Requirements

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
