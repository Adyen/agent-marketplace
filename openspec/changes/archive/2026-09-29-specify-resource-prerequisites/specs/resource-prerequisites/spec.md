## ADDED Requirements

### Requirement: Prerequisite declaration syntax

The marketplace SHALL read `metadata.requires` as a list in the front matter
of skill, command, and subagent resources. Each item SHALL be either a map with
a `name` and optional `kind`, `registry`, and `path` strings, or a string
shorthand. Kind names SHALL be matched case-insensitively and SHALL accept the
same singular, plural, and alias spellings as `--kind`. Surrounding whitespace
SHALL be trimmed from every value.

#### Scenario: Structured item without kind

- **WHEN** a structured item declares a `name` and no `kind` or an empty `kind`
- **THEN** the prerequisite MUST be a skill with that name

#### Scenario: Kind alias spellings

- **WHEN** a structured item declares `kind` as `MCP_Servers`, `agents`, or `Commands`
- **THEN** the prerequisite kind MUST be MCP server, subagent, or command respectively

#### Scenario: Kind all means skill

- **WHEN** a structured item declares `kind: all`
- **THEN** the prerequisite MUST be a skill with that name

#### Scenario: Shorthand without a kind

- **WHEN** a resource declares the string item `other-skill`
- **THEN** it MUST be equivalent to a structured skill prerequisite named `other-skill`

#### Scenario: Surrounding whitespace is trimmed

- **WHEN** an item declares ` mcp : docs-server ` or a structured `name`, `registry`, or `path` with surrounding whitespace
- **THEN** the retained kind, name, and hints MUST have the whitespace removed

#### Scenario: Command and subagent prerequisites

- **WHEN** a Markdown command or subagent declares `metadata.requires` in its front matter
- **THEN** its prerequisites MUST be checked like those of a skill

### Requirement: Malformed declarations are skipped

The marketplace SHALL skip malformed prerequisite declarations without
failing to load the declaring resource and without reporting them in command
output. A malformed item SHALL NOT prevent the remaining items from being read.

#### Scenario: Non-list requires value

- **WHEN** `metadata.requires` is a string or map instead of a list
- **THEN** the resource MUST load with no prerequisites

#### Scenario: Invalid item among valid items

- **WHEN** a list contains a valid item and an item with an unknown kind, kind `collection`, a missing or blank name, a non-string `kind`, `name`, `registry`, or `path`, or a value that is neither a string nor a map
- **THEN** the valid item MUST be retained
- **AND** the invalid item MUST be dropped

#### Scenario: Malformed item stays silent

- **WHEN** a selected resource declares only malformed prerequisite items and sync runs
- **THEN** sync MUST materialize the resource
- **AND** its output MUST contain no prerequisite warning

### Requirement: Satisfaction by kind and name

A prerequisite SHALL be met when any selected resource in the same resolution
has the same kind and name, regardless of the registry, path, or configuration
layer that supplies it. The `registry` and `path` hints SHALL NOT restrict
satisfaction.

#### Scenario: Prerequisite supplied by another registry

- **WHEN** a skill requires an MCP server with a `registry` hint and a different registry supplies a selected MCP server with that name
- **THEN** the marketplace MUST NOT report the prerequisite as unmet

#### Scenario: Same name with another kind

- **WHEN** a skill requires MCP server `docs` and only a skill named `docs` is selected
- **THEN** the marketplace MUST report the MCP prerequisite as unmet

### Requirement: Unmet prerequisite warning text

Each unmet prerequisite SHALL produce the message
`<kind> "<resource>" requires <kind> "<prerequisite>", but it is not selected`
and the remediation
`agent-marketplace install "<prerequisite>" --kind <kind>`, followed by
`--from "<registry>"` and `--path "<path>"` when those hints are present.
Kinds SHALL be printed as `skill`, `command`, `subagent`, `hook`, or `mcp`.

#### Scenario: Remediation with hints

- **WHEN** an unmet MCP prerequisite declares `registry` and `path` hints
- **THEN** the remediation MUST be the install command with `--kind mcp`, `--from`, and `--path` carrying those hints

#### Scenario: Registry hint credentials are redacted

- **WHEN** a `registry` hint contains URL credentials
- **THEN** the remediation MUST replace the credentials with `***`

### Requirement: Install reports only what it selected

`install` SHALL report unmet prerequisites only for the resources that the
install selects, including members of a collection it installs, and SHALL NOT
repeat unmet prerequisites of resources that were already selected.

#### Scenario: Unrelated unmet prerequisite is omitted

- **WHEN** an already selected resource has an unmet prerequisite and install selects a different resource
- **THEN** install MUST NOT print the already selected resource's warning

#### Scenario: Collection member prerequisite is reported

- **WHEN** install selects a collection whose member has an unmet prerequisite
- **THEN** install MUST print that member's warning

### Requirement: Status orders unmet prerequisites

The `status` `UNMET REQUIREMENTS` table SHALL list one row per unmet
prerequisite with `RESOURCE` and `REQUIRES` as `<kind>:<name>` and `FIX` as the
remediation, sorted by resource and then by prerequisite.

#### Scenario: Rows are sorted

- **WHEN** several selected resources have unmet prerequisites
- **THEN** status MUST print the rows sorted by `RESOURCE` and then by `REQUIRES`

### Requirement: Unmet prerequisites do not change exit status

Unmet prerequisites SHALL be warnings only and SHALL NOT change the exit
status of any command, including under `sync --strict`.

#### Scenario: Commands succeed with unmet prerequisites

- **WHEN** install, sync, sync --check, status, or doctor finds only unmet prerequisites and nothing else to report
- **THEN** the command MUST exit with status 0

#### Scenario: Strict sync does not promote unmet prerequisites

- **WHEN** `sync --strict` finds an unmet prerequisite
- **THEN** sync MUST print the warning
- **AND** it MUST exit with status 0
