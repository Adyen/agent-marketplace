# Cursor validation

[Shared validation contract](README.md)

## Native installation and image

The fixture materializes resources below `<repo>/.cursor` or the isolated HOME's `.cursor`. MCP servers are declared in `mcp.json`.

The image resolver parses Cursor's public installer **as data**, extracting the exact calendar-versioned release and the Linux/x64 bundle URL. It does not execute the installer script. Only the exact public download URL for that release is allowed. The image records the downloaded bundle's digest; this is provenance, not a claim that Cursor supplied a signed checksum. The offline smoke check must report the pinned version.

## MCP discovery and tool metadata

The driver runs:

```text
agent --version
agent mcp list
agent mcp list-tools <fixture-server>
```

Although the documentation describes an interactive MCP menu, the pinned CLI's non-TTY listing can enumerate configured servers. The test checks actual behavior instead of excluding the harness based on that description alone.

Before installation, both names must be absent from `mcp list`. After installation they must appear. For each server, `list-tools` must return `amcompat_echo`, the tool exported by the local stdio fixture. No tool invocation or model prompt is sent. Successful enumeration of that local tool is recorded separately from configuration recognition.

## Repository approval

Project MCP definitions require explicit approval. The driver first calls `list-tools` and requires the known unapproved-server rejection, not an arbitrary nonzero exit. It then runs:

```text
agent mcp enable <fixture-server>
```

Only the two fixture names may be approved, and all commands use the validated disposable HOME/workspace. The second server must remain unapproved until its own enable call. User-scope fixtures do not use this project approval transition. Neither the application nor the test grants broad MCP approval, and a final `sync --check` detects materialization drift.

## Remaining limits

- **Skills and commands:** session discovery is separate from the MCP CLI. Cursor ACP exposes a session protocol, but its documented flow requires authentication before session metadata; no dummy credentials or prompts are supplied to work around that.
- **Subagents:** there is no documented standalone custom-definition inventory in the inspected CLI interface. Running or listing cloud/background agents is not validation of a generated definition.
- **Hooks and rules:** no standalone resource inventory is exposed by the inspected commands. Their generated files and supported conversion behavior remain checked without claiming Cascade/Agent execution.
- **Collections:** virtual selections whose members are checked individually.

References: [MCP CLI](https://cursor.com/docs/cli/mcp), [ACP lifecycle and authentication](https://cursor.com/docs/cli/acp).
