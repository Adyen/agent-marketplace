## ADDED Requirements

### Requirement: Claude Code native delivery is verified by a Claude CLI probe

The Claude Code plugin catalog SHALL declare native delivery verified with
the `claude-native-plugin` probe. The probe SHALL run a real Claude Code CLI
offline with an isolated home, without a model call, at user scope and at
repository scope. Before sync it SHALL confirm that Claude does not list the
plugin. After sync it SHALL confirm that Agent Marketplace wrote none of
Claude's installed-plugin records, known-marketplace records, plugin cache, or
trust state. It SHALL then establish workspace trust as the user would, let
Claude register the declared marketplace itself, and require Claude to list
the plugin enabled and to discover the plugin's namespaced skill. Agent
Marketplace SHALL NOT write Claude trust state; a repository-scope Claude
registration takes effect only in a workspace the user trusted.

#### Scenario: Claude probe loads the registered plugin at both scopes

- **WHEN** a plugin is delivered natively to Claude Code at user scope and,
  separately, at repository scope, and the probe establishes workspace trust
  and starts a Claude session
- **THEN** `claude plugin list` MUST report the plugin enabled and Claude
  initialization MUST list the plugin's namespaced skill at each scope, while
  the pre-sync negative control MUST have listed neither

#### Scenario: Claude probe fails when sync wrote Claude-owned plugin state

- **WHEN** Claude's installed-plugin records, known-marketplace records,
  plugin cache, or trust state exist after sync and before the probe starts a
  Claude session
- **THEN** the probe MUST fail and name the path that exists

#### Scenario: Auto delivers Claude plugins natively

- **WHEN** a plugin with `delivery: auto` is synced to Claude Code
- **THEN** Claude Code MUST receive a native registration and no decomposed
  members, and sync output MUST name native delivery for Claude Code
