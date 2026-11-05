## ADDED Requirements

### Requirement: Owned entry edits preserve interleaved unmanaged content

When the native representation of an owned entry is split by unmanaged content, such as a handwritten table or array table placed between the tables of an owned Codex MCP server, replacing, pruning, repairing or removing the owned entry SHALL change only the owned entry's own tables. The interleaved content SHALL be preserved byte-for-byte, and the resulting file SHALL hold the configured value of the owned entry, or no value when it was pruned or removed.

#### Scenario: Interleaved handwritten tables survive owned entry sync

- **WHEN** a handwritten table or array table sits between the tables of an owned Codex MCP server, and sync updates or retires that server
- **THEN** sync SHALL apply the update or retirement without an error diagnostic
- **AND** the handwritten table SHALL remain byte-for-byte and sibling owned servers SHALL be unchanged

#### Scenario: Interleaved handwritten tables survive owned entry removal and replacement

- **WHEN** repair or owned-entry cleanup removes or replaces an owned Codex MCP server whose tables are split by a handwritten table
- **THEN** only that server's tables SHALL change, and the handwritten table and other servers SHALL remain
