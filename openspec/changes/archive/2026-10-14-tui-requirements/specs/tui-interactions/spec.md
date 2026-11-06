## ADDED Requirements

### Requirement: Prerequisite-aware toggle off

When expansion is enabled, toggling off an implied resource, or a directly selected resource that a selected resource still requires, SHALL remove any direct selection and write the effective explicit exclusion through the existing serialized configuration mutation, without a confirmation dialog. The footer SHALL then show each requiring resource's resulting unmet prerequisite warning. When expansion is disabled, toggling off SHALL NOT write an exclusion solely because of the prerequisite. Existing required-selection policy and scope authorization MUST NOT be bypassed, and an exclusion written for one scope MUST NOT change another scope's selection.

#### Scenario: Enabled toggle off excludes a still-required resource

- **WHEN** expansion is enabled and the user toggles off an implied resource or a directly selected resource that a selected resource still requires
- **THEN** the TUI MUST remove any direct selection and write the effective explicit exclusion through the serialized configuration mutation without a confirmation dialog
- **AND** the footer MUST show each requiring resource's resulting unmet prerequisite warning

#### Scenario: Advisory toggle off writes no prerequisite exclusion

- **WHEN** expansion is disabled and the user toggles off a directly selected resource that a selected resource requires
- **THEN** the TUI MUST remove the direct selection
- **AND** it MUST NOT write an exclusion solely because of the prerequisite

#### Scenario: Toggle off keeps policy and scope authorization

- **WHEN** required-selection policy or scope authorization would prevent the toggle-off mutation, or the resource is selected by another scope
- **THEN** prerequisite handling MUST NOT bypass that policy or authorization
- **AND** it MUST NOT write an exclusion that changes the other scope's selection

### Requirement: Prerequisite toggle-on feedback

After toggling a resource on, the footer SHALL note each prerequisite resource that came along with it as `also selected <kind> "<name>" (required by <kind> "<name>")` when expansion is enabled, or as `would also select <kind> "<name>" (required by <kind> "<name>")` when expansion is disabled. Nothing extra SHALL be shown when no prerequisite came along.

#### Scenario: Enabled toggle on reports also-selected prerequisites

- **WHEN** expansion is enabled and toggling a resource on selects implied prerequisites
- **THEN** the footer MUST note each one as also selected, naming the requiring resource

#### Scenario: Advisory toggle on reports would-also-select prerequisites

- **WHEN** expansion is disabled and toggling a resource on adds prerequisites to the implied preview
- **THEN** the footer MUST note each one as would also select, naming the requiring resource

#### Scenario: Toggle on without prerequisites adds no note

- **WHEN** toggling a resource on brings along no prerequisite
- **THEN** the footer MUST show no prerequisite note
