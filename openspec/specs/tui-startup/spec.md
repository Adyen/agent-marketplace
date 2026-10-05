# tui-startup Specification

## Purpose
Describes the observable progressive startup behavior of the interactive TUI: how the startup snapshot is delivered in stages under one request generation, how configured selections render as verification-pending rows before verification completes, how Marketplace browsing proceeds cache-first while transient acquisition is blocked, and how stale or superseded stages are isolated from newer state. Selection precedence, acquisition locks, and configuration layering are governed by the `config-layering`, `acquisition-lock-codec`, `marketplace-api`, and `owned-entry-reconciliation` capabilities; this capability specifies only the TUI's staging and presentation of their outcomes.

## Requirements

### Requirement: Versioned per-user view preferences

The TUI SHALL read view settings once at startup from `$XDG_STATE_HOME/agent-marketplace/tui.json`, falling back to `~/.local/state/agent-marketplace/tui.json`. Version 1 SHALL store `view.groupBy` and `view.thenBy`, with Status and None defaults. Fold state SHALL remain session-only. Only acceptance of changed settings SHALL write this file, using atomic replacement. Preference loading and saving SHALL NOT access authored configuration or acquisition locks. A missing file SHALL use defaults silently. An unreadable, malformed, unsupported-version, or invalid-axis file SHALL use defaults with one footer warning and SHALL remain untouched until the user confirms changed settings. A save failure SHALL be visible and SHALL preserve the previous file without failing the TUI.

#### Scenario: Confirmed preferences survive restart without config writes

- **WHEN** a user confirms changed grouping settings and restarts with an XDG state root or the home-directory fallback
- **THEN** the versioned preference file SHALL load the saved grouping, folds SHALL not persist, and authored config and acquisition locks SHALL remain untouched

#### Scenario: Missing invalid unreadable and failed-save preference files

- **WHEN** the preference file is missing, malformed, unsupported, invalid, or unreadable, the user cancels or accepts unchanged preferences, or an atomic save fails
- **THEN** startup SHALL continue with defaults and a warning only for a bad existing file, cancellation and unchanged acceptance SHALL write nothing, and failure SHALL leave existing bytes unchanged with visible feedback

### Requirement: Missing optional selections are warnings

When a non-required, non-collection selection names a resource that a successfully loaded registry does not provide, the TUI SHALL show the selection as an unavailable row with its not-found reason and a warning diagnostic, and SHALL still assess drift for the remaining resources. Such a selection SHALL NOT fail the snapshot's authoritative assessment, mark rows stale, or turn a sync, update, install, or toggle reload into a failure. Required or collection selections that are not found, and selections whose registry catalog failed to load, SHALL continue to fail the assessment. An update refusal for a selection that resolved at the previously locked revision and stops resolving at the new one happens before publication, as `resource-selection` requires, and is not a reload failure under this requirement.

#### Scenario: Optional missing selection keeps the assessment

- **WHEN** the final stage resolves a configuration in which a loaded registry lacks a non-required selected skill
- **THEN** the missing skill MUST render as an unavailable row carrying its not-found reason, with a warning diagnostic
- **AND** drift MUST be assessed for the other resources and the footer MUST NOT report drift unavailable
- **AND** a sync reload of that snapshot MUST complete as success with warnings rather than failing verification

#### Scenario: Unverifiable selections still fail the assessment

- **WHEN** a required selection is not found, or a selected resource's registry catalog fails to load
- **THEN** the final stage MUST NOT report drift as assessed and a reload of that snapshot MUST fail

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
