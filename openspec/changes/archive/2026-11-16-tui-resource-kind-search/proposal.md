# Proposal

## Why

Searching by resource type in the TUI does not reliably discover Marketplace resources whose names and descriptions lack that type word. This applies to every resource type: `mcp` must find all MCP servers and `command` must find all commands without requiring users to know their names.

## What Changes

- Match every supported resource kind in TUI search for both configured and uninstalled Marketplace resources, not only MCP servers.
- Treat `/` as the existing search shortcut: `/mcp` means open search and type `mcp`, not a new query language.
- Retrieve every matching Marketplace resource for a kind query, including matches beyond the first result page.
- Preserve ordinary text matching, ranking, grouping, selection, and read-only discovery.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `tui-navigation`: kind-aware search reaches Marketplace resources, not only locally loaded rows.

## Impact

TUI discovery and search, the internal Marketplace client's discovery pagination/kind options, regression tests, terminal QA journeys, and TUI reference documentation. No public API, CLI search, installation policy, or configuration format change.

## Release note

Search the TUI by any resource kind, such as `mcp` or `command`, to discover all matching Marketplace resources without knowing their names.
