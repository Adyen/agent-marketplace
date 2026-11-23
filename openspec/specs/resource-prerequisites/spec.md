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

When requirement expansion is unset or disabled, Agent Marketplace SHALL treat prerequisite declarations as advisory and SHALL NOT change resource selection because of them. When expansion is enabled, Agent Marketplace SHALL select configured, resolvable prerequisites transitively after collection expansion and required-shadow handling. An unavailable or explicitly excluded prerequisite MUST remain unmet without removing its requiring resource. Implied resources MUST NOT become required selections.

#### Scenario: Install finds an unmet prerequisite

- **WHEN** install selects a resource whose prerequisite remains unmet after resolution, because expansion is disabled or no eligible candidate can be selected
- **THEN** install MUST emit a remediable warning
- **AND** it MUST NOT select the unmet prerequisite

#### Scenario: Prerequisite is explicitly excluded

- **WHEN** a selected resource requires an explicitly excluded resource that is not `required: true`
- **THEN** the marketplace MUST report the unmet prerequisite
- **AND** it MUST preserve the explicit exclude and the requiring resource's selection

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
`agent-marketplace install` with the POSIX-literal-formatted `<prerequisite>`
and `--kind <kind>`, followed by `--from` and `--path` with the corresponding
POSIX-literal-formatted hint values when those hints are present. Argument
formatting SHALL follow the prerequisite shell-literal requirement below.
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

The `status` `UNMET REQUIREMENTS` table SHALL list one row per unmet prerequisite with `RESOURCE` and `REQUIRES` as `<kind>:<name>`, `REASON` as its typed `not-found`, `excluded` or `source-not-configured` reason, and `FIX` as the existing redacted, POSIX-literal remediation. Rows SHALL be sorted by resource and then by prerequisite.

#### Scenario: Rows are sorted

- **WHEN** several selected resources have unmet prerequisites
- **THEN** status MUST print the rows sorted by `RESOURCE` and then by `REQUIRES`

#### Scenario: Status reports typed reasons safely

- **WHEN** selected resources have missing, excluded and unconfigured-source prerequisites, including a credential-bearing source hint
- **THEN** status MUST show the corresponding typed reasons and existing installation remedies
- **AND** credentials MUST be redacted before shell-literal formatting, and status MUST leave configuration, acquisition locks and materialized resources unchanged

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

Unmet prerequisites SHALL NOT change the exit status of ordinary `install`, `sync`, `sync --check`, `status`, or `doctor`. Under `sync --strict`, including `sync --strict --check`, a prerequisite that remains unmet after resolution SHALL cause a nonzero exit status without changing resource selection. A prerequisite selected by enabled expansion SHALL NOT cause strict sync to fail.

#### Scenario: Commands succeed with unmet prerequisites

- **WHEN** install, sync, sync --check, status, or doctor finds only unmet prerequisites and nothing else to report
- **THEN** the command MUST exit with status 0

#### Scenario: Strict sync rejects unmet prerequisites

- **WHEN** `sync --strict` or `sync --strict --check` finds an unmet prerequisite
- **THEN** sync MUST print the remediable warning and MUST exit nonzero
- **AND** it MUST NOT select the unavailable or explicitly excluded prerequisite

#### Scenario: Strict sync succeeds when satisfied

- **WHEN** a selected resource's prerequisites are selected, including by enabled expansion, and `sync --strict` runs
- **THEN** sync MUST NOT print an unmet-prerequisite warning or fail because of it

### Requirement: Prerequisite remediation arguments are shell literals

Agent Marketplace MUST format registry-authored prerequisite resource names, registry locations and paths as literal POSIX shell arguments in install remediation. Command/variable substitution, whitespace, delimiters and embedded single quotes MUST NOT gain shell meaning from display quoting. Existing credential redaction MUST occur before argument formatting. Typed unmet warnings, resource selection and requested kind/location semantics MUST remain unchanged. This requirement changes human-readable command guidance, not resource schemas or machine-readable warning identities.

#### Scenario: Requirement remediation keeps shell syntax literal

- **WHEN** prerequisite name or location arguments contain inert shell syntax or embedded quotes
- **THEN** install remediation MUST represent them as literal arguments using the established POSIX formatter after credential redaction

#### Scenario: Resolver keeps ordinary prerequisite warnings and selection

- **WHEN** an otherwise selected resource declares an unmet prerequisite
- **THEN** resolution MUST keep the selected resource and typed warning while its install hint represents the prerequisite name literally

### Requirement: Prerequisite lookup uses configured source identity

Agent Marketplace SHALL resolve an unmet prerequisite only from the requiring resource's exact source identity when it has no source hint. A Git `registry` hint SHALL identify an already configured source by normalized Git URL; an optional `path` hint SHALL narrow lookup to that configured root. A hint with no configured match SHALL remain unmet without fallback to the requiring resource's source or acquisition of another source. A directory hint SHALL match only an absolute, cleaned location of an already configured directory source. Lookup SHALL use already discovered catalog resources. A malformed candidate MUST retain its typed malformed diagnostic attributed to the requiring resource.

#### Scenario: Unhinted prerequisite uses the requiring source root

- **WHEN** an unmet prerequisite has no source hint and a matching resource exists only at a different URL, ref, or root
- **THEN** lookup MUST leave the prerequisite unmet and MUST NOT select the resource from the other source identity

#### Scenario: Configured Git hint narrows lookup to its root

- **WHEN** an unmet prerequisite has a Git hint matching an already configured source and a path naming one of its configured roots
- **THEN** lookup MUST consider matching resources from that root without acquiring or adding a source

#### Scenario: Unconfigured Git hint has no fallback

- **WHEN** an unmet prerequisite has a Git hint that matches no configured source and a same-named resource exists in the requiring resource's source
- **THEN** the prerequisite MUST remain unmet with reason `source-not-configured`
- **AND** the requiring resource's source MUST NOT be used as a fallback

#### Scenario: Directory hints require an absolute configured location

- **WHEN** a directory hint is compared with configured directory sources
- **THEN** only an absolute, cleaned configured directory location MUST match
- **AND** a relative path or source-name alias MUST NOT match a directory source

#### Scenario: Malformed candidate keeps its diagnostic attribution

- **WHEN** a candidate for an unmet prerequisite cannot be used because of malformed resource metadata
- **THEN** Agent Marketplace MUST retain its typed malformed diagnostic attributed to the requiring resource
- **AND** the requiring resource MUST remain selected

### Requirement: Expanded prerequisites obey scope and selection precedence

When expansion is enabled, Agent Marketplace SHALL admit a new implied resource separately in each scope containing a requiring resource, subject to the existing effective harness-target set. A prerequisite SHALL be satisfied by any resource actually selected in the same resolution with the same kind and name, regardless of source, configuration layer, install scope, or Marketplace selection; scope SHALL affect admission only for newly implied resources. An implied resource SHALL inherit the supplying source entry's targets intersected with the effective harness-target set. Explicit exclusions and exclude-all selections MUST prevent an implied selection unless an existing `required: true` selection supplies it, and a cross-scope exclusion MUST NOT change machine-scope selection. A direct selection MUST win over an implied selection with the same kind and name. Cycles MUST terminate, and deterministic resolved-resource and declaration order MUST determine the first implied winner.

#### Scenario: Transitive prerequisites are selected after collections

- **WHEN** expansion is enabled and a selected resource, including a collection member, requires a resource that itself has a resolvable prerequisite
- **THEN** Agent Marketplace MUST select the transitive prerequisite closure after collection expansion and required-shadow handling
- **AND** it MUST keep the original requiring resource selected

#### Scenario: Existing selection satisfies by kind and name

- **WHEN** any resource actually selected in the same resolution has the same kind and name as a prerequisite, regardless of source, configuration layer, install scope, or Marketplace selection
- **THEN** the prerequisite MUST be considered satisfied without selecting a duplicate

#### Scenario: Implied resources stay within scope and effective targets

- **WHEN** a requiring resource is selected in a scope with an effective harness-target set and its prerequisite is supplied by an entry with declared targets
- **THEN** the implied resource MUST be admitted only in that requiring scope
- **AND** its targets MUST be the intersection of the supplying entry's targets and the effective target set
- **AND** it MUST NOT become a `required: true` selection

#### Scenario: Cross-scope exclusion does not change machine selection

- **WHEN** an exclusion in one scope matches an implied prerequisite selected by a different scope
- **THEN** the exclusion MUST NOT remove or replace the other scope's selection

#### Scenario: Direct selection wins an implied collision

- **WHEN** a resource is selected directly and a prerequisite resolves to the same kind and name from another source
- **THEN** the direct selection MUST remain the winner

#### Scenario: Cycles and competing implied candidates resolve deterministically

- **WHEN** selected resources contain a prerequisite cycle or multiple declarations resolve to the same kind and name from different sources
- **THEN** resolution MUST terminate and the first candidate in resolved-resource and declaration order MUST win

### Requirement: Prerequisite provenance is distinct from collection provenance

Agent Marketplace SHALL retain which selected resources require each implied resource separately from collection membership. Prerequisite expansion MUST NOT be represented as collection provenance, and existing collection provenance MUST remain unchanged.

#### Scenario: Collection member requirement retains both relationships

- **WHEN** a selected collection member requires an implied resource
- **THEN** the result MUST identify the requiring collection member as the prerequisite source
- **AND** the implied resource's collection provenance MUST continue to identify only its collection membership

### Requirement: Disabled expansion reports an implied preview

When `requires.expand` is unset or false, Agent Marketplace SHALL compute the transitive closure of eligible prerequisites as an advisory preview without appending implied resources to the actual selected resources or writing implied selections to configuration. Existing selection, warning, and strict-sync semantics MUST remain unchanged. Repeated resolution of the same selections and catalog MUST produce the same preview without duplicate implied resources.

#### Scenario: Disabled expansion preserves selection while previewing prerequisites

- **WHEN** expansion is disabled and a selected resource has a resolvable prerequisite
- **THEN** the requiring resource MUST remain selected and the prerequisite MUST NOT be selected or written to configuration
- **AND** the advisory preview MUST contain the transitive implied resources

#### Scenario: Advisory preview is idempotent

- **WHEN** resolution is repeated with the same direct selections, catalogs, and configuration while expansion is disabled
- **THEN** it MUST produce the same implied preview and actual selection without duplicate implied resources

### Requirement: Unmet prerequisite warnings carry typed reasons

Each unmet prerequisite warning SHALL carry exactly one typed reason: `not-found`, `excluded`, or `source-not-configured`. The reason SHALL distinguish an absent candidate, a candidate prevented by an effective exclusion, and a source hint with no configured match. Warning text, credential redaction, and shell-literal remediation SHALL retain their existing requirements.

#### Scenario: Missing candidate has not-found reason

- **WHEN** lookup finds no eligible candidate for an unmet prerequisite and no source hint is unconfigured
- **THEN** its warning MUST carry reason `not-found`

#### Scenario: Excluded candidate has excluded reason

- **WHEN** a matching candidate is prevented from selection by an effective explicit exclusion or exclude-all selection
- **THEN** its warning MUST carry reason `excluded`

#### Scenario: Unconfigured hint has source-not-configured reason

- **WHEN** a prerequisite's source hint matches no configured source
- **THEN** its warning MUST carry reason `source-not-configured`

### Requirement: List exposes prerequisite relationships

Configured `list` SHALL include a `VIA` column showing resolved collection provenance and `requires <kind>:<name>` provenance separately when both apply. `list --from` SHALL include a `REQUIRES` column listing valid declared prerequisite kind/name pairs in declaration order, with canonical CLI kind labels and empty cells for resources with no prerequisites. Existing available and selected rows, kind filtering, malformed prerequisite diagnostics and browse-only behavior SHALL remain intact. Registry-authored text SHALL retain terminal escaping, and credential-bearing source information SHALL be redacted before display.

#### Scenario: Configured list retains collection and requirement provenance

- **WHEN** selected resources have collection membership, requirement provenance or both and configured list runs
- **THEN** its VIA column MUST show the corresponding relationships without treating requirement provenance as a collection
- **AND** kind filtering MUST retain the matching rows

#### Scenario: Ad hoc list shows declarations without selecting them

- **WHEN** list --from browses resources with multiple valid prerequisite declarations, no declarations or malformed declarations
- **THEN** REQUIRES MUST show valid kind/name pairs in declaration order and remain empty for resources without valid declarations
- **AND** malformed prerequisite diagnostics MUST retain their existing non-quoting behavior, and browse MUST NOT write configuration, locks or materialized output

### Requirement: Doctor identifies unconfigured prerequisite hints

Doctor SHALL explicitly identify unmet prerequisites with reason `source-not-configured` as a source-hint configuration warning and present the existing install remediation including `--from`. The check SHALL remain warning-only, SHALL NOT configure or acquire the hinted source, and SHALL retain existing credential redaction, POSIX-literal argument formatting, malformed diagnostics and suppression for selected pending Marketplace providers. A prerequisite satisfied by a selected resource with the same kind and name SHALL NOT produce an unconfigured-source accusation.

#### Scenario: Doctor explains an unconfigured hint without mutation

- **WHEN** a selected resource has an unmet prerequisite whose credential-bearing source hint names no configured source and doctor runs
- **THEN** doctor MUST explicitly report the unconfigured source hint and the redacted literal install --from remedy with a successful ordinary exit status
- **AND** it MUST NOT acquire the hinted source or change configuration, locks or materialized output

#### Scenario: Doctor does not accuse satisfied or pending providers

- **WHEN** a prerequisite is satisfied by an actually selected kind/name provider from another source, or its selected Marketplace provider is pending usable local evidence
- **THEN** doctor MUST retain satisfaction or pending-provider suppression without an unconfigured-source accusation
