## Why

Plugin support is implemented across sources, selection, locks, decomposed
and native delivery, the CLI, the TUI, and export, but the public
documentation only covers it in reference tables. Users have no task guide
for installing a plugin and no explanation of how plugins, package formats,
and delivery modes fit together. Released clients refuse configurations that
use plugins, so teams also need to know which version everyone must run
before a plugin source is committed.

## What Changes

- A how-to guide for installing plugins: declaring a `format` source,
  selecting `plugin/<name>` or a structured `{kind: plugin, name: <name>}`
  entry, excluding members, choosing a delivery mode, inspecting, exporting,
  syncing, checking status, and removing a plugin, with the refusals users
  meet and the minimum client version for mixed-version teams.
- An explanation page covering plugins and their members, package formats
  and native plugin catalogs, decomposed and native delivery and the evidence
  behind each harness's support, package generations and plugin locks, and
  why scripts, acquisition commands, and dependencies are refused.
- Glossary entries for the plugin terms, and links to both pages from the
  documentation index and sidebar.
- The generated plugin support reference stays current with the shipped
  catalogs.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None. Documentation only; no requirement changes.

## Impact

- `site/docs`: a new how-to, a new explanation page, the glossary, and the
  index.
- `site/.vitepress/config.mts`: sidebar links.
- No code, configuration format, lock format, or diagnostic changes.

## Release note

Breaking: Agent Marketplace can now install plugins, packages that bundle
skills, commands, subagents, hooks, and MCP servers, from Agent Plugins,
Claude Code, Factory Droid, and Codex plugin sources: declare `format` on a
source and select `plugin/<name>`. Clients older than v5.2.0 refuse any
configuration that uses `format` or `plugin/`, failing with
`v4.unknown-field` or `v4.locator`, so every teammate and CI job that reads
the repository must upgrade to v5.2.0 before you commit a plugin source.
