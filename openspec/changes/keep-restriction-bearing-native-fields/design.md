## Context

Gemini subagent inline MCP servers live only in the typed Gemini sidecar, so the sidecar is their single source of truth; no other target emits them. Windsurf hook native fields (`powershell`, `show_output`, `working_directory`) are carried in the typed Windsurf sidecar. Devin MCP compilation already removes a server whose required `command` or `url` cannot be represented, and keeps omission with a warning for individual values.

## Decisions

1. **Gemini keeps the whole inline server.** Unmodeled members are preserved as an opaque member map in the sidecar and re-emitted by the Gemini compiler. This is a same-harness round trip, so the server keeps exactly the authority the source granted. Preserved members never override `command`, `args`, `env` or `url`, including case variants.
2. **Windsurf reads the documented key.** `working_directory` is the documented member. Because it changes where a relative command resolves, hook resolution refuses it on every non-Windsurf target with the existing `hook.windsurf.native-fields-unsupported` code instead of dropping it.
3. **Devin treats arguments as required.** Removing one argument changes the command line, so an unrepresentable argument removes the entry with the existing entry-level `mcp.devin.interpolation-unrepresentable` error. Environment and header values keep their existing value-level warning.

## Non-goals

Other Windsurf native fields on other targets, Gemini inline servers on other targets (they are not emitted there), and Devin environment and header omission.

## Verification

Public `Convert` regressions fail before the change: Gemini→Gemini drops `includeTools`, `excludeTools` and `timeout`; Windsurf→Windsurf drops `working_directory`; Windsurf→Claude emits the hook; Devin emits the server without the argument. Positive controls keep modeled inline server members and Windsurf hooks without a working directory.
