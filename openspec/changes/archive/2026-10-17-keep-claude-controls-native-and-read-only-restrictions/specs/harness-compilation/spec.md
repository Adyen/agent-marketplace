## ADDED Requirements

### Requirement: Claude-native controls require Claude provenance

Claude reads subagent `permissionMode`, `mcpServers` and `hooks`, and skill and command `hooks`, as active execution controls. Claude ingestion MUST record these keys in the resource's Claude sidecar, and Claude compilation MUST emit them only from that sidecar. The same keys arriving as extension fields from any other source MUST be omitted on Claude with the warning `compile.claude.native-control-omitted`, and the resource MUST still be emitted. Claude→Claude output MUST be unchanged, directly and through a Marketplace resource payload.

#### Scenario: Foreign controls are omitted on Claude

- **WHEN** Cursor and Factory subagents declaring `permissionMode: bypassPermissions`, `mcpServers` and `hooks`, and a Factory skill and command declaring `hooks`, are converted to Claude
- **THEN** the emitted Claude resources MUST contain none of these keys, and compilation MUST report the warning `compile.claude.native-control-omitted`

#### Scenario: Claude native controls round-trip

- **WHEN** a Claude subagent declaring `permissionMode`, `mcpServers` and `hooks`, and a Claude skill and command declaring `hooks`, are converted to Claude directly and through a Marketplace resource payload
- **THEN** the emitted resources MUST keep every key unchanged, with no `compile.claude.native-control-omitted` warning

### Requirement: Read-only subagent restrictions are mapped or refused

A subagent is read-only when it declares Cursor `readonly: true`, Claude `permissionMode: plan`, Codex `sandbox_mode = "read-only"` or Factory `tools: read-only`, whether recorded in a typed sidecar or carried as an extension field. Compilation MUST keep a read-only subagent read-only: Claude MUST emit `permissionMode: plan`, Cursor MUST emit `readonly: true`, Codex MUST emit `sandbox_mode = "read-only"`, and Factory MUST emit `tools: read-only` when the droid has no explicit tool list. Factory with an explicit tool list, Gemini, Devin and Windsurf MUST suppress the subagent with the error `subagent.security-restriction-unsupported`, and OpenCode MUST suppress it with its existing restriction error `subagent.opencode.restriction-unsupported`.

#### Scenario: Read-only subagents map to documented read-only modes

- **WHEN** a Cursor subagent with `readonly: true` is converted to Claude, Cursor, Codex and Factory, a Claude subagent with `permissionMode: plan` is converted to Cursor, and a Factory droid with `tools: read-only` is converted to Factory and Claude
- **THEN** Claude MUST emit `permissionMode: plan`, Cursor MUST emit `readonly: true`, Codex MUST emit `sandbox_mode = "read-only"`, and Factory MUST emit `tools: read-only`

#### Scenario: Targets without a read-only mode refuse

- **WHEN** a Cursor subagent with `readonly: true` is converted to Gemini, Devin and OpenCode, or a Cursor subagent with `readonly: true` and an explicit tool list is converted to Factory
- **THEN** no subagent artifact MUST be emitted, and compilation MUST report the error `subagent.security-restriction-unsupported` (`subagent.opencode.restriction-unsupported` on OpenCode)
