# Proposal

## Why

Searching `mcp` in the TUI does not discover Marketplace MCP servers whose names and descriptions lack that word. Users should be able to discover resources by kind without first knowing their names.

## What Changes

- Match resource kinds in TUI search for both configured and uninstalled Marketplace resources.
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

Search the TUI by resource kind, such as `mcp`, to discover matching Marketplace resources without knowing their names.
