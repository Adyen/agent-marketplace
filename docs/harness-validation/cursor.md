# Cursor validation

## Current status

Cursor is a supported compiler/materialization target, but it is currently **config-only** in the native compatibility workflow.

The documented `agent mcp list` surface is interactive, so it is not safe for a credential-free, noninteractive PR probe. The matrix declares Cursor as `unavailable` with an explicit reason rather than pretending that a missing command is a pass.

## Configuration coverage

The compatibility scenario still validates:

- catalog listing for every selectable resource kind
- collection expansion
- repository and user paths
- symlink and file materialization
- MCP aggregate structure and preservation
- hook and subagent output
- rule conversion and expected unsupported diagnostics
- idempotent `sync --check`

No model session, login, interactive approval, or real MCP connection is used.

## Enabling native validation later

A future Cursor probe must provide all of the following before changing the matrix from `unavailable` to `probe`:

1. A pinned Linux CLI image and version smoke test.
2. A documented noninteractive command that lists generated resources.
3. A negative control before installation.
4. Assertions on stable names and observable fields.
5. A clear distinction between configured, connected, authenticated, and interactive-only states.
6. A local fixture or no-network mode for any connection test.

Do not automate an interactive menu with terminal keystrokes as a substitute for a documented machine-readable contract.
