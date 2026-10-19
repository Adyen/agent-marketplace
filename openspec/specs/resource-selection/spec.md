# resource-selection Specification

## Purpose
Defines how selected resources with the same installed name are reconciled
within each install scope while required machine selections prevent repository shadowing.

## Requirements

### Requirement: Name collisions resolve per install scope

When different sources select resources of the same kind and installed name,
whether directly or through a collection, Agent Marketplace SHALL resolve
competing selections separately for machine and repository install scopes.
Sources in different scopes MUST NOT displace one another or produce a
name-collision warning solely because they share an installed name, except
when a required machine-scope selection prevents a different non-required
repository source from shadowing its installed name. A repository selection
from the required source itself does not shadow the required resource, and
whether it remains installed MUST NOT depend on how the shadowing source
locations sort. Sources competing within one scope SHALL retain deterministic
precedence and a name-collision warning for different registry sources.
Within one install scope, a direct selection takes precedence over a
collection member with the same kind and installed name, except that a
required selection outranks a non-required one whether either is selected
directly or through a collection. A required collection member that displaces
a non-required selection in its install scope SHALL produce a name-collision
warning that identifies the required source as the winner and each displaced
source location as a runner-up, unless the displaced selection is from the
required member's own source location.

#### Scenario: Repository winner does not displace a user-scope selection

- **WHEN** user configuration selects a non-required skill from a Git source and repository configuration selects a different skill with the same installed name from another source
- **THEN** repository `sync` MUST retain the user skill in the machine harness target and install the repository skill in the repository harness target
- **AND** `sync --check` MUST succeed from both the repository and the user's home after either sync order
- **AND** the complementary selections MUST NOT produce a name-collision warning

#### Scenario: Collection member is not displaced by a selection in the other scope

- **WHEN** configuration in one install scope selects a collection whose member has the same kind and installed name as a resource that configuration in the other install scope selects from another source, directly or through another collection
- **THEN** repository `sync` MUST install each resource in its own install scope's harness target
- **AND** the machine harness target MUST contain the same resources whether sync runs from the repository or from the user's home
- **AND** `sync --check` MUST succeed from both the repository and the user's home after either sync order
- **AND** the complementary selections MUST NOT produce a name-collision warning

#### Scenario: Same-scope collision keeps one winner

- **WHEN** different registry sources select resources of the same kind and installed name within one install scope
- **THEN** only the winning source MUST materialize that installed name in that scope
- **AND** a name-collision warning MUST identify only sources competing within that scope
- **AND** a selection with the same installed name in the other scope MUST remain available

#### Scenario: Required user-scope selection suppresses a repository selection

- **WHEN** a required user or organization source selects a resource and a different, non-required repository source selects the same kind and installed name
- **THEN** only the required resource MUST materialize, in the machine harness target
- **AND** the repository harness target MUST NOT contain a resource under that installed name
- **AND** a name-collision warning MUST identify the required source as the winner and the repository source as a runner-up
- **AND** repository `sync` MUST leave the machine output unchanged

#### Scenario: Required selection through a collection suppresses a repository selection

- **WHEN** a required user or organization source and a different, non-required repository source select resources of the same kind and installed name, and at least one of them is selected through a collection
- **THEN** repository `sync` MUST install the required resource in the machine harness target
- **AND** the repository harness target MUST NOT contain the repository source's resource
- **AND** a name-collision warning MUST identify the required source as the winner and the repository source as a runner-up
- **AND** the machine harness target MUST contain the same resources whether sync runs from the repository or from the user's home

#### Scenario: Required suppression keeps the repository copy from the required source

- **WHEN** a required user or organization source selects a resource, directly or through a collection, and repository configuration selects the same kind and installed name both from that required source and from a different, non-required source
- **THEN** the repository harness target MUST contain the resource from the required source, whatever the names of the competing source locations
- **AND** a name-collision warning MUST identify the required source as the winner and only the different source as a runner-up

#### Scenario: Required collection member displaces an optional selection in the same scope

- **WHEN** a required user or organization source selects a collection whose member has the same kind and installed name as a resource that a different, non-required source selects directly in the same install scope
- **THEN** only the required collection member MUST materialize under that installed name in the machine harness target, whichever source is configured first
- **AND** a name-collision warning MUST identify the required source as the winner and the non-required source as a runner-up
- **AND** `sync --check` MUST succeed from both the repository and the user's home after sync
- **AND** a non-required collection member MUST NOT displace a direct selection in its install scope

### Requirement: Required selection misses stay hard across layers

When several configured entries for the same source location (URL, ref, and path) select a resource that the location does not provide, the miss SHALL be reported once and SHALL be required whenever any of those entries is required, regardless of layer order. A required miss SHALL remain an error rather than a warning in `sync`, `status`, and the TUI.

#### Scenario: Optional entry ordered before a required entry for the same missing resource

- **WHEN** an earlier configuration layer selects a missing resource from a source location without `required`, and a later layer selects the same resource from the same location with `required: true`
- **THEN** exactly one not-found diagnostic MUST be reported for the resource and it MUST be required
- **AND** the TUI MUST NOT assess drift as ready, MUST show the row as a hard unavailable row, and a reload of that snapshot MUST fail

### Requirement: Unqualified collection members retain the resolved owner

A collection member without an explicit registry SHALL retain the source location, configuration layer, required policy, and effective harness targets of the resolved collection definition that contributes it. Another configured entry for the same registry location MUST NOT rebind that member. Existing higher-authority exclusions and independent nested collection definition selection SHALL retain their semantics.

#### Scenario: Unqualified member retains the collection owner

- **GIVEN** several consumer configuration entries refer to the same registry location
- **WHEN** one entry selects a collection with a member that has no explicit registry
- **THEN** the member MUST materialize only in the selected collection owner's install scope and effective harness targets
- **AND** include-file precedence and root versus subtree locations MUST NOT cause the member to inherit an unrelated owner's policy
- **AND** `sync --check` MUST succeed after synchronization

#### Scenario: Same collection remains selected in both install scopes

- **GIVEN** machine and repository configuration select the same collection from the same registry location
- **WHEN** its unqualified members are synchronized
- **THEN** each install scope MUST retain its own member copies
- **AND** those copies MUST NOT produce a name-collision warning solely because they share an installed name
- **AND** `sync --check` MUST succeed after synchronization

#### Scenario: Unqualified required member retains required policy

- **GIVEN** a required collection definition and an optional entry share a registry location
- **WHEN** the required collection contributes a member without an explicit registry
- **THEN** the member MUST retain required policy and the resolved definition's install scope and effective harness targets
- **AND** personal exclusions MUST NOT suppress that member
- **AND** a missing or malformed member MUST retain a required diagnostic

#### Scenario: Nested organization member retains harness diagnostics

- **GIVEN** a collection includes a nested definition owned by organization configuration
- **WHEN** the containing collection narrows that definition's effective harness targets so an unqualified member has no compatible target
- **THEN** the member MUST NOT materialize through that collection
- **AND** its existing required error or organization default warning MUST remain visible

### Requirement: Source-bound collection requests preserve required selection

Consumer collection selections SHALL be authored as `collection/<name>` selectors in v4 `resources` groups bound to declared sources, not as legacy top-level collection requests. Selecting a collection MUST NOT disable another group's required selections. A required Git or directory group with `all: true` SHALL retain its catalog selection; an explicitly authored required subset SHALL remain a subset. `required: true` alone MUST NOT select any resource or implicitly select the catalog.

#### Scenario: Collection keeps required all selection

- **GIVEN** an organization v4 resource group selects its source catalog with `all: true` and `required: true`
- **WHEN** a repository v4 resource group selects `collection/shared` from its declared source
- **THEN** all selected organization resources MUST remain required and materialized in machine scope
- **AND** the requested collection MUST expand in repository scope without a spurious missing-collection diagnostic

#### Scenario: Collection preserves authored required subset

- **GIVEN** an organization v4 resource group explicitly selects a required subset of its source catalog
- **WHEN** a repository v4 resource group selects a collection from its declared source
- **THEN** the explicitly selected required resources and collection members MUST remain selected in their respective scopes
- **AND** unrelated organization resources MUST NOT become selected solely because the collection is selected

#### Scenario: Required policy alone selects no resources

- **WHEN** a v4 Git or directory resource group declares `required: true` without `include`, `tags`, or `all: true`
- **THEN** that group MUST select no resources
- **AND** selecting a collection from another group MUST NOT activate that unselected catalog

### Requirement: Update refuses atomically when a selection stops resolving

Before `update` publishes any acquisition lock or configuration change, it SHALL resolve every explicit selection against the new revision. An explicit selection is a direct selection, a `required: true` entry, a selected collection, or a member that the new collection definition still lists. When a selection that resolved at the previously locked revision does not resolve at the new revision, `update` SHALL exit 1, SHALL name each such selection exactly once using the `<kind> "<name>" was selected but not found in <source>` wording, each followed by one remediation line. For a selection that is not required, the remediation line SHALL name `agent-marketplace remove --kind <kind> <name>` to run before rerunning `update`, and `agent-marketplace install <new-name>` for a renamed resource. For a required selection, the remediation line SHALL state that `agent-marketplace remove` cannot drop it and that the configuration entry marking it required must change. When the previously locked revision of a lock cannot be loaded, the refusal SHALL name that lock and the load error, and selections it could not compare SHALL count as newly missing. The refusal SHALL end with the line `update left the lock and harness outputs unchanged; rerun 'agent-marketplace update' after resolving the selections above, or update other registries with 'agent-marketplace update --registry <url>'`. The refusal MUST leave acquisition lock, configuration, and harness output bytes and file modes unchanged and MUST NOT leave a pending dependency transaction. A selection that already did not resolve at the previously locked revision, an `all: true` selection whose resource names change, and a member removed from a collection definition SHALL NOT cause a refusal. An update started from the TUI SHALL apply the same check before publication, SHALL fail with the same refusal text, and SHALL leave the same bytes and file modes unchanged; the TUI then reloads from the unchanged lock.

#### Scenario: Renamed or removed direct selection refuses update

- **WHEN** a repository configuration selects a skill directly from a Git source, and the upstream registry renames or removes that skill before `update` runs
- **THEN** `update` MUST exit 1 and name the skill with the `was selected but not found` wording followed by its remediation line and the closing update line
- **AND** the acquisition lock, the configuration, and the materialized harness outputs MUST be byte-identical with unchanged file modes, and no dependency transaction MUST remain pending

#### Scenario: Missing required selection leaves the lock unchanged

- **WHEN** a configuration entry with `required: true` selects a skill that the upstream registry renames before `update` runs
- **THEN** `update` MUST exit 1 with the refusal, MUST NOT report a `dependency-committed sync failure`, and MUST leave the acquisition lock unchanged
- **AND** a following `sync --check` MUST exit 0 and report the outputs in sync

#### Scenario: Missing collection or listed collection member refuses update

- **WHEN** a configuration selects a collection and the new revision either removes the collection or renames a member that the collection definition still lists
- **THEN** `update` MUST exit 1, name the missing collection or member, and leave the acquisition lock and harness outputs unchanged

#### Scenario: Collection membership changes that resolve do not refuse update

- **WHEN** a configuration selects a collection and the new revision removes a member from the collection definition, or renames a member and updates the collection definition to the new name
- **THEN** `update` MUST exit 0 and advance the acquisition lock

#### Scenario: All-resources selection follows upstream renames

- **WHEN** a configuration selects a Git source with `all: true`, with or without an `exclude` naming a skill that the new revision no longer contains, and the upstream registry renames a skill
- **THEN** `update` MUST exit 0, advance the acquisition lock, and materialize the renamed skill

#### Scenario: Every newly missing selection is named once

- **WHEN** two different selections stop resolving at the new revision, and one of them is selected from the same source location by both the user and the repository configuration
- **THEN** the refusal MUST name each missing selection exactly once

#### Scenario: Selection already missing before update does not refuse

- **WHEN** a configuration selects a skill that did not resolve at the previously locked revision, and the upstream registry makes an unrelated change
- **THEN** `update` MUST exit 0 and advance the acquisition lock
- **AND** a following `status` MUST still report the stale selection as a warning

#### Scenario: Following the remediation completes a rename

- **WHEN** a directly selected skill is renamed upstream and `update` refuses with the remediation line naming `agent-marketplace remove --kind skill <old-name>`
- **THEN** running that `remove` command MUST exit 0, a following `update` MUST exit 0 and advance the acquisition lock, and `agent-marketplace install <new-name>` MUST exit 0 and add the renamed skill to the configuration

#### Scenario: Following the remediation excludes a missing collection member

- **WHEN** a selected collection still lists a member that the upstream registry renamed, and `update` refuses with the remediation line naming that member
- **THEN** running the named `remove` command MUST exit 0 and a following `update` MUST exit 0 and advance the acquisition lock

#### Scenario: Required selection remediation does not suggest remove

- **WHEN** a `required: true` selection stops resolving at the new revision
- **THEN** the refusal MUST state that `agent-marketplace remove` cannot drop it and MUST NOT name an `agent-marketplace remove --kind` command for it

#### Scenario: Unloadable prior revision is named in the refusal

- **WHEN** a configuration selects a skill that never existed upstream, and the previously locked commit can no longer be loaded when `update` runs
- **THEN** `update` MUST exit 1, name the skill, and name the lock whose previous revision could not be loaded

#### Scenario: Targeted update of another registry proceeds

- **WHEN** two Git sources are configured, a selection from the first stops resolving upstream, and the second has a new commit
- **THEN** `update` MUST refuse, and `update --registry <second-url>` MUST exit 0, advance the second source's lock, and leave the first source's lock unchanged

#### Scenario: TUI update refuses a newly missing selection

- **WHEN** a configuration selects a skill directly from a Git source, the upstream registry renames or removes that skill, and the user starts an update from the TUI
- **THEN** the TUI update MUST fail with the refusal, naming the skill with the `was selected but not found` wording followed by its remediation line and the closing update line
- **AND** the acquisition lock, the configuration, and the materialized harness outputs MUST be byte-identical with unchanged file modes, and no dependency transaction MUST remain pending

#### Scenario: TUI update proceeds when no selection newly stops resolving

- **WHEN** the user starts an update from the TUI and the only missing selection already did not resolve at the previously locked revision, an `all: true` selection's resource names change, or a member is removed from a selected collection's definition
- **THEN** the TUI update MUST succeed and advance the acquisition lock

### Requirement: Target harnesses resolve per selecting scope

Output selected by the repository or personal-local configuration MUST use the repository harness list when it is non-empty, otherwise the user list, otherwise the organization list. Output selected by the user or organization configuration MUST use the user harness list when it is non-empty, otherwise the organization list. Repository and personal-local configuration MUST NOT narrow or replace that machine list. Only when neither the user nor the organization configuration declares targets MUST their selections use the repository list. A collection's members MUST use the list of the layer that selected the collection. Per-source group `harnesses` MUST keep narrowing the applicable list and MUST never add a harness. `sync` and `deinit --scope user` MUST apply this rule when computing the output they install, keep or remove. When the two lists differ, `status` MUST report both, each with the layer that supplied it.

#### Scenario: Repository harness list does not narrow machine-scope output

- **WHEN** the user and organization configurations declare `harnesses: [factory]` with selected skills, and a repository declares `harnesses: [claude]` with its own selected skill
- **THEN** `sync` inside that repository MUST keep the user and organization skills installed for Factory in machine scope
- **AND** the repository skill MUST be installed only for Claude in the repository
- **AND** `status` MUST report the repository list from the repository layer and the machine list from the user layer

#### Scenario: Machine-scope teardown preserves organization output for machine targets

- **WHEN** `deinit --scope user` runs inside a repository whose `harnesses:` list omits a target that the organization's list declares
- **THEN** organization output for that target MUST remain installed, while user-owned machine output MUST be removed

#### Scenario: Machine-scope output falls back to the repository list

- **WHEN** neither the user nor the organization configuration declares `harnesses`, and a repository declares `harnesses: [claude]`
- **THEN** `sync` inside that repository MUST install selected user skills for Claude in machine scope

### Requirement: Ref bumping leaves exact commit pins unchanged

`update --bump` SHALL treat a source ref that is a full Git object ID, 40 hexadecimal characters or 64 in a SHA-256 repository in either letter case, as an exact commit pin. It MUST NOT rewrite that ref, MUST NOT count it in `refs bumped`, and MUST keep the acquisition lock on that commit, whatever tags the registry publishes. Every other ref SHALL keep its existing bump behavior.

#### Scenario: Ref bumping leaves a full commit pin unchanged

- **WHEN** a repository configuration pins one Git source to a full commit ID that ends in decimal digits, the registry publishes a tag named after the commit ID's non-numeric prefix followed by a larger number, and another source of the same registry uses ref `v0` while tag `v1` exists
- **THEN** `update --bump` MUST rewrite `v0` to `v1` and report `refs bumped: 1`
- **AND** the commit pin's configuration line, its acquisition lock commit and its installed resource MUST be unchanged
