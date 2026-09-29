## REMOVED Requirements

### Requirement: Opt-in registration

**Reason:** A dedicated config toggle and registration commands duplicate ordinary resource selection and ownership.
**Migration:** Remove `codeMode` from every config layer and install the first-party `code-mode` collection from its Git registry, then run `sync`.

## ADDED Requirements

### Requirement: Collection-based registration

Code mode SHALL be off by default. The first-party Git registry SHALL publish a `code-mode` collection containing one bootstrap skill and one `agent-marketplace` MCP server resource launching the installed `agent-marketplace mcp` executable. Ordinary resource installation SHALL select both resources, and sync SHALL materialize them only for supported selected harness targets. An unavailable executable or unrepresentable resource SHALL produce a visible actionable failure rather than reporting usable code mode. Ordinary collection removal SHALL remove only owned entries no longer selected elsewhere, preserve handwritten MCP entries and unrelated settings, and refuse unowned collisions. Repeated install, sync, and remove SHALL be idempotent. The former `codeMode` configuration field and the `mcp enable`, `mcp disable`, and `init --code-mode` CLI options SHALL NOT be accepted. Loading a root or fragment with `codeMode` SHALL fail without rewriting that file, with a diagnostic directing the user to remove the field and install the collection. The `agent-marketplace mcp` stdio runtime SHALL remain available with the same `search` and `execute` tool contract.

#### Scenario: Installing collection selects skill and MCP

- **WHEN** a user installs the first-party `code-mode` collection from its Git registry and syncs a supported target
- **THEN** its bootstrap skill and owned `agent-marketplace mcp` server materialize; a missing executable or unsupported target reports an actionable failure, and an unselected fresh config installs neither

#### Scenario: Removing collection preserves other owners

- **WHEN** a user removes the `code-mode` collection and syncs, including a case where one member is still independently selected or the MCP aggregate contains handwritten entries
- **THEN** only unselected owned entries are removed, handwritten entries survive, unowned collisions are refused, and repeating the operations does not change the resulting state

#### Scenario: Retired enablement fails with migration guidance

- **WHEN** a root or included config contains `codeMode`, or a user invokes `mcp enable`, `mcp disable`, or `init --code-mode`
- **THEN** the config load refuses without rewriting it and directs the user to remove the field and install the collection, and the retired CLI options are not available

#### Scenario: Installed server retains two tools

- **WHEN** a harness starts the MCP server installed from the first-party collection
- **THEN** it serves the existing stdio protocol with `search` and `execute` and no extra tools
