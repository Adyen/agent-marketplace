## ADDED Requirements

### Requirement: Repair refuses duplicate desired identities and names what it writes

When two selected resources produce the same Codex MCP server key, repair SHALL report the error `materialize.codex.duplicate-identity` naming the key and both resources, and SHALL offer neither adoption nor replacement for that key. The key SHALL remain desired, so repair SHALL NOT offer it for orphan removal. Other entries in the file SHALL remain repairable. Every offered adoption or replacement SHALL name the resource whose generated value it records or writes, and the interactive replacement confirmation SHALL name that resource and the generated value digest.

#### Scenario: Duplicate desired Codex MCP identity is refused by repair

- **WHEN** two selected resources produce the same Codex MCP server key and `config.toml` holds an unmanaged entry with that key
- **THEN** the repair preview SHALL report `materialize.codex.duplicate-identity` naming the key and both resources, offer no adoption, replacement or removal for that key, and leave the file unchanged
- **AND** an unrelated conflicting entry in the same file SHALL still be offered for replacement under its own resource

#### Scenario: Replacement confirmation names the written resource

- **WHEN** `doctor --repair` asks for confirmation to replace a conflicting entry
- **THEN** the prompt SHALL name the entry, the file, the resource whose generated value would be written, and the generated value digest
