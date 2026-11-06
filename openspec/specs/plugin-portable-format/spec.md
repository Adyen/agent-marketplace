# plugin-portable-format Specification

## Purpose
Defines the portable plugin package format: how an `agent-plugin` source
written to Agent Plugins 1.0.0 is imported into plugin IR (manifest
validation, member discovery, and failure semantics) and how portable MCP
runtime values keep their meaning when a plugin member is projected to a
harness target.

## Requirements

### Requirement: Agent Plugins 1.0.0 import follows the specification's failure rules

An `agent-plugin` source SHALL be interpreted as Agent Plugins 1.0.0 using
bundled schema data, never fetching `$schema`. Any other declared version,
including the 1.1.0 working draft, SHALL be refused. Invalid required
manifest data SHALL reject the package before component discovery. Unknown
top-level fields, and a non-object `extensions` value, SHALL be reported and
ignored. Skills SHALL be discovered only as immediate children of `skills/`.
An invalid skill or MCP server SHALL be skipped with a diagnostic while
valid siblings remain.

#### Scenario: Unsupported Agent Plugins version is refused

- **WHEN** an `agent-plugin` source's manifest declares a version other than
  1.0.0
- **THEN** the plugin MUST be reported with `plugin.format.version-unsupported`
  and not be installable

#### Scenario: Invalid required manifest field rejects the package

- **WHEN** the manifest lacks a valid required `name`
- **THEN** the package MUST be rejected with `plugin.format.manifest-invalid`
  and no component MUST be discovered

#### Scenario: Nested skill directories are not discovered

- **WHEN** a package has `skills/a/SKILL.md` and `skills/group/b/SKILL.md`
- **THEN** only skill `a` MUST be a member

#### Scenario: Invalid skill is skipped while siblings remain

- **WHEN** one of two skills under `skills/` has invalid frontmatter
- **THEN** inspection MUST list the valid skill as a member and report the
  invalid one with a diagnostic

### Requirement: Portable MCP runtime values keep their meaning

Only `${PLUGIN_ROOT}` and `${PLUGIN_DATA}` SHALL expand, once and
non-recursively, and only in MCP `args`, environment values, and `cwd`. Other
placeholder-like text SHALL stay literal. When a harness target would
reinterpret a literal placeholder and has no escape for it, that MCP member
SHALL be blocked rather than changed.

#### Scenario: Unrecognized placeholder stays literal

- **WHEN** an Agent Plugins MCP server has an argument `${UNRECOGNIZED}`
- **THEN** decomposed and native output MUST carry the argument with the same
  meaning as the literal text, or block the member with
  `plugin.member.literal-unrepresentable`

#### Scenario: Plugin root expands in arguments only

- **WHEN** `${PLUGIN_ROOT}` appears in both an MCP `command` and its `args`
- **THEN** only the `args` occurrence MUST be resolved to the package
  generation

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
