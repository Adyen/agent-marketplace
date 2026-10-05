## Purpose

An interactive client is the TUI or the desktop GUI acting on the user's behalf. This capability defines the surface-neutral startup snapshot, provisional rows, generation isolation, and reload behavior shared by those clients.

## ADDED Requirements

### Requirement: Progressive staged snapshot delivery

An interactive client SHALL deliver the startup snapshot progressively under one request generation: a provisional stage of configured selections, a config stage publishing the validated merged configuration, an inventory stage of verified rows with drift pending, and a final assessed stage.

#### Scenario: Startup snapshot delivers config, inventory, then assessed stages

- **WHEN** a startup snapshot resolves a valid merged configuration
- **THEN** it MUST deliver, in order under one shared generation, a provisional config stage whose rows are all marked verification-pending, a config stage publishing the validated merged configuration without rows, an inventory stage publishing verified rows with drift still pending, and a final stage publishing assessed drift and diagnostics

#### Scenario: Invalid configuration delivers one fail-closed final stage

- **WHEN** the merged configuration cannot be parsed or validated during startup
- **THEN** the snapshot MUST deliver exactly one final stage carrying diagnostics
- **AND** no provisional, config, or inventory stage MUST be delivered

#### Scenario: Config stage starts cached-first Marketplace discovery before local verification finishes

- **WHEN** the config stage publishes the validated merged configuration and that configuration declares Marketplace endpoints
- **THEN** cached-first Marketplace discovery MUST start immediately without waiting for local catalog verification
- **AND** cached discovery rows delivered during local verification MUST render without advancing the snapshot generation

### Requirement: Verification-pending provisional rows

Once the merged configuration validates, an interactive client SHALL render the actual configured selection set immediately as verification-pending rows. Pending rows SHALL occupy the positions and carry the scope and harness badges that verified inventory will give them, SHALL show a check state reflecting only the validated configuration — never a claim of installation or in-sync state — and SHALL NOT be eligible for toggles, installs, or sync until verified inventory replaces them. Pending rows SHALL be produced from validated configuration and read-only local evidence only: local-directory `all: true` and tag selections expand through a local catalog ingest, and no network fetch or acquisition-lock access is permitted to produce them.

#### Scenario: Provisional rows show configured selection state

- **WHEN** the validated configuration selects a Marketplace resource pin
- **THEN** the provisional stage MUST render a row for it marked verification-pending
- **AND** the row MUST show its configured check state and scope badge alongside an explicit verification-pending status that distinguishes configuration from installation
- **AND** install, remove, and sync actions for the row MUST remain unavailable until verified inventory replaces it

#### Scenario: Excluded configured selections render unchecked in their verified position

- **WHEN** the user-personal configuration contains an optional Marketplace pin that a higher-precedence exclusion disables
- **THEN** the provisional stage MUST render that pin unchecked with an explicit excluded status in the position verified inventory will give it
- **AND** non-excluded pins MUST render checked
- **AND** actions for the excluded pending row MUST remain unavailable until verification

#### Scenario: User-scope local all-true selections expand into pending rows

- **WHEN** the user-personal configuration selects every resource of a local-directory registry with `all: true`
- **THEN** the provisional stage MUST expand that selection through a read-only local catalog ingest and render each discovered resource as a pending row with its user scope badge
- **AND** the merged config stage that follows MUST preserve the pending rows

#### Scenario: Provisional rows render while transient acquisition is blocked

- **WHEN** an organization Marketplace has no deployed lock and its acquisition is still blocked
- **THEN** provisional rows MUST come from the validated pre-acquisition configuration, never from the pin-stripped browse-safe configuration
- **AND** configured Marketplace pins and `all: true` selections MUST render as checked pending rows with their scope badges while acquisition remains blocked

#### Scenario: Excluded-only configuration publishes no provisional rows

- **WHEN** every user-personal selection is excluded by Consumer organization configuration
- **THEN** the provisional stage MUST publish no rows
- **AND** no excluded selection MUST appear as a verification-pending row

#### Scenario: Provisional layout matches verified inventory

- **WHEN** the provisional stage renders pending rows for a configuration containing Marketplace pins, user-scope exclusions, and collection selections
- **THEN** the section and row layout, ordering, and badges MUST equal the layout that verified inventory produces for the same configuration, so verification does not reshuffle the list

#### Scenario: Optional collection member yields to a configured selection of the same resource

- **WHEN** an optional collection member and another configured selection name the same resource
- **THEN** the provisional stage MUST render only the configured selection, keeping the position verified inventory will retain

#### Scenario: Registry selections carry the harness-override badge

- **WHEN** a configured registry entry narrows its target harnesses
- **THEN** its pending rows MUST carry the same harness-override badge that verified inventory renders for them

#### Scenario: Organization Marketplace collection expands from the verified cached manifest

- **WHEN** a configured Marketplace collection's members are not yet pinned — such as an organization collection awaiting transient acquisition — and a verified collection manifest is cached locally
- **THEN** the provisional stage MUST expand the collection from that cached manifest and render its members as pending rows holding the positions and check states verified inventory will give them, with user and organization exclusions applied
- **AND** the cache read MUST NOT fetch, MUST NOT mutate the validated configuration, and a missing or unverifiable cache entry MUST leave the members to arrive with verified inventory

#### Scenario: Authoritative failure clears provisional rows

- **WHEN** provisional pending rows are displayed and the snapshot ends in an authoritative failure such as a failed transient acquisition
- **THEN** every verification-pending row MUST be cleared
- **AND** the failure diagnostics MUST be preserved

### Requirement: Browse-safe configuration during transient acquisition

When an organization Marketplace has no deployed lock and must be transiently acquired, resolution SHALL publish a browse-safe configuration carrying only the well-known-verified endpoint identity before acquisition completes, so cached browsing and search can render while acquisition is still blocked. The browse-safe configuration SHALL carry no provisional instance IDs and no unverified resource pins, and exact acquisition and validation SHALL remain mandatory before the authoritative inventory, toggles, and pins.

#### Scenario: Browse-safe config publishes the verified endpoint before acquisition

- **WHEN** an organization Marketplace requires transient acquisition and that acquisition is still blocked
- **THEN** resolution MUST publish a browse-safe configuration whose Marketplace entries carry the well-known-verified endpoint identity and no resource pins
- **AND** resolution MUST NOT return before acquisition completes
- **AND** the authoritative configuration published after acquisition MUST carry the verified pins and verified endpoint identity

#### Scenario: Provisional config publishes configured pins before acquisition

- **WHEN** an organization Marketplace requires transient acquisition and that acquisition is still blocked
- **THEN** resolution MUST publish a provisional configuration carrying the configured Marketplace pins unchanged
- **AND** resolution MUST NOT return before acquisition completes

#### Scenario: Browse-safe config is never published for unsafe configurations

- **WHEN** no transient acquisition is needed, the endpoint is denied by `allowed_marketplaces`, the deployed lock is malformed, or the endpoint cannot be bound
- **THEN** the browse-safe callback MUST NOT publish a configuration carrying Marketplace entries, and denied or malformed inputs MUST produce errors
- **AND** any published entry MUST carry no unverified resource pins

#### Scenario: Cached Marketplace rows render while acquisition is blocked

- **WHEN** a browse-safe configuration is published while organization Marketplace acquisition is blocked and fresh cached discovery results exist
- **THEN** the cached rows MUST render without any remote search request
- **AND** no authoritative inventory MUST be published before acquisition completes

### Requirement: Verified inventory and assessed handoff

Once exact-lock and catalog resolution complete, an interactive client SHALL publish a verified inventory stage whose rows are navigable and searchable while drift remains explicitly pending — never presented as `in sync` or assessed. A final assessed stage SHALL then replace the pending inventory with authoritative rows, drift, and diagnostics.

#### Scenario: Verified inventory is navigable while drift is pending

- **WHEN** the inventory stage publishes verified rows
- **THEN** the interactive client MUST leave the loading state, render the verified rows, and keep drift explicitly pending rather than `in sync` or assessed
- **AND** otherwise-eligible config-only actions on verified rows MUST be permitted

#### Scenario: Drift applies only to rows of its own catalog selection

- **WHEN** assessed drift items are applied to rows sharing a resource name across different catalog selections
- **THEN** only the selected row of the matching catalog MUST receive the assessed drift item
- **AND** rows from other catalogs and existing recovery drift MUST be preserved untouched

#### Scenario: Final stage replaces pending inventory with the assessment

- **WHEN** the final stage arrives after an inventory stage
- **THEN** it MUST replace the pending inventory with authoritative rows and assessed drift
- **AND** the startup progress indicator MUST be cleared

#### Scenario: Final stage merges discovery published during the config stage

- **WHEN** cached Marketplace discovery rows were rendered before the final stage arrives
- **THEN** the final stage MUST merge its authoritative local rows with the already-rendered discovery rows rather than dropping them

### Requirement: Selection and detail anchoring across stages

Accepted stage rebuilds SHALL preserve the user's selection and an open detail's Resource identity as rows are inserted or reordered between stages, retaining the selected row's position in the list viewport.

#### Scenario: Selection follows its resource when rows are inserted

- **WHEN** a pending row is selected and verified inventory inserts rows above it
- **THEN** the selection MUST follow the same resource
- **AND** its position in the list viewport MUST remain stable

#### Scenario: Untouched selection remains at the top as rows arrive

- **WHEN** the user has not changed the selection and discovery rows, verified inventory rows, and the final stage arrive
- **THEN** the selection and list offset MUST remain at the top through every stage

#### Scenario: An open detail follows its verified row

- **WHEN** a detail view is open on a verification-pending row and verified inventory replaces it
- **THEN** the open detail MUST retain the same Resource identity under the verified row's key rather than being cleared

### Requirement: Generation isolation and mutation gating

Startup stages SHALL share one snapshot request generation, so a superseded or failed stream SHALL NOT overwrite newer rows, selection, pane, filters, operation outcome, or drift. An intermediate stage SHALL NOT complete a pending mutation reload, and mutations SHALL remain gated until verified inventory arrives.

#### Scenario: Superseded stage delivery cannot overwrite newer state

- **WHEN** a config or inventory stage arrives carrying a superseded snapshot generation
- **THEN** it MUST NOT publish configuration or rows, MUST NOT start discovery, and MUST NOT consume the active generation

#### Scenario: A stale provisional stage cannot publish rows

- **WHEN** a provisional config stage arrives carrying a superseded snapshot generation
- **THEN** it MUST NOT publish any verification-pending rows

#### Scenario: An intermediate stage cannot complete a pending mutation reload

- **WHEN** a mutation-triggered reload is pending and an inventory stage of the same generation arrives
- **THEN** the pending mutation operation MUST remain active
- **AND** only the final assessed stage MAY complete the pending reload

#### Scenario: Mutation actions remain gated until verified inventory

- **WHEN** only the config stage has been published
- **THEN** install, remove, sync, and update actions MUST remain unavailable
- **AND WHEN** the verified inventory stage arrives
- **THEN** otherwise-eligible actions MUST become available

### Requirement: Cached collection revision expansion

Provisional expansion of an unpinned Marketplace collection SHALL read only locally verified cached collection manifests, preferring the exact configured revision when cached and otherwise the most recently cached verified revision, and SHALL reject cache entries that fail verification.

#### Scenario: Cached collection leaves prefer the exact then newest verified revision

- **WHEN** cached collection manifests exist for a collection coordinate and leaves are requested for a revision that is not cached
- **THEN** the most recently cached verified manifest's members MUST be returned, and unverified cache entries MUST be skipped
- **AND WHEN** leaves are requested for an exact cached verified revision
- **THEN** that manifest's members MUST be returned
- **AND** an uncached collection MUST report no leaves

#### Scenario: Tampered cached manifests are rejected

- **WHEN** a cached collection manifest's bytes no longer match its recorded digest
- **THEN** the cache MUST report no leaves for that collection

### Requirement: Single-view provisional row continuity

The single status-grouped view SHALL preserve provisional-to-verified row layout, scope and harness indicators, registry-qualified names, and applicable harness override badges. Unverified or required rows SHALL retain their mutation guards. Supported config-writing toggles SHALL continue to use the selected registry and user-personal scope and authoritative reload, and synchronization SHALL retain its existing confirmation and safety behavior.

#### Scenario: Provisional rows retain single-view position and guarded actions

- **WHEN** provisional user-personal rows become verified inventory in the single view, an unverified or required row is offered a toggle, and an eligible user-personal row is toggled and synchronized
- **THEN** provisional and verified section ordering and labels agree, harness override badges remain visible, guarded rows refuse mutation, and the eligible action writes only its chosen registry and user-personal scope before authoritative reload and existing synchronization handling

### Requirement: Ordinary reloads retain the current view until final assessment

Outside initial startup, an ordinary reload SHALL retain the last accepted resource list while resolution is in progress. It SHALL replace that view only with the final authoritative snapshot and SHALL NOT display provisional configured rows or intermediate configuration and inventory states. Ordinary reloads SHALL NOT perform startup-only provisional catalog ingestion, provisional harness-default reads, or browse-only endpoint identity discovery; authoritative resolution and assessment SHALL remain unchanged.

#### Scenario: Ordinary reload keeps the current view until final assessment

- **WHEN** an ordinary reload starts while an accepted resource list is displayed and then resolves a different resource list
- **THEN** the accepted list SHALL remain displayed until the final assessed snapshot is accepted, after which the authoritative rows and drift SHALL replace it together
