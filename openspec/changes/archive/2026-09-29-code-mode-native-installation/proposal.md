## Why

Code-mode registration currently uses its own config switch and commands even though Agent Marketplace already installs collections, skills, and MCP servers. Users should select code mode through the same resource installation and removal workflow as other capabilities.

## What Changes

- Publish a first-party Git-registry collection containing a bootstrap skill and an MCP server definition that runs the installed `agent-marketplace mcp` binary.
- Select, synchronize, and remove both resources with ordinary collection operations and ownership-safe reconciliation. Default installations select neither resource.
- **BREAKING:** Remove `codeMode` config, `mcp enable`/`mcp disable`, and `init --code-mode`. Reject legacy occurrences with a diagnostic that directs users to remove the key and install the collection.
- Keep `agent-marketplace mcp` as the stdio runtime, retaining its two-tool execution contract.
- Update consumer documentation and behavioral test witnesses.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `code-mode-mcp`: replace special opt-in registration with first-party collection installation, ownership-safe removal, default-off selection, and migration diagnostics.

## Impact

First-party `skills/` registry, CLI command/help and init, config readers and merge, sync pipeline, command and E2E tests, OpenSpec, and CLI/setup reference. Hosted Marketplace publication and code-mode execution semantics are unchanged.
