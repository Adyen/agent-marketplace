## MODIFIED Requirements

### Requirement: List includes Marketplace selections from the verified cache

`agent-marketplace list` SHALL include Marketplace selections from every configured layer. It SHALL resolve them from the verified local cache, show them as `enabled` (or `required (org)` when required), and its sync-state line SHALL agree with `sync --check`. It SHALL NOT download artifacts and SHALL NOT write configuration or lock files. When any selected Marketplace pin cannot be resolved from verified local cache evidence, `list` SHALL report the sync state as unknown together with the `agent-marketplace sync` remedy, and SHALL NOT report an out-of-sync count. When cache evidence for every selected pin is complete, the sync-state line SHALL report drift as before.

#### Scenario: synced-marketplace-selection-is-listed-enabled

- **WHEN** a Marketplace selection has been installed and synced
- **THEN** `list` MUST show the selected resource with status `enabled`
- **AND** `list` MUST NOT print an out-of-sync line for it

#### Scenario: list-downloads-nothing-and-writes-nothing

- **WHEN** `list` runs against a configuration with Marketplace selections
- **THEN** it MUST NOT request Marketplace artifacts from the server
- **AND** the configuration and lock bytes MUST be unchanged afterwards

#### Scenario: missing-marketplace-cache-lists-unknown-sync-state

- **WHEN** a Marketplace collection has been installed and synced, the local Marketplace cache is then deleted, and `list` runs
- **THEN** `list` MUST exit 0, MUST show the selected collection with status `enabled`, and MUST NOT print an out-of-sync count
- **AND** it MUST print that the sync state is unknown, naming the `agent-marketplace sync` remedy
- **AND** it MUST NOT request Marketplace artifacts, and the configuration and lock bytes MUST be unchanged afterwards

#### Scenario: never-acquired-marketplace-pin-lists-unknown-sync-state

- **WHEN** a Marketplace pin is selected but was never acquired into the local cache and `list` runs
- **THEN** `list` MUST print that the sync state is unknown, naming the `agent-marketplace sync` remedy
- **AND** it MUST NOT report the tree as in sync

#### Scenario: list-counts-drift-when-marketplace-cache-is-complete

- **WHEN** a Marketplace selection has been installed and synced, its verified cache evidence is present, one of its materialized outputs is deleted, and `list` runs
- **THEN** `list` MUST print an out-of-sync count with the `agent-marketplace sync` remedy
- **AND** it MUST NOT print that the sync state is unknown
