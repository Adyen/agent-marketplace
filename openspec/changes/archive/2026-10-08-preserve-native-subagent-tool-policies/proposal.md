## Why

Same-harness compilation rejects native subagent tool identifiers that are absent from the cross-harness mapping table. A Factory droid that names `LS`, `FetchUrl`, `ToolSearch`, or a registered MCP tool is suppressed when compiled back to Factory (#599), and removing the tool list to get past the error broadens its permissions. Cursor and Gemini subagents have the same defect.

## What Changes

- Preserve explicit native subagent tool lists through Factory, Cursor, and Gemini same-harness ingestion and compilation, including exact MCP identifiers, ordering, and explicit empty lists.
- Keep cross-harness conversion unchanged: a native list is used only by its own harness, and canonical tools still need a supported translation or an explicit invocation mapping.
- Keep Claude and Devin subagent compilation, diagnostic codes and severities, and materialization unchanged.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `harness-compilation`: faithful native subagent tool policies for same-harness output.

## Impact

Factory, Cursor, and Gemini subagent ingestion, their typed native sidecars and compilers, and conversion and CLI regression tests. No configuration, acquisition-lock, ownership-state, or diagnostic change. These checks prove generated policy preservation, not native runtime enforcement.
