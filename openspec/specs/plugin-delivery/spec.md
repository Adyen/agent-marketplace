# plugin-delivery Specification

## Purpose
Defines how plugin members are delivered to a harness target under the
plugin-surface field policies in the `pluginpolicy` catalogs: fields the
catalog marks as blocking restrictions refuse lossy delivery, while
preference losses omit only the field with a warning.

## Requirements

### Requirement: Security restrictions are never accepted as loss

A member field that the harness catalog marks as a blocking restriction on
the plugin surface SHALL block the member with the catalog's diagnostic. No
option SHALL install that member without the restriction. A preference loss
(for example an unresolved model) SHALL omit only the field, with a warning.

#### Scenario: Claude plugin subagent with permissionMode is blocked

- **WHEN** a plugin subagent declaring `permissionMode` is delivered natively
  to Claude Code
- **THEN** the member MUST be blocked with
  `plugin.member.restriction-unsupported`, blocking the instance

#### Scenario: Unresolved model on a plugin member warns and installs

- **WHEN** a plugin subagent selects a model the destination model catalog
  cannot resolve
- **THEN** the member MUST be delivered without the model field, with a
  warning

### Requirement: Plugin instances install as a unit

Each plugin instance, one plugin delivered to one harness target in one
scope, SHALL install as a unit. If any non-excluded member fails to decode
or compile, or is blocked by a plugin-surface restriction, the update of that
instance SHALL be blocked. Its previously installed projection SHALL stay
unchanged, and a first installation SHALL install nothing. Other plugin
instances and independently selected resources SHALL continue.

#### Scenario: A failing member blocks the whole instance update

- **WHEN** a plugin's new revision has one member that fails to compile for a
  harness target
- **THEN** that instance MUST keep every member of the previous revision,
  report the failing member, and install no member of the new revision

#### Scenario: A failing member on first install installs nothing

- **WHEN** a newly selected plugin has one member that fails to compile for a
  harness target
- **THEN** no member of that plugin MUST be installed for that harness target

#### Scenario: Other instances continue past a blocked instance

- **WHEN** one plugin instance is blocked
- **THEN** other plugins, the same plugin on other harness targets, and leaf
  resources MUST still be delivered

#### Scenario: Excluding the failing member unblocks the instance

- **WHEN** the failing member is added to `exclude`
- **THEN** the instance MUST install the new revision without that member

### Requirement: Decomposed members use a deterministic namespace

Decomposed delivery SHALL install each member under the installed name
`<plugin>-<member>`. A name that collides with another installed name in the
same harness target and scope, or that breaks the harness target's name
rules, SHALL be refused with `plugin.namespace-collision` or
`plugin.namespace-invalid`. No suffix SHALL be added. The sync preview SHALL
show the installed names.

#### Scenario: Decomposed skill gets a namespaced installed name

- **WHEN** plugin `review-kit` with member `skill/review` is decomposed to
  Cursor at repository scope
- **THEN** `.cursor/skills/review-kit-review/SKILL.md` MUST be written

#### Scenario: Namespace collision is refused

- **WHEN** a decomposed member's installed name equals the installed name of
  another resource in the same harness target and scope
- **THEN** that plugin instance MUST fail with `plugin.namespace-collision`
  and the other resource MUST stay unchanged

### Requirement: Package roots resolve to immutable package generations

Both delivery modes SHALL resolve plugin-root references to an immutable,
complete package generation owned by Agent Marketplace, never to an
acquisition cache or upstream checkout. A generation SHALL be retained while
any committed projection, registration, or recovery journal references it.
Garbage collection SHALL remove only unreferenced generations.

#### Scenario: Plugin hook command reaches its generation

- **WHEN** a plugin hook command references `${CLAUDE_PLUGIN_ROOT}/scripts/check.sh`
  and is decomposed to a harness target
- **THEN** the installed command MUST reference `scripts/check.sh` inside the
  instance's package generation, and that file MUST exist with its source
  bytes and mode

#### Scenario: Cache deletion keeps installed plugins working

- **WHEN** the acquisition cache is deleted after sync
- **THEN** `sync --check` MUST report no drift for the plugin, and its
  package generation MUST be unchanged

#### Scenario: Unreferenced generations are collected

- **WHEN** a plugin is updated and the previous generation is no longer
  referenced
- **THEN** `gc` MUST remove that generation and keep the current one

### Requirement: Removal retires only owned plugin output

Removing a plugin's last selection SHALL unregister owned native entries,
remove owned decomposed members, and release the instance's generation
reference. Handwritten content, other scopes, and plugin data SHALL be kept,
and the retained plugin-data path SHALL be reported.

#### Scenario: Remove keeps plugin data and reports it

- **WHEN** a plugin whose instance has plugin data is removed
- **THEN** its owned outputs MUST be removed, its plugin data directory MUST
  remain, and the command MUST print the retained path

#### Scenario: Repository removal leaves the machine instance

- **WHEN** the same plugin is installed at machine scope and repository
  scope, and the repository selection is removed
- **THEN** the machine-scope instance MUST stay installed and registered
