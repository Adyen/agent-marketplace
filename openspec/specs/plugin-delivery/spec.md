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

### Requirement: Delivery is chosen per plugin request

A plugin request's `delivery` SHALL be `auto`, `native`, or `decompose`,
defaulting to `auto`, and SHALL apply to every harness target the request is
delivered to. `auto` SHALL choose native delivery for a harness target
exactly when its shipped catalog declares native delivery verified, and
decomposed delivery otherwise. Sync SHALL report the delivery chosen per
harness target.

#### Scenario: Auto decomposes where native delivery is unverified

- **WHEN** a plugin with `delivery: auto` is synced to Cursor and to a harness
  target whose catalog declares native delivery verified
- **THEN** Cursor MUST receive decomposed members, the other harness target
  MUST receive a native registration, and sync output MUST name both choices

#### Scenario: Native delivery never falls back to decomposed output

- **WHEN** a plugin with `delivery: native` is synced to a harness target
  whose catalog does not declare native delivery verified
- **THEN** that instance MUST fail with `plugin.delivery.native-unverified`
  and no decomposed members MUST be written for it

### Requirement: Native registration uses owned entries only

Native delivery SHALL publish the package generation into a native plugin
catalog owned by Agent Marketplace and SHALL register it only through owned
entries in the harness target's native settings. Handwritten entries, keys,
order, and comments SHALL be preserved. Entries sharing a file with hooks or
MCP servers SHALL be written in one combined mutation. Agent Marketplace
SHALL NOT write trust state, vendor-managed policy, or private runtime
plugin caches. Claude Code registration SHALL be written to
`.claude/settings.local.json` at repository scope and to
`~/.claude/settings.json` at user scope, and the Claude catalog SHALL be a
marketplace file Claude Code accepts, naming an owner and listing each
plugin with a source relative to the catalog root.

#### Scenario: Claude registration preserves handwritten settings

- **WHEN** a plugin is delivered natively to Claude Code at repository scope,
  `.claude/settings.local.json` has handwritten `enabledPlugins` entries, and
  `.claude/settings.json` has owned hooks
- **THEN** `.claude/settings.local.json` MUST gain the owned registration
  entries and keep every handwritten entry and comment, and
  `.claude/settings.json` MUST keep the owned hooks and gain no registration
  entry

#### Scenario: Claude catalog is a marketplace Claude Code accepts

- **WHEN** a plugin is delivered natively to Claude Code
- **THEN** the owned catalog's `.claude-plugin/marketplace.json` MUST name the
  catalog and an owner and list the plugin with a source relative to the
  catalog root that resolves to the plugin's package generation

#### Scenario: Claude registration moves out of shared project settings

- **WHEN** an owned Claude registration from an earlier sync is recorded in
  `.claude/settings.json` and sync runs
- **THEN** after that one sync the owned entries MUST be in
  `.claude/settings.local.json` and gone from `.claude/settings.json`, the
  plugin MUST be registered exactly once, and entries without ownership state
  MUST be left unchanged

#### Scenario: Registration without ownership state is not pruned

- **WHEN** a registration entry matching Agent Marketplace's naming exists
  but ownership state does not record it
- **THEN** sync MUST leave it unchanged and report the collision

#### Scenario: Codex registration writes no trust

- **WHEN** a plugin is delivered natively to Codex
- **THEN** `config.toml` MUST gain only the owned plugin entries and no
  project trust entry

#### Scenario: Reload requirement is reported

- **WHEN** a native generation changes for a harness target whose catalog
  declares that refresh requires a restart
- **THEN** sync and status MUST report `reload-required` for that instance

### Requirement: Delivery transitions never activate a plugin twice

Changing a plugin instance between native and decomposed delivery SHALL
stage the new delivery first. It SHALL then retire only proven owned
outputs and registrations of the old delivery, in one recoverable sequence.

#### Scenario: Switching to decompose removes the native registration

- **WHEN** `delivery` changes from `native` to `decompose` and sync completes
- **THEN** the decomposed members MUST be installed and the owned native
  registration entries MUST be gone

#### Scenario: Interrupted delivery switch recovers without duplicates

- **WHEN** sync is interrupted after staging the new delivery and before
  retiring the old one
- **THEN** the next recovery-capable command MUST complete the switch, and no
  state with both deliveries active MUST remain
