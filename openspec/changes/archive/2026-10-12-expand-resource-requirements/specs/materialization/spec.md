## ADDED Requirements

### Requirement: Advisory implied previews preserve only existing scoped materialization

When resource-requirement expansion is disabled, materialization SHALL tolerate existing outputs that correspond to an implied-preview resource admitted for that scope and harness target. Preview preservation MUST remain separate from desired writes and retain only the matching existing managed file, link, manifest path, or aggregate identity and its existing ownership evidence. It MUST NOT create missing preview outputs, rewrite or overwrite present preview values, adopt handwritten content, change ownership for other entries, or suppress pruning of unrelated stale outputs. These operations MUST leave healthy acquisition-lock bytes unchanged.

#### Scenario: Preview preserves existing owned file and link paths

- **WHEN** an implied-preview resource has an existing managed file or cache-backed link in an admitted scope and harness target
- **THEN** sync and check MUST preserve that exact output path and its ownership evidence without rewriting or overwriting it
- **AND** neither operation MUST create a missing preview output

#### Scenario: Preview preserves existing aggregate identity without adoption

- **WHEN** a preview resource corresponds to an existing shared-configuration aggregate identity
- **THEN** materialization MUST preserve that identity's current value and existing ownership evidence without treating it as a desired write
- **AND** a handwritten entry MUST remain unmanaged and MUST NOT be adopted

#### Scenario: Preview retention does not suppress unrelated pruning

- **WHEN** one owned output corresponds to an admitted implied preview and a different owned output is stale and unrelated to any preview
- **THEN** materialization MUST retain only the preview output and MUST prune the unrelated stale output under existing ownership rules

#### Scenario: Removing the last requiring selection ends preview retention

- **WHEN** no selected resource requires an output that was previously present only in the advisory implied preview
- **THEN** the next synchronization MUST stop retaining that output as a preview and apply ordinary stale-output pruning

#### Scenario: Preview preservation does not weaken fail-closed ownership or aggregate handling

- **WHEN** ownership evidence is missing or corrupt, or a shared aggregate is malformed, while a preview would otherwise match
- **THEN** preview handling MUST follow existing fail-closed safety behavior without inferring ownership or adopting an entry

#### Scenario: Plain sync preserves healthy acquisition lock bytes

- **WHEN** plain sync runs with a healthy acquisition lock and existing outputs for admitted implied-preview resources
- **THEN** preview preservation MUST leave the acquisition lock byte-identical

#### Scenario: Locked sync tolerates existing preview outputs

- **WHEN** `sync --locked` runs with existing materialized outputs for admitted implied-preview resources
- **THEN** those outputs MUST be tolerated without creating or rewriting preview outputs
- **AND** the healthy acquisition lock MUST remain byte-identical

#### Scenario: Check-only sync tolerates existing preview outputs

- **WHEN** `sync --check` runs with existing materialized outputs for admitted implied-preview resources
- **THEN** it MUST report no drift solely because those outputs are not actual selected resources
- **AND** it MUST NOT create or rewrite preview outputs
- **AND** the healthy acquisition lock MUST remain byte-identical
