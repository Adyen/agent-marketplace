# tui-feedback Specification

## Purpose
Defines truthful operation feedback and available actions without allowing progress displays or shortcut discovery to authorize mutation.

## Requirements

### Requirement: Observed progress and status
The TUI SHALL show drift, Marketplace health, active operation, and error and warning counts while presenting only pipeline phases actually entered. Direct and injected lifecycle operations SHALL deliver phase observations before completion, including preparation and consent when applicable, materialization, and authoritative reload. An operation without observations SHALL show a generic running state. Accepted progress SHALL NOT finish an operation, clear diagnostics or announce success; authoritative completion and reload remain decisive.

#### Scenario: Phase observed before completion
- **WHEN** an update or install is blocked after entering a real pipeline phase
- **THEN** the active phase is visible while the operation remains busy, including through injected lifecycle adapters

#### Scenario: No observer
- **WHEN** a lifecycle path emits no phase observations
- **THEN** the status bar displays generic running instead of invented phases

### Requirement: Stale progress isolation
Progress observations SHALL carry operation identity, a monotonic within-operation sequence, and relevant request generation; duplicate, out-of-order, finished and superseded events SHALL be ignored.

#### Scenario: Late progress
- **WHEN** a phase event arrives for a completed or superseded operation, or with a sequence no newer than the last accepted event
- **THEN** phase display, active operation, diagnostics and success state remain unchanged

### Requirement: Bounded notifications
The TUI SHALL display at most three notifications and remove them after their timer expires without obstructing required controls.

#### Scenario: Notification overflow
- **WHEN** four notifications arrive before expiration
- **THEN** no more than three are visible and expired notifications disappear

### Requirement: Context-aware command palette
The palette SHALL open only from an eligible resource list with supported size and no active search input, modal, overlay or fold prefix. It SHALL filter explicit action labels fuzzily with deterministic identity tie breaks, show each action's shortcut separately from its label, expose only list-context actions, and re-evaluate action eligibility when submitted. It SHALL use the keyboard path's guarded dispatcher for search, clear search while a filter is applied, details, toggle, reload, sync, update, harness and config editing, help, diagnostics, tab and fold actions, and interactive quit. q SHALL be query text while open, Ctrl+C SHALL remain hard exit, and Esc SHALL close the palette.

#### Scenario: State changes after opening
- **WHEN** selected row, config validity, pending verification or active operation changes while the palette is open
- **THEN** submission applies current eligibility and refuses unsafe mutations without bypassing confirmation or recovery paths

#### Scenario: Prefix action and excluded controls
- **WHEN** a user invokes a fold-prefix action from the palette
- **THEN** the action dispatches by stable identity without synthesizing keys, and modal-only scope/confirmation actions are absent

#### Scenario: Clear search from the palette
- **WHEN** a filter is applied and a user runs the clear-search action
- **THEN** the filter clears as it does with Esc, and the action is absent when no filter is applied

### Requirement: Discoverable footer actions
The resource list footer SHALL show the command palette, help and quit hints at every supported width, alongside status segments and active operations; the palette hint MAY use a shorter label to fit. While errors are present, the error count and the errors, reload and quit hints SHALL take precedence, followed by the palette and help hints as space allows. Other segments SHALL be dropped whole, lowest priority first, when space runs out. While search input is focused, the footer SHALL show how to apply and cancel the query and how to quit at every supported width.

#### Scenario: Narrow footer with status
- **WHEN** the list renders at 50, 80 or 120 columns with warnings, Marketplace health segments or an active operation
- **THEN** the palette, help and quit hints remain visible and no hint is cut mid-word
