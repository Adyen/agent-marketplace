## MODIFIED Requirements

### Requirement: Read-only subagent restrictions are mapped or refused

A subagent is read-only when it declares Cursor `readonly: true`, Claude `permissionMode: plan`, Codex `sandbox_mode = "read-only"` or Factory `tools: read-only`, whether recorded in a typed sidecar or carried as an extension field. A native Codex read-only sandbox MUST reach these mappings instead of being refused as an unrepresentable Codex sidecar on targets with a read-only mode. Compilation MUST keep a read-only subagent read-only: Claude MUST emit `permissionMode: plan`, Cursor MUST emit `readonly: true`, Codex MUST emit `sandbox_mode = "read-only"`, and Factory MUST emit `tools: read-only` when the droid has no explicit tool list. Factory with an explicit tool list, Gemini, Devin and Windsurf MUST suppress the subagent with the error `subagent.security-restriction-unsupported`, and OpenCode MUST suppress it with its existing restriction error `subagent.opencode.restriction-unsupported`.

#### Scenario: Read-only subagents map to documented read-only modes

- **WHEN** a Cursor subagent with `readonly: true` is converted to Claude, Cursor, Codex and Factory, a Claude subagent with `permissionMode: plan` is converted to Cursor, a Factory droid with `tools: read-only` is converted to Factory and Claude, and a Codex agent with `sandbox_mode = "read-only"` is converted to Claude, Cursor and Factory
- **THEN** Claude MUST emit `permissionMode: plan`, Cursor MUST emit `readonly: true`, Codex MUST emit `sandbox_mode = "read-only"`, and Factory MUST emit `tools: read-only`

#### Scenario: Targets without a read-only mode refuse

- **WHEN** a Cursor subagent with `readonly: true` is converted to Gemini, Devin and OpenCode, or a Cursor subagent with `readonly: true` and an explicit tool list is converted to Factory
- **THEN** no subagent artifact MUST be emitted, and compilation MUST report the error `subagent.security-restriction-unsupported` (`subagent.opencode.restriction-unsupported` on OpenCode)

### Requirement: Artifact paths that differ only in letter case collide

Bundle compilation MUST treat two non-fragment artifact paths that differ only in letter case, compared by Unicode simple case folding, as the same output path, because they name one file on case-insensitive filesystems. Both producing resources MUST be suppressed with the error `bundle.conflict.duplicate-artifact-path` (or `bundle.conflict.cross-kind-artifact-path` for different kinds), naming both paths, and every other resource in the bundle MUST still be emitted. Installation MUST apply the same rule to selected skills, commands, subagents or rules of one kind, for the same target and scope, whose installed names differ only in letter case: each of them MUST be refused with `bundle.conflict.duplicate-artifact-path` before any write, and the rest of the target MUST still install. The rule MUST hold on every host filesystem, so output stays portable between case-sensitive and case-insensitive systems.

#### Scenario: Case-only name collisions are refused portably

- **WHEN** a Codex bundle with agents named `Foo` and `foo` and an agent named `bar` is converted to Codex
- **THEN** neither `Foo` nor `foo` MUST be emitted, compilation MUST report `bundle.conflict.duplicate-artifact-path` naming both paths, and `bar` MUST be emitted

#### Scenario: Case-only name collisions are refused on install

- **WHEN** `sync` installs a directory source whose Codex agents are named `Foo`, `foo` and `bar` to Codex
- **THEN** neither `Foo` nor `foo` MUST be installed, `sync` MUST report `bundle.conflict.duplicate-artifact-path` naming both, and `bar` MUST be installed

#### Scenario: Unicode case-folded paths collide

- **WHEN** two resources produce artifact paths that differ only by the Greek sigma `σ` and final sigma `ς`
- **THEN** neither artifact MUST be emitted, and compilation MUST report `bundle.conflict.duplicate-artifact-path` naming both paths
