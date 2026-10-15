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
