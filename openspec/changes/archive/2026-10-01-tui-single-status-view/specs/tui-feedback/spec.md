## MODIFIED Requirements

### Requirement: Context-aware command palette
The palette SHALL open only from an eligible resource list with supported size and no active search input, modal, overlay or fold prefix. It SHALL filter explicit action labels fuzzily with deterministic identity tie breaks, show each action's shortcut separately from its label, expose only list-context actions, and re-evaluate action eligibility when submitted. It SHALL use the keyboard path's guarded dispatcher for search, clear search while a filter is applied, details, toggle, reload, sync, update, harness and config editing, help, diagnostics and fold actions, and interactive quit. It SHALL NOT expose tab-switch actions. q SHALL be query text while open, Ctrl+C SHALL remain hard exit, and Esc SHALL close the palette.

#### Scenario: State changes after opening
- **WHEN** selected row, config validity, pending verification or active operation changes while the palette is open
- **THEN** submission applies current eligibility and refuses unsafe mutations without bypassing confirmation or recovery paths

#### Scenario: Prefix action and excluded controls
- **WHEN** a user invokes a fold-prefix action from the palette
- **THEN** the action dispatches by stable identity without synthesizing keys, and modal-only scope/confirmation actions are absent

#### Scenario: Clear search from the palette
- **WHEN** a filter is applied and a user runs the clear-search action
- **THEN** the filter clears as it does with Esc, and the action is absent when no filter is applied

## ADDED Requirements

### Requirement: Tab-free browse telemetry

Opening a resource detail SHALL retain the browse action's action, view, and resource-kind telemetry attributes, but SHALL NOT emit the obsolete tab attribute or a tab-switch action. Other TUI action telemetry and disclosure policy SHALL remain unchanged.

#### Scenario: Detail browse span has no tab attribute

- **WHEN** a user opens a resource detail with TUI action telemetry enabled
- **THEN** the emitted action span identifies browse, detail, and the resource kind without a tab attribute
