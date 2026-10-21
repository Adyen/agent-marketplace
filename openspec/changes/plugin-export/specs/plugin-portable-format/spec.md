## ADDED Requirements

### Requirement: Export is loss-intolerant by default

`export plugin/<name> --format <format> --out <dir>` SHALL write the locked
plugin as the requested package format into a new or empty directory. If any
member, field, or file cannot be represented, the export SHALL fail, write
nothing, and list the losses. `--allow-loss` SHALL permit omissions that are
not security restrictions and SHALL list each omission. No option SHALL
permit omitting a security restriction.

#### Scenario: Export with a loss writes nothing

- **WHEN** a plugin containing a hook is exported to `agent-plugin`
- **THEN** export MUST fail with `plugin.export.loss`, list the hook, and
  leave `--out` absent or empty

#### Scenario: Allow-loss export omits and reports non-security members

- **WHEN** the same export runs with `--allow-loss`
- **THEN** the package MUST be written without the hook, and the omission
  MUST be listed

#### Scenario: Allow-loss cannot drop a restriction

- **WHEN** an export would need to drop a member's security restriction
- **THEN** export MUST fail even with `--allow-loss`

#### Scenario: Export into a non-empty directory is refused

- **WHEN** `--out` names a non-empty directory
- **THEN** export MUST fail before writing

#### Scenario: Exported Agent Plugins package re-imports equivalently

- **WHEN** a plugin with skills and MCP servers only is exported to
  `agent-plugin` and the result is imported as an `agent-plugin` source
- **THEN** the imported plugin's members and their content MUST equal the
  original's
