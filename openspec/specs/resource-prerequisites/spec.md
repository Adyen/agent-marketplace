# resource-prerequisites Specification

## Purpose

Define how catalog resources declare functional prerequisites and how commands
report prerequisites that are not selected without changing configuration.

## Requirements

### Requirement: Resources declare prerequisites

The marketplace SHALL read prerequisites from `metadata.requires` on cataloged
resources.

#### Scenario: Structured MCP prerequisite

- **WHEN** a resource declares an item with `kind: mcp` and a non-empty name
- **THEN** the catalog MUST retain its kind, name, and optional provider hints

#### Scenario: Shorthand prerequisite

- **WHEN** a resource declares `"mcp:server-name"`
- **THEN** it MUST be equivalent to a structured MCP prerequisite without
  provider hints

### Requirement: Prerequisites do not change selection

The marketplace SHALL treat prerequisite declarations as advisory and SHALL NOT
change resource selection because of them.

#### Scenario: Install finds an unmet prerequisite

- **WHEN** an installed resource declares a prerequisite that is not selected
- **THEN** install MUST emit a remediable warning
- **AND** it MUST NOT select the prerequisite

#### Scenario: Prerequisite is explicitly excluded

- **WHEN** a selected resource requires an explicitly excluded resource that is not `required: true`
- **THEN** the marketplace MUST report the unmet prerequisite
- **AND** it MUST preserve the explicit exclude

#### Scenario: Excluded prerequisite is required

- **WHEN** a selected resource requires an excluded resource provided by a `required: true` entry
- **THEN** existing `required: true` precedence MUST keep the prerequisite selected
- **AND** the marketplace MUST NOT report it as unmet

### Requirement: Unmet prerequisites are visible

The marketplace SHALL report selected resources whose prerequisite kind and
name are not selected.

#### Scenario: Sync reports without mutation

- **WHEN** sync finds an unmet prerequisite
- **THEN** it MUST emit a remediable warning
- **AND** it MUST NOT enable the prerequisite

#### Scenario: Status and doctor diagnostics

- **WHEN** status or doctor finds an unmet prerequisite
- **THEN** it MUST identify both resources and an installation command

### Requirement: Satisfaction by kind and name

A prerequisite SHALL be met when any selected resource in the same resolution has the same kind and name, regardless of the registry, path, configuration layer, or Marketplace selection that supplies it. The `registry` and `path` hints SHALL NOT restrict satisfaction.

#### Scenario: Prerequisite supplied by another registry

- **WHEN** a skill requires an MCP server with a `registry` hint and a different registry supplies a selected MCP server with that name
- **THEN** the marketplace MUST NOT report the prerequisite as unmet

#### Scenario: Same name with another kind

- **WHEN** a skill requires MCP server `docs` and only a skill named `docs` is selected
- **THEN** the marketplace MUST report the MCP prerequisite as unmet

#### Scenario: Doctor treats a Marketplace selection as a satisfied prerequisite

- **WHEN** a selected resource declares a prerequisite that only a Marketplace-selected resource satisfies and `doctor` runs
- **THEN** `doctor` MUST NOT report the prerequisite as unmet

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
valid items from being read or cause unrelated resources in a covered
sub-bundle to be reported as undiscovered. `list --from` and `doctor` SHALL
print a warning with the stable code `catalog.requires.invalid`, the declaring
resource and its source file, and the item index when the malformed value is a
list item. The warning MUST NOT include the malformed value or credentials from
it.

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

#### Scenario: Covered sub-bundle with malformed prerequisite

- **WHEN** a discovered resource in a covered organizational sub-bundle declares a malformed prerequisite
- **THEN** the marketplace MUST retain the resource and report `catalog.requires.invalid`
- **AND** it MUST NOT report that covered sub-bundle as ignored

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

### Requirement: Prerequisite remediation arguments are shell literals

Agent Marketplace MUST format registry-authored prerequisite resource names, registry locations and paths as literal POSIX shell arguments in install remediation. Command/variable substitution, whitespace, delimiters and embedded single quotes MUST NOT gain shell meaning from display quoting. Existing credential redaction MUST occur before argument formatting. Typed unmet warnings, resource selection and requested kind/location semantics MUST remain unchanged. This requirement changes human-readable command guidance, not resource schemas or machine-readable warning identities.

#### Scenario: Requirement remediation keeps shell syntax literal

- **WHEN** prerequisite name or location arguments contain inert shell syntax or embedded quotes
- **THEN** install remediation MUST represent them as literal arguments using the established POSIX formatter after credential redaction

#### Scenario: Resolver keeps ordinary prerequisite warnings and selection

- **WHEN** an otherwise selected resource declares an unmet prerequisite
- **THEN** resolution MUST keep the selected resource and typed warning while its install hint represents the prerequisite name literally
