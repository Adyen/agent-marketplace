## MODIFIED Requirements

### Requirement: MCP removal preserves valid user configuration

The marketplace SHALL remove only its managed MCP server entries while retaining valid JSON configuration and any user-owned entries.

#### Scenario: Final managed MCP server is removed

- **WHEN** the only marketplace-managed MCP server is removed and sync completes
- **THEN** the MCP configuration file MUST remain present
- **AND** it MUST contain an empty `mcpServers` object
- **AND** it MUST NOT contain the removed server name
- **AND** it MUST NOT contain the marketplace ownership marker

#### Scenario: User-owned MCP servers survive removal

- **WHEN** a marketplace-managed MCP server is removed from a file that also contains user-owned MCP servers
- **THEN** sync MUST retain the user-owned servers
- **AND** it MUST remove only the managed server and its ownership marker
