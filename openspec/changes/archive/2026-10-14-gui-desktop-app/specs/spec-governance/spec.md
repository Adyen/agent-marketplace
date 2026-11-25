## ADDED Requirements

### Requirement: Interactive surface parity

Every scenario in a `session-*` capability MUST be bound by at least one test of the TUI surface (test files under `internal/tui/` or `tools/tui-qa/`) and at least one test of the GUI surface (test files under `gui/` or `cmd/agent-marketplace-gui/`). The traceability check SHALL fail and name the missing surface.

#### Scenario: Session scenario has both surface witnesses

- **WHEN** a `session-*` scenario has an annotated TUI witness and an annotated GUI witness
- **THEN** the traceability check SHALL accept the scenario

#### Scenario: Missing surface witness is reported

- **WHEN** a `session-*` scenario lacks a test annotation from either the TUI or GUI surface
- **THEN** the traceability check SHALL fail
- **AND** its diagnostic SHALL name the missing surface
