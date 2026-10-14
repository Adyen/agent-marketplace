## Purpose

Define how native tool names are translated between harness targets for
hook matchers, subagent tool lists, and skill allowed-tools, driven by shipped
per-harness tool catalogs that maintainers can update without code changes.

## ADDED Requirements

### Requirement: Tool names come from shipped per-harness tool catalogs

The compiler SHALL translate tool names for hook matchers, subagent `tools`,
and skill `allowed-tools` using only tool catalogs shipped with the binary.
Each catalog maps a harness's native tool names, and their accepted aliases,
onto one shared set of tool concepts. It also declares which surfaces the
harness offers. A surface the target catalog does not declare SHALL never be
emitted.

#### Scenario: Factory droids receive Factory tool IDs

- **WHEN** a Claude Code subagent with tools `Read` and `Bash` is compiled for
  Factory Droid
- **THEN** the droid's `tools` MUST be `Read` and `Execute`

#### Scenario: Gemini agents receive Gemini tool names

- **WHEN** a Claude Code subagent with tools `Read` and `Grep` is compiled for
  Gemini CLI
- **THEN** the agent's `tools` MUST be `read_file`, `read_many_files`, and
  `grep_search`

#### Scenario: Renamed tools are accepted through aliases

- **WHEN** a Gemini agent whose `tools` list contains the legacy name
  `search_file_content` is ingested
- **THEN** it MUST be treated as `grep_search`

### Requirement: Allow-lists narrow but never widen

When translating a subagent `tools` list or a skill `allowed-tools` list to
another harness, the compiler SHALL emit every target tool whose concepts all
lie within the concepts of the source list. A target tool that covers any
concept outside the source list SHALL NOT be emitted. When a source tool's
concepts are left uncovered, the target's existing unknown-tool diagnostic
SHALL be emitted with its existing effect:

- `subagent.factory.tool-unknown`, `subagent.gemini.tool-unknown`, and
  `subagent.cursor.tool-unknown` suppress the resource;
- `subagent.devin.tool-unknown` and `skill.devin.tool-unknown` warn and
  omit `allowed-tools`.

A target without a tool-list surface for the resource kind cannot enforce a
restriction. A non-empty source list compiled for such a target SHALL
suppress the resource with that target's unknown-tool diagnostic, instead of
emitting an ignored field or dropping the restriction.

#### Scenario: Write and Edit reach every Factory file-change tool

- **WHEN** a Claude Code subagent with tools `Write` and `Edit` is compiled
  for Factory Droid
- **THEN** the droid's `tools` MUST be `Create`, `Edit`, and `ApplyPatch`

#### Scenario: A combined tool is not granted for part of its scope

- **WHEN** a Claude Code subagent with tool `Edit` only is compiled for
  Factory Droid
- **THEN** the droid's `tools` MUST be `Edit` and MUST NOT contain
  `ApplyPatch`

#### Scenario: Cursor subagents with a tool list are suppressed

- **WHEN** a Claude Code subagent with a non-empty `tools` list is compiled
  for Cursor
- **THEN** no artifact MUST be emitted and the diagnostic MUST be the error
  `subagent.cursor.tool-unknown`

#### Scenario: Devin keeps its narrow allowed-tools vocabulary

- **WHEN** a Claude Code subagent with tools `Read` and `Write` is compiled
  for Devin CLI
- **THEN** `subagent.devin.tool-unknown` MUST be emitted with its existing
  warning effect

### Requirement: Same-harness tool lists keep native names

When a resource's tool-source harness equals the target harness, the
compiler SHALL emit its tool list with the native names as authored,
including wildcards the target documents (for example Gemini `mcp_*`).

#### Scenario: Gemini wildcards survive same-harness output

- **WHEN** a Gemini agent with tools `read_file` and `mcp_*` is ingested and
  compiled for Gemini CLI
- **THEN** the emitted `tools` MUST be `read_file` and `mcp_*`

### Requirement: Tool catalogs are validated and documented

`make catalog-check` SHALL fail when any tool catalog:

- has an unknown key or a duplicate key;
- names a concept missing from the shared vocabulary;
- declares an alias that collides with a tool name or another alias in the
  same catalog;
- restricts a tool to a surface the harness does not declare;
- declares no evidence references.

`make catalog-check` SHALL also fail when the generated tool-name and
hook-contract reference tables in the documentation site differ from the
catalogs.

#### Scenario: Colliding alias fails the check

- **WHEN** a tool catalog declares an alias equal to another tool's name
- **THEN** `make catalog-check` MUST fail and name the catalog

#### Scenario: Stale generated reference fails the check

- **WHEN** a tool catalog is edited without regenerating the reference
  tables
- **THEN** `make catalog-check` MUST fail and name the stale document
