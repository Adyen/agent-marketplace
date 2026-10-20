## ADDED Requirements

### Requirement: Plugin operations never execute package content

Discovery, list, inspection, lock, compile, sync, export, and status SHALL
NOT run package scripts, catalog acquisition commands, vendor CLIs, schema
downloads, or plugin subprocesses. A plugin that declares dependencies SHALL
be inspectable and refused for installation.

#### Scenario: Plugin with dependencies is refused

- **WHEN** a selected plugin declares a dependency on another plugin
- **THEN** sync MUST fail that plugin with `plugin.format.dependencies-unsupported`
  and install none of its members

#### Scenario: Plugin lifecycle runs no package process

- **WHEN** a plugin whose manifest and members reference executable scripts
  is listed, inspected, locked, synced, and exported
- **THEN** none of those scripts or any other subprocess outside Git
  acquisition MUST run

### Requirement: Plugins have a CLI lifecycle

`list --kind plugin` SHALL list plugins. Plugin inspection SHALL show the
authored name, exact source and revision, format, members, files, declared
dependencies, unsupported content, per-harness-target delivery, losses, and
blockers. `install`, `remove`, `update`, `sync`, `sync --check`, and
`status` SHALL accept plugins. Status SHALL report the locked revision and
the installed revision separately.

#### Scenario: Install writes a plugin request

- **WHEN** `install plugin/review-kit` runs against a configured format source
- **THEN** the configuration MUST gain a `plugin/review-kit` request and sync
  MUST deliver the plugin

#### Scenario: Remove retires the plugin only on its last selection

- **WHEN** a plugin selected both directly and by a collection is removed
  directly
- **THEN** the plugin MUST stay installed until the collection selection is
  also removed

#### Scenario: Status separates locked and installed revisions

- **WHEN** a plugin instance's update is blocked
- **THEN** status MUST report the locked revision, the still-installed
  revision, and the blocking diagnostic

### Requirement: The TUI shows plugins as parent rows

The TUI SHALL list each plugin as one parent row whose expandable member
rows show skills, commands, subagents, hooks, and MCP servers. Toggling a
member row SHALL change that member's exclusion on the plugin request. It
SHALL NOT select the member independently.

#### Scenario: Plugin members appear under their plugin

- **WHEN** a format source offers a plugin with a skill, a hook, and an MCP
  server
- **THEN** the TUI MUST show one plugin row that expands to those three
  member rows, and no top-level rows for them

#### Scenario: Toggling a member row edits exclusion

- **WHEN** a member row of a selected plugin is toggled off and the change is
  applied
- **THEN** the plugin request MUST gain that member in `exclude`, and no leaf
  request MUST be written
