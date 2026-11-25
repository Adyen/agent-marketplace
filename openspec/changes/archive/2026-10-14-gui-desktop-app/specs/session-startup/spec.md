## MODIFIED Requirements

### Requirement: Verification-pending provisional rows

Once the merged configuration validates, an interactive client SHALL render the actual configured selection set immediately as verification-pending rows. Pending rows SHALL occupy the positions and carry the scope and harness badges that verified inventory will give them, SHALL show a check state reflecting only the validated configuration — never a claim of installation or in-sync state — and SHALL NOT be eligible for toggles, installs, or sync until verified inventory replaces them. Pending rows SHALL be produced from validated configuration and read-only local evidence only: local-directory `all: true` and tag selections expand through a local catalog ingest, and no network fetch or acquisition-lock access is permitted to produce them.

#### Scenario: Provisional rows show configured selection state

- **WHEN** the validated configuration selects a Marketplace resource pin
- **THEN** the provisional stage MUST render a row for it marked verification-pending
- **AND** the row MUST show its configured check state and scope badge alongside an explicit verification-pending status that distinguishes configuration from installation
- **AND** install, remove, and sync actions for the row MUST remain unavailable until verified inventory replaces it

#### Scenario: Excluded configured selections render unchecked in their verified position

- **WHEN** Consumer organization configuration contains optional Marketplace resource pins and a user-personal exclusion disables one of those pins
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

- **WHEN** a validated user-personal configuration selects resources from a non-required local-directory registry with `all: true` and subsequent same-layer exclusions disable every selected resource, with no other configured selections
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
