## ADDED Requirements

### Requirement: Prerequisite detail sections

The TUI detail pane SHALL show a "Requires" section listing each valid declared prerequisite of the shown resource in declaration order with exactly one marker: `met` when a selected resource with the same kind and name satisfies it, `implied` when an expansion-selected implied resource satisfies it, `preview` when expansion is disabled and the prerequisite is in the implied preview, `excluded` when its unmet reason is `excluded`, and `unmet` for any other unmet reason. The detail pane SHALL show a "Required by" section listing the selected dependents recorded in prerequisite provenance, never derived from collection provenance. A section with no entries SHALL NOT be shown. Malformed prerequisite declarations SHALL keep their existing diagnostic. Registry-authored text SHALL remain terminal-escaped and credential-bearing source hints SHALL be redacted before display.

#### Scenario: Requires section marks each prerequisite state

- **WHEN** a shown resource declares prerequisites that are met, implied, previewed, excluded, and unmet
- **THEN** the Requires section MUST list them in declaration order, each with its one corresponding marker

#### Scenario: Required by section uses prerequisite provenance

- **WHEN** a shown resource is required by selected resources and is also a collection member
- **THEN** the Required by section MUST list the requiring resources
- **AND** collection membership MUST remain shown as collection provenance and MUST NOT appear in Required by

#### Scenario: Resources without relationships show no prerequisite sections

- **WHEN** a shown resource has no valid declared prerequisites and no dependents, or only malformed declarations
- **THEN** neither section MUST be shown
- **AND** a malformed declaration MUST keep its existing diagnostic

#### Scenario: Prerequisite text is escaped and redacted

- **WHEN** a prerequisite or dependent carries terminal control sequences or a credential-bearing source hint
- **THEN** the detail pane MUST render the text terminal-escaped and the credential redacted

### Requirement: Prerequisite list markers

A TUI list row for an implied resource selected by expansion SHALL carry a `req` badge beside any existing collection badge. A row whose resource has an unmet prerequisite SHALL carry a warning marker. A resource present only in the implied preview SHALL NOT carry a `req` badge. Rows with no prerequisite relationship SHALL render unchanged.

#### Scenario: Implied and unmet rows are marked

- **WHEN** the list contains an implied resource selected by expansion and a resource with an unmet prerequisite
- **THEN** the implied row MUST carry a `req` badge and the requiring row MUST carry a warning marker

#### Scenario: Preview-only and unrelated rows are not marked

- **WHEN** expansion is disabled and a resource appears only in the implied preview, and another row has no prerequisite relationship
- **THEN** the preview resource MUST NOT carry a `req` badge
- **AND** the unrelated row MUST render unchanged
