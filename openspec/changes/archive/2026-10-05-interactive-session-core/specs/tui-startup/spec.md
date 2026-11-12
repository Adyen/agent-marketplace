## REMOVED Requirements

### Requirement: Progressive staged snapshot delivery

**Reason**: Moved whole to `session-startup`.

**Migration**: Move test annotations to `session-startup/startup-snapshot-delivers-config-inventory-then-assessed-stages`, `session-startup/invalid-configuration-delivers-one-fail-closed-final-stage`, and `session-startup/config-stage-starts-cached-first-marketplace-discovery-before-local-verification-finishes`.

### Requirement: Verification-pending provisional rows

**Reason**: Split between surface-neutral row behavior in `session-startup` and the TUI-specific row-modal and configuration-layer residual added below.

**Migration**: Move annotations for neutral rows to `session-startup/provisional-rows-show-configured-selection-state`, `session-startup/excluded-configured-selections-render-unchecked-in-their-verified-position`, `session-startup/user-scope-local-all-true-selections-expand-into-pending-rows`, `session-startup/provisional-rows-render-while-transient-acquisition-is-blocked`, `session-startup/excluded-only-configuration-publishes-no-provisional-rows`, `session-startup/provisional-layout-matches-verified-inventory`, `session-startup/optional-collection-member-yields-to-a-configured-selection-of-the-same-resource`, `session-startup/registry-selections-carry-the-harness-override-badge`, `session-startup/organization-marketplace-collection-expands-from-the-verified-cached-manifest`, and `session-startup/authoritative-failure-clears-provisional-rows`. Keep TUI-only annotations for the modal and repository/personal-local exclusion scenarios under `tui-startup`.

### Requirement: Browse-safe configuration during transient acquisition

**Reason**: Moved whole to `session-startup`.

**Migration**: Move test annotations to `session-startup/browse-safe-config-publishes-the-verified-endpoint-before-acquisition`, `session-startup/provisional-config-publishes-configured-pins-before-acquisition`, `session-startup/browse-safe-config-is-never-published-for-unsafe-configurations`, and `session-startup/cached-marketplace-rows-render-while-acquisition-is-blocked`.

### Requirement: Verified inventory and assessed handoff

**Reason**: Split between surface-neutral inventory behavior in `session-startup` and the TUI startup-footer residual added below.

**Migration**: Move test annotations to `session-startup/verified-inventory-is-navigable-while-drift-is-pending`, `session-startup/drift-applies-only-to-rows-of-its-own-catalog-selection`, `session-startup/final-stage-replaces-pending-inventory-with-the-assessment`, and `session-startup/final-stage-merges-discovery-published-during-the-config-stage`. Keep the footer-progress annotation under `tui-startup`.

### Requirement: Selection and detail anchoring across stages

**Reason**: Split between surface-neutral resource/viewport identity in `session-startup` and TUI screen-line/cursor anchoring added below.

**Migration**: Move annotations to `session-startup/selection-follows-its-resource-when-rows-are-inserted`, `session-startup/untouched-selection-remains-at-the-top-as-rows-arrive`, and `session-startup/an-open-detail-follows-its-verified-row`. Keep screen-line and cursor annotations under `tui-startup`.

### Requirement: Generation isolation and mutation gating

**Reason**: Split between surface-neutral generation/action gating in `session-startup` and TUI keyboard gating added below.

**Migration**: Move annotations to `session-startup/superseded-stage-delivery-cannot-overwrite-newer-state`, `session-startup/a-stale-provisional-stage-cannot-publish-rows`, `session-startup/an-intermediate-stage-cannot-complete-a-pending-mutation-reload`, and `session-startup/mutation-actions-remain-gated-until-verified-inventory`. Keep the keyboard-gating annotation under `tui-startup`.

### Requirement: Cached collection revision expansion

**Reason**: Moved whole to `session-startup`.

**Migration**: Move test annotations to `session-startup/cached-collection-leaves-prefer-the-exact-then-newest-verified-revision` and `session-startup/tampered-cached-manifests-are-rejected`.

### Requirement: Single-view provisional row continuity

**Reason**: Moved whole to `session-startup`.

**Migration**: Move test annotations to `session-startup/provisional-rows-retain-single-view-position-and-guarded-actions`.

### Requirement: Ordinary reloads retain the current view until final assessment

**Reason**: Moved whole to `session-startup`.

**Migration**: Move test annotations to `session-startup/ordinary-reload-keeps-the-current-view-until-final-assessment`.

## ADDED Requirements

### Requirement: TUI provisional row guards and layered exclusions

The TUI SHALL retain its toggle-modal guard for verification-pending rows and apply committed and personal-local exclusions, including registry-scoped personal-local exclusions, before publishing provisional rows. Shared row status and read-only evidence behavior follow `session-startup`.

#### Scenario: Provisional config stage publishes verification-pending rows

- **WHEN** the validated configuration selects a Marketplace resource pin
- **THEN** the provisional stage MUST render a row for it marked verification-pending
- **AND** the row MUST show its configured check state and scope badge alongside an explicit verification-pending status that distinguishes configuration from installation
- **AND** the row MUST refuse to open the toggle modal until verified inventory replaces it without duplicates or fabricated badges

#### Scenario: Excluded configured selections render unchecked in their verified position

- **WHEN** the validated configuration contains an optional Marketplace pin that a user-level exclusion disables
- **THEN** the provisional stage MUST render that pin unchecked with an explicit excluded status in the position verified inventory will give it
- **AND** non-excluded pins MUST render checked
- **AND** the excluded pending row MUST refuse the toggle modal until verification

#### Scenario: Local all-true selections expand into pending rows on the normal config path

- **WHEN** validated configuration layers select every resource of local-directory registries with `all: true`
- **THEN** the provisional stage MUST expand those selections through a read-only local catalog ingest and render each discovered resource as a pending row with its scope badge
- **AND** configured committed and personal-local excludes MUST already be applied so excluded resources render no pending row
- **AND** the merged config stage that follows MUST preserve the pending rows

#### Scenario: Registry-scoped local excludes apply to provisional rows

- **WHEN** a personal-local configuration excludes a resource name only for specific registry entries
- **THEN** the provisional stage MUST suppress pending rows for the excluded registry occurrences only
- **AND** pending rows for the same resource name from registries not covered by the exclusion MUST still render

### Requirement: TUI startup progress footer

The TUI SHALL keep startup progress visible in its footer while verified inventory is navigable and drift remains pending, then clear progress when the final assessed stage arrives.

#### Scenario: Verified inventory is navigable and toggle-ready while drift is pending

- **WHEN** the inventory stage publishes verified rows
- **THEN** the model MUST leave the loading state, render the verified rows, and keep drift explicitly pending rather than `in sync` or assessed
- **AND** startup progress MUST remain visible in the footer
- **AND** otherwise-eligible config-only toggles on verified rows MUST be permitted

### Requirement: TUI stage selection anchoring

The TUI SHALL retain a selected row's terminal line when inventory inserts rows above it and SHALL keep an untouched cursor at the top during staged delivery. Surface-neutral selection and detail identity anchoring follow `session-startup`.

#### Scenario: Verified rows inserted above a selection keep it on the same screen line

- **WHEN** a pending row is selected and verified inventory inserts rows above it
- **THEN** the selection MUST follow the same resource
- **AND** the selected row MUST keep its line within the list viewport rather than shifting on screen

#### Scenario: An untouched cursor stays on top when installed rows arrive

- **WHEN** the user has not moved the cursor and discovery rows, then verified inventory rows, then the final stage arrive
- **THEN** the cursor and list offset MUST remain at the top through every stage

### Requirement: TUI keyboard mutation gating

The TUI SHALL keep keyboard mutations gated until verified inventory arrives. Snapshot generation isolation and non-keyboard mutation gating follow `session-startup`.

#### Scenario: Mutations stay gated until verified inventory

- **WHEN** only the config stage has been published
- **THEN** sync and update keys MUST NOT start a mutation
- **AND** pressing Space on a cached discovery row MUST NOT open the install scope modal
- **AND WHEN** the verified inventory stage arrives
- **THEN** sync MUST become eligible
