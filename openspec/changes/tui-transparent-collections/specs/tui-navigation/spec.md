## ADDED Requirements

### Requirement: Collection members are visible in detail and expandable inline

The TUI SHALL list a collection's members and included sub-collections in the detail pane for Git/local and Marketplace collections. Discovery-only Marketplace collections SHALL load and verify their exact collection manifest read-only and fail closed on fetch or digest errors. Collections with known members SHALL render as expandable parent rows (`▼`/`▶`) across Status, Source, and Kind groupings.

#### Scenario: Expanding a collection shows its MCP servers and skills

- **WHEN** a user views a collection containing an MCP server and a skill in Status or Kind grouping and expands it (or views its detail pane)
- **THEN** the detail pane MUST list the contained MCP server and skill with their kinds
- **AND** expanding the collection row in the list MUST reveal indented member rows under the collection without fabricating unverified members

#### Scenario: Discovery-only Marketplace collection inspection stays read-only and fails closed

- **WHEN** a user inspects a discovery-only Marketplace collection whose manifest fetch succeeds or fails digest verification
- **THEN** a verified manifest MUST display its members read-only without writing config, lock, or harness files
- **AND** a failed or digest-mismatched manifest MUST report an actionable inspection error without showing unverified members
