## Why

Three native fields that restrict or locate what a resource runs were dropped with no error:

- Gemini ingestion kept only `command`, `args`, `env` and `url` of a subagent's inline MCP server. A Gemini→Gemini conversion dropped `includeTools` and `excludeTools`, so the server exposed every tool.
- Windsurf ingestion read an undocumented `working_dir` key, so the documented `working_directory` landed in extension fields and compilation dropped it. A Windsurf→Windsurf hook then ran its relative command from the workspace root instead of the reviewed directory.
- Devin MCP compilation dropped a single argument it could not represent, such as `--sandbox=${file:…}`, and still emitted the server without it.

## What Changes

- Gemini ingestion preserves every inline MCP server member; Gemini compilation emits the unmodeled members unchanged and never lets them override modeled ones.
- Windsurf ingestion reads `working_directory`; Windsurf compilation emits it, and other targets refuse the hook with `hook.windsurf.native-fields-unsupported`.
- Devin MCP compilation refuses a server with an unrepresentable argument with `mcp.devin.interpolation-unrepresentable`.

## Capabilities

### Modified Capabilities

- `harness-compilation`: restriction-bearing native fields are preserved or refused.

## Impact

Gemini and Windsurf ingestion, the Gemini subagent compiler, hook resolution and Devin MCP compilation in `pkg/harness-compilation`. Windsurf hooks with a working directory no longer convert to other harnesses, and Devin no longer installs a server whose argument list it would have shortened. No fixture executes a resource.
