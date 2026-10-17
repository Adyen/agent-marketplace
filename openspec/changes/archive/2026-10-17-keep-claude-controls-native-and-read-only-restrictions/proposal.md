## Why

Two conversion gaps found while reconciling R4 (#433):

- **#635.** Claude reads subagent `permissionMode`, `mcpServers` and `hooks`, and skill and command `hooks`, as active execution controls. These keys passed through from any source as extension fields, so a Cursor or Factory subagent declaring `permissionMode: bypassPermissions`, inline MCP servers or hooks gained them as live Claude settings with no diagnostic. This is the mechanism #248 closed for Gemini and Windsurf.
- **#636.** Documented read-only restrictions (Cursor `readonly: true`, Claude `permissionMode: plan`) were extension fields that other targets ignore, so a converted read-only reviewer ran with the target's default tools. Typed restrictions such as `disallowedTools` already fail closed.

## What Changes

- Claude ingestion records Claude execution controls in a new Claude sidecar field, `nativeControls`. Claude compilation emits them only from there, and omits the same keys from any other source with the new warning `compile.claude.native-control-omitted`.
- A read-only subagent maps to Claude `permissionMode: plan`, Cursor `readonly: true`, Codex `sandbox_mode = "read-only"` or Factory `tools: read-only`. Other targets refuse it. The restriction counts whether typed or carried as an extension field.
- Factory ingestion records the `read-only` tools category in a new Factory sidecar field instead of treating it as a tool name.

## Capabilities

### Modified Capabilities

- `harness-compilation`: Claude-native controls require Claude provenance; read-only subagent restrictions are mapped or refused.

## Impact

Claude and Factory ingestion; the Claude, Cursor, Codex, Factory, Gemini, Devin and OpenCode compilers; two sidecar fields; one warning code; the frozen source-revision fallback projection; the field matrix and changelog. Maintainer decisions are recorded on #635 and #636.
