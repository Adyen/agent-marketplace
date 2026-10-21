# tui-startup Specification

## Purpose
Describes the observable progressive startup behavior of the interactive TUI: how the startup snapshot is delivered in stages under one request generation, how configured selections render as verification-pending rows before verification completes, how Marketplace browsing proceeds cache-first while transient acquisition is blocked, and how stale or superseded stages are isolated from newer state. Selection precedence, acquisition locks, and configuration layering are governed by the `config-layering`, `acquisition-lock-codec`, `marketplace-api`, and `owned-entry-reconciliation` capabilities; this capability specifies only the TUI's staging and presentation of their outcomes.

## Requirements

### Requirement: Progressive staged snapshot delivery

The TUI SHALL validate required consumer roots and include trees under `consumer-config-format` before delivering usable startup configuration or rows. For valid input, it SHALL deliver the startup snapshot progressively under one request generation: a provisional stage of configured selections, a config stage publishing the validated merged configuration, an inventory stage of verified rows with drift pending, and a final assessed stage. Unsupported input MUST produce the path-specific format refusal without legacy operation or in-process conversion.

#### Scenario: Startup snapshot delivers config, inventory, then assessed stages

- **WHEN** a startup snapshot resolves a valid v4 merged configuration
- **THEN** it MUST deliver, in order under one shared generation, a provisional config stage whose rows are all marked verification-pending, a config stage publishing the validated merged configuration without rows, an inventory stage publishing verified rows with drift still pending, and a final stage publishing assessed drift and diagnostics

#### Scenario: Invalid configuration delivers one fail-closed final stage

- **WHEN** a required root or include tree cannot be parsed or validated during startup, including an unsupported root or explicit fragment version
- **THEN** the snapshot MUST deliver exactly one final stage carrying diagnostics
- **AND** no provisional, config, or inventory stage MUST be delivered
- **AND** unsupported consumer input MUST retain the path-specific diagnostic from `consumer-config-format`

#### Scenario: Config stage starts cached-first Marketplace discovery before local verification finishes

- **WHEN** the config stage publishes the validated merged configuration and that configuration declares Marketplace endpoints
- **THEN** cached-first Marketplace discovery MUST start immediately without waiting for local catalog verification
- **AND** cached discovery rows delivered during local verification MUST render without advancing the snapshot generation

### Requirement: Verification-pending provisional rows

Once the merged configuration validates, the TUI SHALL render the actual
configured selection set immediately as verification-pending rows. Pending
rows SHALL occupy the positions and carry the scope and harness badges that
verified inventory will give them, SHALL show a check state reflecting only
the validated configuration — never a claim of installation or in-sync state
— and SHALL NOT be eligible for toggles, installs, or sync until verified
inventory replaces them. Pending rows SHALL be produced from validated
configuration and read-only local evidence only: local-directory `all: true`
and tag selections expand through a local catalog ingest, and no network
fetch or acquisition-lock access is permitted to produce them.

#### Scenario: Provisional config stage publishes verification-pending rows

- **WHEN** the validated configuration selects a Marketplace resource pin
- **THEN** the provisional stage MUST render a row for it marked
  verification-pending
- **AND** the row MUST show its configured check state and scope badge
  alongside an explicit verification-pending status that distinguishes
  configuration from installation
- **AND** the row MUST refuse to open the toggle modal until verified
  inventory replaces it without duplicates or fabricated badges

#### Scenario: Excluded configured selections render unchecked in their verified position

- **WHEN** the validated configuration contains an optional Marketplace pin
  that a user-level exclusion disables
- **THEN** the provisional stage MUST render that pin unchecked with an
  explicit excluded status in the position verified inventory will give it
- **AND** non-excluded pins MUST render checked
- **AND** the excluded pending row MUST refuse the toggle modal until
  verification

#### Scenario: Local all-true selections expand into pending rows on the normal config path

- **WHEN** validated configuration layers select every resource of
  local-directory registries with `all: true`
- **THEN** the provisional stage MUST expand those selections through a
  read-only local catalog ingest and render each discovered resource as a
  pending row with its scope badge
- **AND** configured committed and personal-local excludes MUST already be
  applied so excluded resources render no pending row
- **AND** the merged config stage that follows MUST preserve the pending rows

#### Scenario: Provisional rows render while transient acquisition is blocked

- **WHEN** an organization Marketplace has no deployed lock and its
  acquisition is still blocked
- **THEN** provisional rows MUST come from the validated pre-acquisition
  configuration, never from the pin-stripped browse-safe configuration
- **AND** configured Marketplace pins and `all: true` selections MUST render
  as checked pending rows with their scope badges while acquisition remains
  blocked

#### Scenario: Registry-scoped local excludes apply to provisional rows

- **WHEN** a personal-local configuration excludes a resource name only for
  specific registry entries
- **THEN** the provisional stage MUST suppress pending rows for the excluded
  registry occurrences only
- **AND** pending rows for the same resource name from registries not covered
  by the exclusion MUST still render

#### Scenario: Excluded-only configuration publishes no provisional rows

- **WHEN** every configured selection is excluded by a higher-precedence
  layer
- **THEN** the provisional stage MUST publish no rows
- **AND** no excluded selection MUST appear as a verification-pending row

#### Scenario: Provisional layout matches verified inventory

- **WHEN** the provisional stage renders pending rows for a configuration
  containing Marketplace pins, exclusions, and collection selections
- **THEN** the section and row layout, ordering, and badges MUST equal the
  layout that verified inventory produces for the same configuration, so
  verification does not reshuffle the list

#### Scenario: Optional collection member yields to a configured selection of the same resource

- **WHEN** an optional collection member and another configured selection
  name the same resource
- **THEN** the provisional stage MUST render only the configured selection,
  keeping the position verified inventory will retain

#### Scenario: Registry selections carry the harness-override badge

- **WHEN** a configured registry entry narrows its target harnesses
- **THEN** its pending rows MUST carry the same harness-override badge that
  verified inventory renders for them

#### Scenario: Organization Marketplace collection expands from the verified cached manifest

- **WHEN** a configured Marketplace collection's members are not yet pinned —
  such as an organization collection awaiting transient acquisition — and a
  verified collection manifest is cached locally
- **THEN** the provisional stage MUST expand the collection from that cached
  manifest and render its members as pending rows holding the positions and
  check states verified inventory will give them, with exclusions applied
- **AND** the cache read MUST NOT fetch, MUST NOT mutate the validated
  configuration, and a missing or unverifiable cache entry MUST leave the
  members to arrive with verified inventory

#### Scenario: Authoritative failure clears provisional rows

- **WHEN** provisional pending rows are displayed and the snapshot ends in an
  authoritative failure such as a failed transient acquisition
- **THEN** every verification-pending row MUST be cleared
- **AND** the failure diagnostics MUST be preserved

### Requirement: Browse-safe configuration during transient acquisition

When an organization Marketplace has no deployed lock and must be
transiently acquired, resolution SHALL publish a browse-safe configuration
carrying only the well-known-verified endpoint identity before acquisition
completes, so cached browsing and search can render while acquisition is
still blocked. The browse-safe configuration SHALL carry no provisional
instance IDs and no unverified resource pins, and exact acquisition and
validation SHALL remain mandatory before the authoritative inventory,
toggles, and pins.

#### Scenario: Browse-safe config publishes the verified endpoint before acquisition

- **WHEN** an organization Marketplace requires transient acquisition and
  that acquisition is still blocked
- **THEN** resolution MUST publish a browse-safe configuration whose
  Marketplace entries carry the well-known-verified endpoint identity and no
  resource pins
- **AND** resolution MUST NOT return before acquisition completes
- **AND** the authoritative configuration published after acquisition MUST
  carry the verified pins and verified endpoint identity

#### Scenario: Provisional config publishes configured pins before acquisition

- **WHEN** an organization Marketplace requires transient acquisition and
  that acquisition is still blocked
- **THEN** resolution MUST publish a provisional configuration carrying the
  configured Marketplace pins unchanged
- **AND** resolution MUST NOT return before acquisition completes

#### Scenario: Browse-safe config is never published for unsafe configurations

- **WHEN** no transient acquisition is needed, the endpoint is denied by
  `allowed_marketplaces`, the deployed lock is malformed, or the endpoint
  cannot be bound
- **THEN** the browse-safe callback MUST NOT publish a configuration carrying
  Marketplace entries, and denied or malformed inputs MUST produce errors
- **AND** any published entry MUST carry no unverified resource pins

#### Scenario: Cached Marketplace rows render while acquisition is blocked

- **WHEN** a browse-safe configuration is published while organization
  Marketplace acquisition is blocked and fresh cached discovery results
  exist
- **THEN** the cached rows MUST render without any remote search request
- **AND** no authoritative inventory MUST be published before acquisition
  completes

### Requirement: Verified inventory and assessed handoff

Once exact-lock and catalog resolution complete, the TUI SHALL publish a
verified inventory stage whose rows are navigable and searchable while drift
remains explicitly pending — never presented as `in sync` or assessed. A
final assessed stage SHALL then replace the pending inventory with
authoritative rows, drift, and diagnostics.

#### Scenario: Verified inventory is navigable and toggle-ready while drift is pending

- **WHEN** the inventory stage publishes verified rows
- **THEN** the model MUST leave the loading state, render the verified rows,
  and keep drift explicitly pending rather than `in sync` or assessed
- **AND** startup progress MUST remain visible in the footer
- **AND** otherwise-eligible config-only toggles on verified rows MUST be
  permitted

#### Scenario: Drift applies only to rows of its own catalog selection

- **WHEN** assessed drift items are applied to rows sharing a resource name
  across different catalog selections
- **THEN** only the selected row of the matching catalog MUST receive the
  assessed drift item
- **AND** rows from other catalogs and existing recovery drift MUST be
  preserved untouched

#### Scenario: Final stage replaces pending inventory with the assessment

- **WHEN** the final stage arrives after an inventory stage
- **THEN** it MUST replace the pending inventory with authoritative rows and
  assessed drift
- **AND** the startup progress indicator MUST be cleared

#### Scenario: Final stage merges discovery published during the config stage

- **WHEN** cached Marketplace discovery rows were rendered before the final
  stage arrives
- **THEN** the final stage MUST merge its authoritative local rows with the
  already-rendered discovery rows rather than dropping them

### Requirement: Selection and detail anchoring across stages

Accepted stage rebuilds SHALL preserve the user's selection and an open
detail's resource identity as rows are inserted or reordered between stages.

#### Scenario: Verified rows inserted above a selection keep it on the same screen line

- **WHEN** a pending row is selected in a list that overflows the viewport and
  verified inventory inserts rows above it
- **THEN** the selection MUST follow the same resource
- **AND** the selected row MUST keep its line within the list viewport rather
  than shifting on screen

#### Scenario: An untouched cursor stays on top when installed rows arrive

- **WHEN** the user has not moved the cursor and discovery rows, then
  verified inventory rows, then the final stage arrive
- **THEN** the cursor and list offset MUST remain at the top through every
  stage

#### Scenario: An open detail follows its verified row

- **WHEN** a detail pane is open on a verification-pending row and verified
  inventory replaces it
- **THEN** the open detail MUST retain the same resource identity under the
  verified row's key rather than being cleared

### Requirement: Generation isolation and mutation gating

Startup stages SHALL share one snapshot request generation, so a superseded
or failed stream SHALL NOT overwrite newer rows, selection, pane, filters,
operation outcome, or drift. An intermediate stage SHALL NOT complete a
pending mutation reload, and mutations SHALL remain gated until verified
inventory arrives.

#### Scenario: Superseded stage delivery cannot overwrite newer state

- **WHEN** a config or inventory stage arrives carrying a superseded snapshot
  generation
- **THEN** it MUST NOT publish configuration or rows, MUST NOT start
  discovery, and MUST NOT consume the active generation

#### Scenario: A stale provisional stage cannot publish rows

- **WHEN** a provisional config stage arrives carrying a superseded snapshot
  generation
- **THEN** it MUST NOT publish any verification-pending rows

#### Scenario: An intermediate stage cannot complete a pending mutation reload

- **WHEN** a mutation-triggered reload is pending and an inventory stage of
  the same generation arrives
- **THEN** the pending mutation operation MUST remain active
- **AND** only the final assessed stage MAY complete the pending reload

#### Scenario: Mutations stay gated until verified inventory

- **WHEN** only the config stage has been published
- **THEN** sync and update keys MUST NOT start a mutation
- **AND** pressing Space on a cached discovery row MUST NOT open the install
  scope modal
- **AND WHEN** the verified inventory stage arrives
- **THEN** sync MUST become eligible

### Requirement: Cached collection revision expansion

Provisional expansion of an unpinned Marketplace collection SHALL read only
locally verified cached collection manifests, preferring the exact
configured revision when cached and otherwise the most recently cached
verified revision, and SHALL reject cache entries that fail verification.

#### Scenario: Cached collection leaves prefer the exact then newest verified revision

- **WHEN** cached collection manifests exist for a collection coordinate and
  leaves are requested for a revision that is not cached
- **THEN** the most recently cached verified manifest's members MUST be
  returned, and unverified cache entries MUST be skipped
- **AND WHEN** leaves are requested for an exact cached verified revision
- **THEN** that manifest's members MUST be returned
- **AND** an uncached collection MUST report no leaves

#### Scenario: Tampered cached manifests are rejected

- **WHEN** a cached collection manifest's bytes no longer match its recorded
  digest
- **THEN** the cache MUST report no leaves for that collection

### Requirement: Single-view provisional row continuity

The single status-grouped view SHALL preserve provisional-to-verified row layout, scope letters, registry-qualified names, and applicable harness override badges. Unverified or required rows SHALL retain their mutation guards. Supported config-writing toggles SHALL continue to use the selected registry and scope and authoritative reload, and synchronization SHALL retain its existing confirmation and safety behavior.

#### Scenario: Provisional rows retain single-view position and guarded actions

- **WHEN** provisional configured rows become verified inventory in the single view, an unverified or required row is offered a toggle, and an eligible row is toggled and synchronized
- **THEN** provisional and verified section ordering and labels agree, harness override badges remain visible, guarded rows refuse mutation, and the eligible action writes only its chosen registry and scope before authoritative reload and existing synchronization handling

### Requirement: Ordinary reloads retain the current view until final assessment

Outside initial startup, an ordinary reload SHALL retain the last accepted resource list while resolution is in progress. It SHALL replace that view only with the final authoritative snapshot and SHALL NOT display provisional configured rows or intermediate configuration and inventory states. Ordinary reloads SHALL NOT perform startup-only provisional catalog ingestion, provisional harness-default reads, or browse-only endpoint identity discovery; authoritative resolution and assessment SHALL remain unchanged.

#### Scenario: Ordinary reload keeps the current view until final assessment

- **WHEN** an ordinary reload starts while an accepted resource list is displayed and then resolves a different resource list
- **THEN** the accepted list SHALL remain displayed until the final assessed snapshot is accepted, after which the authoritative rows and drift SHALL replace it together

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

### Requirement: Installed collections render as installed rows

The provisional and verified startup stages SHALL render a selected collection, whether a Git or local collection or a direct Marketplace collection pin, as an installed row with its scope badges. They SHALL NOT render it as available. Disabling an installed collection SHALL remove its collection selection through the existing config write path.

#### Scenario: verified-selected-collection-row-is-installed

- **WHEN** verified inventory contains a selected collection
- **THEN** its row MUST be enabled with the scope of the selecting layer
- **AND** the row MUST NOT appear again as an available row

#### Scenario: provisional-selected-collection-row-is-installed

- **WHEN** the validated configuration selects a collection
- **THEN** the provisional stage MUST render that collection as a verification-pending installed row
- **AND** a non-direct nested collection pin MUST NOT render a row

#### Scenario: disabling-installed-collection-removes-selection

- **WHEN** the user disables an installed collection row in its selecting scope
- **THEN** the collection selection MUST be removed from that scope's configuration
- **AND** other selections in that configuration MUST be preserved
