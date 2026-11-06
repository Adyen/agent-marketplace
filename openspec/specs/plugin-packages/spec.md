# plugin-packages Specification

## Purpose
Defines how consumers use plugin packages: opting a source into package
interpretation with `format`, selecting plugins as resources without
selecting their members independently, resolving native plugin catalog
entries to exact sources under source policy, and locking plugin
acquisition exactly in the existing acquisition lock format.

## Requirements

### Requirement: Package interpretation is opt-in per source

A Git or directory source SHALL accept an optional `format` field naming a
package format (`agent-plugin`, `claude-plugin`, `factory-plugin`,
`codex-plugin`) or a native plugin catalog format (`claude-marketplace`,
`factory-marketplace`, `codex-marketplace`). A source without `format` SHALL
keep its existing ingestion, even when it contains a plugin manifest.

#### Scenario: Plugin-shaped source without format stays loose

- **WHEN** a source containing `.claude-plugin/plugin.json` has no `format`
  and is selected with `all: true`
- **THEN** sync MUST install the same loose resources, output, and lock
  bytes as before this change

#### Scenario: Format on a Marketplace source is refused

- **WHEN** a source declares both `marketplace` and `format`
- **THEN** configuration loading MUST fail with `v4.option-combination`
  before any acquisition

#### Scenario: Format combined with source_harness is refused

- **WHEN** a source declares both `format` and `source_harness`
- **THEN** configuration loading MUST fail with `v4.option-combination`

#### Scenario: Unknown format is refused

- **WHEN** a source declares a `format` value that is not a recognized
  package or native plugin catalog format
- **THEN** configuration loading MUST fail with `plugin.format.unknown`
  naming the source and field

### Requirement: Plugins are selectable package resources

A format source SHALL expose plugins as resources of kind `plugin`,
selectable with `plugin/<name>` or with a structured `{kind: plugin, name:
<name>}` entry. Plugin names SHALL follow the grammar of the plugin's package
format. A plugin selected by a collection SHALL remain one plugin and SHALL
NOT add its members to the collection's selection.

#### Scenario: Structured entry carries a name the locator cannot

- **WHEN** a configuration selects `{kind: plugin, name: "1password.tools"}`
  from an `agent-plugin` source
- **THEN** the plugin `1password.tools` MUST be selected, and editors MUST
  keep the structured form when rewriting the file

#### Scenario: Plugin options are refused on other kinds

- **WHEN** a `skill/` or `collection/` request carries `delivery` or
  `exclude`
- **THEN** configuration loading MUST fail with `v4.option-combination`

#### Scenario: Installed-name alias on a plugin is refused

- **WHEN** a plugin request carries `as`
- **THEN** configuration loading MUST fail with `v4.option-combination`

### Requirement: Plugin members are not independently selectable

Members SHALL be identified by `kind/name` within their plugin, so members of
different plugins with the same kind and name are distinct. A leaf locator
that names a member of a format source SHALL be refused. A plugin's members
SHALL NOT be deduplicated against other resources by content.

#### Scenario: Same-named members of two plugins stay distinct

- **WHEN** plugins `review-kit` and `lint-kit` each contain `skill/review`
  and both are selected
- **THEN** resolution MUST plan two distinct members

#### Scenario: Direct selection of a plugin member is refused

- **WHEN** a configuration selects `skill/review` from a format source where
  `review` is a member of plugin `review-kit`
- **THEN** resolution MUST fail with `plugin.member-direct-selection` naming
  `plugin/review-kit`

#### Scenario: Matching loose resource is not deduplicated against a member

- **WHEN** a loose `skill/review` from another source is byte-identical to a
  selected plugin's `skill/review`
- **THEN** both MUST be planned as separate resources, and any installed-name
  collision MUST be reported rather than resolved by content

### Requirement: Member exclusion is exact

A plugin request's `exclude` SHALL list exact `kind/name` member locators of
that plugin. An excluded member SHALL NOT be delivered. An entry that names
no member of the plugin SHALL be refused.

#### Scenario: Exclusion of an unknown member is refused

- **WHEN** `exclude` names a locator that is not a member of the plugin
- **THEN** resolution MUST fail with `plugin.member.unknown-exclusion`

### Requirement: Native plugin catalogs resolve entries to exact sources

Reading a native plugin catalog SHALL yield one outcome per entry. Relative
paths SHALL resolve against the format's catalog root. Git and Git
subdirectory entries SHALL be acquired as exact requirements. Entry source
types the format catalog marks refused (such as npm, URL, or command
sources) SHALL be inspectable but not installable. Every origin SHALL pass
the consumer's source policy on its own; listing an origin in a catalog
SHALL NOT authorize it.

#### Scenario: Relative entry resolves against the catalog root

- **WHEN** a `claude-marketplace` source lists an entry with source
  `./plugins/review-kit`
- **THEN** the plugin MUST be read from `plugins/review-kit` relative to the
  directory containing `.claude-plugin/`

#### Scenario: Refused entry source type blocks only that entry

- **WHEN** a catalog lists one Git entry and one npm entry
- **THEN** list MUST show both, the npm entry with
  `plugin.catalog.source-unsupported`, and selecting the npm entry MUST fail
  while the Git entry remains installable

#### Scenario: Catalog-listed origin outside source policy is refused

- **WHEN** an allowed catalog lists an entry whose Git origin is not allowed
  by `allowed_registries`
- **THEN** selecting that entry MUST fail before acquiring the origin

#### Scenario: Duplicate catalog entry names are refused

- **WHEN** a native plugin catalog lists two entries with the same name
- **THEN** both entries MUST be reported with `plugin.catalog.duplicate-entry`
  and neither MUST be installable

### Requirement: Plugin acquisition is locked exactly

A layer that selects a plugin SHALL record, in the existing
`agent-marketplace-lock/v2` format and directly or through the root's
source-revision digest, the catalog origin, commit, catalog root, and index
digest; the selected entry and its overlay digest; the plugin origin, commit,
and subdirectory; the format and version; the package digest and file
inventory; and the selection edges. Locked and offline modes SHALL use only
these records. A layer that selects no plugin SHALL keep its existing lock
bytes.

#### Scenario: Plugin-free layers keep their lock bytes

- **WHEN** sync runs on a layer that selects no plugin
- **THEN** its acquisition lock MUST be byte-identical to the lock the
  previous release writes

#### Scenario: Plugin-selecting layers keep the v2 lock format

- **WHEN** sync locks a layer that selects a plugin
- **THEN** the lock `apiVersion` MUST be `agent-marketplace-lock/v2` and the
  lock MUST contain no field outside the v2 schema

#### Scenario: External catalog entry is locked to its own commit

- **WHEN** a catalog entry points at an external Git repository
- **THEN** the lock MUST record that repository's exact commit separately
  from the catalog commit, and `sync --locked` MUST acquire exactly that
  commit

#### Scenario: Changed catalog entry makes the lock stale

- **WHEN** the selected entry's definition in a live directory catalog
  changes after locking
- **THEN** `sync --check` and `sync --locked` MUST report the plugin record
  stale and write nothing

### Requirement: Released v4 clients fail closed on plugin configuration

A configuration using `format` or `plugin` requests SHALL be refused by
released v4.x clients before any write.

#### Scenario: Released v4 client refuses a plugin configuration

- **WHEN** the pinned released v4 client runs `sync` on a repository whose
  configuration declares a `format` source and a `plugin/` request
- **THEN** it MUST exit non-zero with `v4.unknown-field` or `v4.locator` and
  leave configuration, lock, and harness files unchanged
