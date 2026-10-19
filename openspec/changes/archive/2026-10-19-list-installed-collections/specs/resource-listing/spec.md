## ADDED Requirements

### Requirement: List includes Marketplace selections from the verified cache

`agent-marketplace list` SHALL include Marketplace selections from every configured layer. It SHALL resolve them from the verified local cache, show them as `enabled` (or `required (org)` when required), and its sync-state line SHALL agree with `sync --check`. It SHALL NOT download artifacts and SHALL NOT write configuration or lock files.

#### Scenario: synced-marketplace-selection-is-listed-enabled

- **WHEN** a Marketplace selection has been installed and synced
- **THEN** `list` MUST show the selected resource with status `enabled`
- **AND** `list` MUST NOT print an out-of-sync line for it

#### Scenario: list-downloads-nothing-and-writes-nothing

- **WHEN** `list` runs against a configuration with Marketplace selections
- **THEN** it MUST NOT request Marketplace artifacts from the server
- **AND** the configuration and lock bytes MUST be unchanged afterwards

### Requirement: List shows installed collections as enabled

A collection selected by a configured layer, from a Git or local registry or as a direct Marketplace pin, SHALL be listed once with status `enabled` (or `required (org)` when required), and SHALL NOT be listed as `available`. A collection that no layer selects SHALL remain `available`.

#### Scenario: selected-git-collection-listed-enabled

- **WHEN** a Git registry's collection is selected by a configured layer
- **THEN** `list` MUST show that collection once with status `enabled`

#### Scenario: selected-marketplace-collection-listed-enabled

- **WHEN** a direct Marketplace collection pin is installed and synced
- **THEN** `list` MUST show that collection once with status `enabled`

#### Scenario: unselected-collection-listed-available

- **WHEN** a collection in a configured registry is not selected by any layer
- **THEN** `list` MUST show it with status `available`
