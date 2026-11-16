## ADDED Requirements

### Requirement: Kind-aware Marketplace search

TUI search SHALL match resource kind identifiers and displayed kind labels case-insensitively for configured and uninstalled Marketplace resources, independently of their names and descriptions. A kind query SHALL reach every matching discoverable resource across result pages and configured allowed endpoints. The existing slash shortcut SHALL open search without becoming part of the query.

#### Scenario: Every kind query discovers all Marketplace matches

- **WHEN** a user presses `/` and types any supported resource kind with configured and uninstalled resources whose names and descriptions contain no kind text
- **THEN** all resources of that kind MUST be reachable in the filtered list, including Marketplace matches beyond the first page and across configured allowed endpoints
- **AND** `/mcp` MUST reach all MCP servers and `/command` MUST reach all commands through this same generic behavior
- **AND** mixed-case kind queries MUST match the same kinds

#### Scenario: Other kinds retain additive text matching

- **WHEN** a user searches for another supported kind identifier or displayed kind label, or searches for a resource by name or metadata
- **THEN** kind-only matches MUST be included alongside existing text matches with existing name-before-metadata ranking, deterministic ties, grouping, and highlighting
- **AND** a kind-only match MUST NOT highlight unrelated name characters

### Requirement: Kind discovery preserves search lifecycle

Kind-aware Marketplace discovery SHALL preserve immediate local filtering, debounced remote requests, endpoint policy, and cancellation of superseded results. Discovery SHALL NOT install resources or edit authored configuration, acquisition locks, or materialized files.

#### Scenario: Superseded kind search cannot replace current results

- **WHEN** a kind search is delayed and the user changes or clears the query before it completes
- **THEN** late results MUST NOT replace the current filter, selection, or browse inventory
- **AND** a no-match query and clearing search MUST retain safe navigation and the existing saved-fold behavior

#### Scenario: Failed kind discovery preserves usable local results

- **WHEN** one configured Marketplace endpoint fails or is denied during a kind search
- **THEN** local matches and successful allowed endpoints MUST remain usable with visible failure diagnostics
- **AND** denied endpoints MUST NOT be contacted and discovery MUST NOT change authored configuration, acquisition locks, or materialized files
