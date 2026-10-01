## ADDED Requirements

### Requirement: Status inspects Marketplace selections from the verified cache

The `status` command SHALL include Marketplace selections from every configuration layer in its synced-resource and drift inspection, resolving them from the verified local cache so its verdict agrees with `sync --check` for the same tree. Inspecting Marketplace selections SHALL NOT download resource artifacts or write configuration or lock files. A selected Marketplace pin without usable local artifact evidence MUST stay attributable to its resource coordinate together with the `agent-marketplace sync` remedy, and its materialized outputs MUST NOT be reported as orphaned or pending removal.

#### Scenario: Synced Marketplace selection appears in synced resources

- **WHEN** a Marketplace pin is installed and synchronized and `status` runs
- **THEN** the pinned resource MUST appear in `SYNCED RESOURCES`
- **AND** `status` MUST NOT report it as orphaned or pending removal, and its verdict MUST agree with `sync --check`

#### Scenario: Organization collection members are not orphaned without an organization lock

- **WHEN** an organization configuration selects a Marketplace collection, no organization lock is distributed, and the collection members are synchronized
- **THEN** `status` MUST NOT report the members as orphaned or pending removal
- **AND** it MUST NOT create an organization lock

#### Scenario: Unsynced Marketplace pin reports an attributable error without orphan rows

- **WHEN** a selected Marketplace pin has cached revision metadata but no verified local artifact and `status` runs
- **THEN** `status` MUST exit nonzero with an error that names the pin's resource coordinate and the `agent-marketplace sync` remedy
- **AND** it MUST NOT report the pin's materialized outputs as orphaned

#### Scenario: Status downloads no artifacts and writes no files

- **WHEN** `status` runs for a configuration with Marketplace selections
- **THEN** it MUST NOT download resource artifacts from the Marketplace endpoint
- **AND** it MUST leave configuration and lock files byte-identical
