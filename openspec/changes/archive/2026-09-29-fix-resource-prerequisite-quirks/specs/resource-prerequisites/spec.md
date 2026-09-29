## REMOVED Requirements

### Requirement: Prerequisite declaration syntax

**Reason**: Its `kind: all` scenario defined accidental skill fallback; the replacement explicitly rejects `all`.
**Migration**: Write one of `skill`, `command`, `subagent`, `hook`, or `mcp` in each prerequisite declaration.

### Requirement: Malformed declarations are skipped

**Reason**: Silent skipping makes invalid advisory metadata invisible to registry authors.
**Migration**: Check `list --from` or `doctor` for `catalog.requires.invalid` warnings and fix the indicated item.

### Requirement: Unmet prerequisites do not change exit status

**Reason**: Strict sync must fail on unmet prerequisites to support CI enforcement.
**Migration**: Select the prerequisite before running `sync --strict`, or use ordinary `sync` for advisory warnings.

## ADDED Requirements

### Requirement: Prerequisite kind syntax

The marketplace SHALL read `metadata.requires` as a list in the front matter
of skill, command, and subagent resources. Each item SHALL be either a map with
a `name` and optional `kind`, `registry`, and `path` strings, or a string
shorthand. Kind names SHALL be matched case-insensitively and SHALL accept the
same singular, plural, and alias spellings as `--kind` except `all`, which
SHALL be rejected in prerequisite declarations. Surrounding whitespace SHALL
be trimmed from every value.

#### Scenario: Structured item without kind

- **WHEN** a structured item declares a `name` and no `kind` or an empty `kind`
- **THEN** the prerequisite MUST be a skill with that name

#### Scenario: Kind alias spellings

- **WHEN** a structured item declares `kind` as `MCP_Servers`, `agents`, or `Commands`
- **THEN** the prerequisite kind MUST be MCP server, subagent, or command respectively

#### Scenario: Kind all is rejected

- **WHEN** an item declares `kind: all` or `all:name` with any letter case or surrounding whitespace
- **THEN** it MUST NOT be retained as a prerequisite of any kind
- **AND** a valid sibling MUST still be retained

#### Scenario: Shorthand without a kind

- **WHEN** a resource declares the string item `other-skill`
- **THEN** it MUST be equivalent to a structured skill prerequisite named `other-skill`

#### Scenario: Surrounding whitespace is trimmed

- **WHEN** an item declares ` mcp : docs-server ` or a structured `name`, `registry`, or `path` with surrounding whitespace
- **THEN** the retained kind, name, and hints MUST have the whitespace removed

#### Scenario: Command and subagent prerequisites

- **WHEN** a Markdown command or subagent declares `metadata.requires` in its front matter
- **THEN** its prerequisites MUST be checked like those of a skill

### Requirement: Malformed prerequisite diagnostics

The marketplace SHALL skip malformed prerequisite declarations without failing
to load the declaring resource. A malformed item SHALL NOT prevent remaining
valid items from being read. `list --from` and `doctor` SHALL print a warning
with the stable code `catalog.requires.invalid`, the declaring resource and its
source file, and the item index when the malformed value is a list item. The
warning MUST NOT include the malformed value or credentials from it.

#### Scenario: Non-list requires value

- **WHEN** `metadata.requires` is a string or map instead of a list
- **THEN** the resource MUST load with no prerequisites
- **AND** `list --from` and `doctor` MUST report `catalog.requires.invalid` with its resource and source file

#### Scenario: Invalid item among valid items

- **WHEN** a list contains a valid item and an item with an unknown kind, kind `collection` or `all`, a missing or blank name, a non-string `kind`, `name`, `registry`, or `path`, or a value that is neither a string nor a map
- **THEN** the valid item MUST be retained and the invalid item MUST be dropped
- **AND** `list --from` and `doctor` MUST report `catalog.requires.invalid` for the invalid item's index without including the input value

#### Scenario: Malformed item stays silent

- **WHEN** a selected resource declares only malformed prerequisite items and ordinary sync runs
- **THEN** sync MUST materialize the resource and MUST NOT print an unmet-prerequisite warning

### Requirement: Prerequisite exit status

Unmet prerequisites SHALL NOT change the exit status of ordinary `install`,
`sync`, `sync --check`, `status`, or `doctor`. Under `sync --strict`, including
`sync --strict --check`, they SHALL cause a nonzero exit status without changing
resource selection.

#### Scenario: Commands succeed with unmet prerequisites

- **WHEN** install, sync, sync --check, status, or doctor finds only unmet prerequisites and nothing else to report
- **THEN** the command MUST exit with status 0

#### Scenario: Strict sync rejects unmet prerequisites

- **WHEN** `sync --strict` or `sync --strict --check` finds an unmet prerequisite
- **THEN** sync MUST print the remediable warning and MUST exit nonzero
- **AND** it MUST NOT select the prerequisite

#### Scenario: Strict sync succeeds when satisfied

- **WHEN** a selected resource's prerequisites are also selected and `sync --strict` runs
- **THEN** sync MUST NOT print an unmet-prerequisite warning or fail because of it
