## ADDED Requirements

### Requirement: Command palette typing remains distinct from navigation

The command palette SHALL append ordinary typed characters to its focused query, including `j` and `k`. Only the up and down arrow keys SHALL move the palette's match cursor.

#### Scenario: j and k remain query text

- **WHEN** the command palette has multiple matching actions, its match cursor is not at the first action, and the user types `j` or `k`
- **THEN** each character SHALL be appended to the palette query without dispatching an action, while up and down arrows continue to move the match cursor
