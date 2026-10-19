# Cursor validation

[Shared validation contract](README.md)

## Native installation and image

The fixture materializes resources below `<repo>/.cursor` or the isolated
HOME's `.cursor`. Hooks live in `hooks.json`, whose root has `version: 1` and a
`hooks` member. MCP servers are declared in `mcp.json`.

The image resolver reads Cursor's public installer as data and extracts the
exact calendar-versioned release and the Linux/x64 bundle URL. It never
executes the installer script, and it allows only the exact public download
URL for that release. The image records the downloaded bundle's digest as
provenance only, not as a signed checksum from Cursor. The offline smoke check
must report the pinned version.

## MCP discovery and tool metadata

The driver runs:

```text
agent --version
agent mcp list
agent mcp list-tools <fixture-server>
```

Cursor's documentation describes an interactive MCP menu, yet the pinned
CLI's non-TTY listing can enumerate configured servers. The test checks the
actual behavior rather than excluding the harness on the strength of that
description.

Before installation, both names must be absent from `mcp list`, and after
installation they must appear. For each server, `list-tools` must return
`amcompat_echo`, the tool that the local stdio fixture exports. The driver
invokes no tool and sends no model prompt. Enumerating that local tool
successfully is recorded separately from configuration recognition.

## Repository approval

Project MCP definitions require explicit approval. The driver first calls
`list-tools` and requires the known unapproved-server rejection; an arbitrary
nonzero exit does not qualify. It then runs:

```text
agent mcp enable <fixture-server>
```

Only the two fixture names may be approved, and every command uses the
validated disposable HOME and workspace. The second server must stay
unapproved until its own enable call. User-scope fixtures skip this project
approval step. Neither the application nor the test grants broad MCP
approval, and a final `sync --check` detects materialization drift.

## Hooks

The native sync file is `.cursor/hooks.json` in either scope. Its `hooks`
member maps each event directly to handler entries, with an optional `matcher`
field on the handler. Hook support files live under
`.cursor/hook-support/<source>/<hook>/`. The fixture checks this file and its
support bytes without claiming that Cursor fired a handler. The shipped
catalog declares command and prompt handlers for every Cursor event.

Cursor also loads Claude hook files. When a sync selects both targets for the
same scope, the shared coverage expects `hook.duplicate-execution-risk` while
leaving both hook outputs intact. A marked entry in legacy
`.cursor/hooks/hooks.json` may migrate to the native file; handwritten entries
are preserved.

## Remaining limits

- Skills and commands: session discovery is separate from the MCP CLI. Cursor
  ACP exposes a session protocol, but its documented flow requires
  authentication before session metadata, and the driver supplies no dummy
  credentials or prompts to work around that.
- Subagents: the inspected CLI interface has no documented standalone
  custom-definition inventory. Running or listing cloud or background agents
  does not validate a generated definition. A subagent with an explicit tool
  restriction Cursor cannot represent fails closed with
  `subagent.cursor.tool-unknown`; adding a target mapping does not create a
  Cursor tool-list field that the format lacks.
- Hooks and rules: the inspected commands expose no standalone resource
  inventory. Their generated files and supported conversion behavior are still
  checked, without any claim about Agent execution.
- Collections: virtual selections whose members are checked individually.

References: [MCP CLI](https://cursor.com/docs/cli/mcp), [ACP lifecycle and authentication](https://cursor.com/docs/cli/acp).
