## ADDED Requirements

### Requirement: Change proposals carry a release note

Every `openspec/changes/**/proposal.md` that a pull request adds SHALL contain a `## Release note` section with a non-empty body. The body SHALL be user-facing sentences, text starting `Breaking:` that states what breaks and the upgrade steps, or `none` followed by a reason. A `none` body without a reason SHALL fail the gate. Proposals that already exist on the base commit SHALL NOT be checked.

#### Scenario: Proposal without release note fails

- **WHEN** a pull request adds a change proposal without a `## Release note` section, with an empty section, or whose note is `Breaking:` with no text
- **THEN** the gate MUST fail and name the proposal

#### Scenario: Release note none without reason fails

- **WHEN** a pull request adds a change proposal whose release note is `none` with no reason
- **THEN** the gate MUST fail and ask for `none — <reason>`

#### Scenario: Release note none with reason passes

- **WHEN** a pull request adds a change proposal whose release note is `none — <reason>` or `none - <reason>`
- **THEN** the gate MUST accept the proposal

#### Scenario: Pre-existing proposals are not checked

- **WHEN** a pull request modifies a change proposal that already exists on the base commit and lacks a release note
- **THEN** the gate MUST NOT report it
