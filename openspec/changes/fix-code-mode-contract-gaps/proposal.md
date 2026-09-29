## Why

Exercising code mode against a real catalog showed that it does not meet its own contract. Declared MCP servers that authenticate through `${VAR}` references in headers receive the literal placeholder, so every credentialed Marketplace server fails with HTTP 401. Several script-visible values use Go struct field names (`Code`, `Message`, `Rule`) instead of the advertised JSON fields. Declaring one ref twice renames its alias. Catalog refs disagree with their own `registry` field and are named after directory basenames rather than configured source names. Oversized results return about 1 MiB of prefix, which floods the calling agent's context instead of bounding it.

## What Changes

- Expand `${VAR}` and `${env:VAR}` references in a declared server's command, args, env values, URL, and headers from the host environment before it starts. Unset or unsupported references fail `mcp.start-failed` without sending a placeholder. A remote server that rejects startup reports `mcp.start-failed` instead of `mcp.tool-error`.
- Treat a ref repeated in `servers` as one declaration bound under its normal alias.
- Throw JavaScript `Error` instances with lowercase `code` and `message` into scripts. **BREAKING** for scripts that read `e.Code` or `e.Message`.
- Return catalog entries, findings, and `skills.load` results with their documented lowercase fields.
- Make every catalog ref's prefix equal its `registry` field, and label v4 Git and directory sources by their configured source name. **BREAKING** for scripts that hard-code current `local:<directory>` refs; configuration files are unaffected, and refs already recorded in session logs stay promotable.
- Replace oversized results with a marker carrying the original serialized size and at most a 4 KiB prefix.
- Document the result, finding, and error shapes in the `search` and `execute` tool descriptions.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `code-mode-mcp`: server definition interpolation and startup failure codes, repeated declared refs, script error and value shapes, catalog ref identity, and truncation markers.

## Impact

Code-mode snapshot labelling, script bindings, VM error mapping and truncation, the MCP client pool's server start path, the v4 source-to-registry projection (to carry the source name), the code-mode tool descriptions, the `code-mode` bootstrap skill, and focused, command, and stdio E2E tests. Materialization, harness compilation, and `promote` routing do not change.
