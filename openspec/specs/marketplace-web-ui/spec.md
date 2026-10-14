# marketplace-web-ui Specification

## Purpose
Defines the Marketplace web interface's persistent chrome and primary resource-detail actions.

## Requirements

### Requirement: Persistent Marketplace header
The Marketplace SHALL keep its primary header visible at the top of the viewport while users scroll, without removing the header from normal document flow.

#### Scenario: Header remains visible while scrolling
- **WHEN** a user scrolls a Marketplace page at a desktop or mobile viewport size
- **THEN** the primary header remains aligned with the top of the viewport

### Requirement: Marketplace footer identity
The Marketplace SHALL show its running version, documentation link, and Adyen attribution in the page footer.

#### Scenario: Footer identifies the running Marketplace
- **WHEN** a user views the Marketplace homepage
- **THEN** the footer shows the running version, Docs link, and Adyen attribution

### Requirement: Resource installation presentation
The Marketplace SHALL present compact revision metadata and actionable installation controls for installable resources. The revision copy action SHALL copy the full digest, and command copying SHALL exclude the decorative prompt.

#### Scenario: Installable resource exposes compact actions
- **WHEN** a user views an installable resource
- **THEN** revision identity, scan date, setup documentation, command, and copy actions are available
- **AND** copy actions use the full revision and command values

### Requirement: Compact unavailable installation notice
When installation is unavailable, the Marketplace SHALL replace setup and command controls and duplicate warnings with one concise reason-specific notice. A security-policy notice SHALL offer an action that activates Security details.

#### Scenario: Policy-blocked resource links to security details
- **WHEN** a resource cannot be installed because of the Marketplace security policy
- **THEN** one Not available notice explains the reason without setup, command, copy, or duplicate warning controls
- **AND** activating View security details selects and displays the Security tab

#### Scenario: Other unavailable reasons remain concise
- **WHEN** a resource is revoked or otherwise unavailable for installation
- **THEN** one Not available notice gives the applicable concise reason without setup, command, copy, duplicate warning controls, or a security-details action

### Requirement: Resource prerequisite presentation

When the latest resource detail response includes `requires`, the resource page SHALL show a "Requires" list in response order. Each entry SHALL show its kind and name and exactly one badge: `resolved` when the entry includes `resolved`, linking to that resource's latest page; `external` when `resolved` is absent and a `registry` hint is present; and `unresolved` otherwise. An empty `requires` array SHALL state that the resource declares no requirements. Registry-authored hint text SHALL render as plain text and MUST NOT produce markup or links. When `required_by` is present, the page SHALL show a "Required by" list linking each dependent to its latest resource page, and an empty array SHALL show "none". When `requires` is present and `required_by` is absent, the page SHALL state that dependents are unknown because some revisions are not yet indexed. When `requires` is absent, neither section SHALL be shown and existing page behavior SHALL remain unchanged.

#### Scenario: Requires entries carry one badge each

- **WHEN** the latest resource detail response has prerequisites that are resolved, unresolved with a registry hint, and unresolved without a hint
- **THEN** the Requires list MUST show them in response order with the `resolved`, `external`, and `unresolved` badges respectively
- **AND** only the resolved entry MUST link to a resource page

#### Scenario: Hint text renders as plain text

- **WHEN** a prerequisite's registry or path hint contains markup
- **THEN** the page MUST render it as literal text without creating elements or links

#### Scenario: Required by shows dependents, none, or unknown

- **WHEN** the response has a non-empty `required_by`, an empty `required_by`, or `requires` without `required_by`
- **THEN** the page MUST link each dependent, show "none", or state that dependents are unknown, respectively

#### Scenario: Absent prerequisite data shows no sections

- **WHEN** the response has no `requires`, including on a revision-by-digest page
- **THEN** neither the Requires nor the Required by section MUST be shown

### Requirement: Conditional prerequisite install note

When a resource is installable and declares at least one prerequisite, the installation panel SHALL add the note "Requires N resource(s). agent-marketplace selects them automatically when `requires.expand` is enabled." with the correct singular or plural noun. The copied install command SHALL remain unchanged. No note SHALL be shown when the resource declares no prerequisites or installation is unavailable.

#### Scenario: Installable resource with prerequisites shows the note

- **WHEN** an installable resource declares one prerequisite, or several
- **THEN** the installation panel MUST show the note with the singular or plural noun respectively
- **AND** the copied command MUST be unchanged

#### Scenario: No note without prerequisites or installation

- **WHEN** a resource declares no prerequisites, or installation is unavailable
- **THEN** no prerequisite note MUST be shown and the compact unavailable notice MUST remain unchanged
