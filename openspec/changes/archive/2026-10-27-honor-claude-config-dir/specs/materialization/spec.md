## MODIFIED Requirements

### Requirement: Harness target roots remain within their authorized scope

Agent Marketplace SHALL admit every selected harness target root before materialization writes below that root. Repository roots MUST remain within the repository scope. Explicit user-owned machine directory aliases MAY retain their existing relocation behavior. An explicitly configured absolute CODEX_HOME MAY relocate the Codex configuration partition without relocating its skills partition. An explicitly configured absolute CLAUDE_CONFIG_DIR MAY relocate the whole Claude Code machine root, including its machine MCP configuration file, inside or outside HOME. Repository and HOME scope anchors MAY be filesystem aliases. A contained repository directory-root alias MAY be honored, but an escaping repository root, dangling or cyclic root alias, or non-directory root MUST be refused without changing the root object or any external target bytes, permissions or modification time. Refusal MUST carry `materialize.destination.error`. Independent safe non-Codex targets MAY continue; Codex destination preflight MUST refuse before destination mutation when any active partition root is unsafe.

#### Scenario: Unsafe harness root preserves external state

- **WHEN** a selected repository harness target root is an escaping link or a selected harness root is a dangling link, cyclic link or non-directory object
- **THEN** materialization MUST report `materialize.destination.error` and preserve the root object and external state

#### Scenario: Contained root and scope aliases remain usable

- **WHEN** a repository or HOME is a filesystem alias and a harness root is a real directory or a contained directory alias
- **THEN** materialization MUST install selected resources at their native paths in both supported materialization modes without replacing the aliases

#### Scenario: Relocated Claude Code machine root is admitted

- **WHEN** CLAUDE_CONFIG_DIR is an absolute path inside or outside HOME, whether the directory is missing or a real directory, and machine-scope selections include Claude Code resources
- **THEN** materialization MUST admit that root, create it when missing, and install the selected resources under it in both supported materialization modes without an error diagnostic

#### Scenario: Unsafe relocated Claude Code machine root is refused

- **WHEN** CLAUDE_CONFIG_DIR names a dangling link, a cyclic link, or a non-directory object and machine-scope selections include Claude Code resources
- **THEN** materialization MUST report `materialize.destination.error` and preserve that object, `~/.claude`, and `~/.claude.json` unchanged

### Requirement: Diagnostic root summaries remain available on boundary refusal

Doctor and status native destination summaries MUST render a typed filesystem-boundary refusal as an unresolvable root while retaining independent target information. Diagnostic-only doctor MUST NOT read a non-directory root as aggregate payload or change it. Structural configuration failures such as relative CODEX_HOME or relative CLAUDE_CONFIG_DIR MUST retain their existing hard-error behavior, and repair MUST still refuse unsafe destination roots before aggregate work.

#### Scenario: Doctor continues diagnosing an unsafe native root

- **WHEN** diagnostic-only doctor encounters a non-directory Codex root
- **THEN** it MUST report an unresolvable root, retain other target information, and preserve the root's bytes and metadata without displaying its payload

#### Scenario: Relative Claude configuration directory is a hard error in summaries

- **WHEN** CLAUDE_CONFIG_DIR is a relative path and Claude Code is a configured harness target
- **THEN** `status` and `doctor` MUST exit nonzero with an error naming `CLAUDE_CONFIG_DIR` as required to be absolute instead of rendering a Claude Code root summary
