## ADDED Requirements

### Requirement: Update refuses atomically when a selection stops resolving

Before `update` publishes any acquisition lock or configuration change, it SHALL resolve every explicit selection against the new revision. An explicit selection is a direct selection, a `required: true` entry, a selected collection, or a member that the new collection definition still lists. When a selection that resolved at the previously locked revision does not resolve at the new revision, `update` SHALL exit 1, SHALL name each such selection exactly once using the `<kind> "<name>" was selected but not found in <source>` wording, and SHALL then print the hint `update left the lock and harness outputs unchanged; run 'agent-marketplace sync --prune-stale' to drop missing selections, then 'agent-marketplace install <new-name>' for any renamed resource, and rerun 'agent-marketplace update'`. The refusal MUST leave acquisition lock, configuration, and harness output bytes and file modes unchanged and MUST NOT leave a pending dependency transaction. A selection that already did not resolve at the previously locked revision, an `all: true` selection whose resource names change, and a member removed from a collection definition SHALL NOT cause a refusal.

#### Scenario: Renamed or removed direct selection refuses update

- **WHEN** a repository configuration selects a skill directly from a Git source, and the upstream registry renames or removes that skill before `update` runs
- **THEN** `update` MUST exit 1 and name the skill with the `was selected but not found` wording followed by the update hint
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
