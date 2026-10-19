## MODIFIED Requirements

### Requirement: Update refuses atomically when a selection stops resolving

Before `update` publishes any acquisition lock or configuration change, it SHALL resolve every explicit selection against the new revision. An explicit selection is a direct selection, a `required: true` entry, a selected collection, or a member that the new collection definition still lists. When a selection that resolved at the previously locked revision does not resolve at the new revision, `update` SHALL exit 1, SHALL name each such selection exactly once using the `<kind> "<name>" was selected but not found in <source>` wording, each followed by one remediation line. For a selection that is not required, the remediation line SHALL name `agent-marketplace remove --kind <kind> <name>` to run before rerunning `update`, and `agent-marketplace install <new-name>` for a renamed resource. For a required selection, the remediation line SHALL state that `agent-marketplace remove` cannot drop it and that the configuration entry marking it required must change. When the previously locked revision of a lock cannot be loaded, the refusal SHALL name that lock and the load error, and selections it could not compare SHALL count as newly missing. The refusal SHALL end with the line `update left the lock and harness outputs unchanged; rerun 'agent-marketplace update' after resolving the selections above, or update other registries with 'agent-marketplace update --registry <url>'`. The refusal MUST leave acquisition lock, configuration, and harness output bytes and file modes unchanged and MUST NOT leave a pending dependency transaction. A selection that already did not resolve at the previously locked revision, an `all: true` selection whose resource names change, and a member removed from a collection definition SHALL NOT cause a refusal.

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
