# tui-feedback Specification

## Purpose
Defines truthful operation feedback and available actions without allowing progress displays or shortcut discovery to authorize mutation.

## Requirements

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

### Requirement: Discoverable footer actions
The resource list footer SHALL show the command palette, help and quit hints at every supported width, alongside status segments and active operations; the palette hint MAY use a shorter label to fit. While errors are present, the error count and the errors, reload and quit hints SHALL take precedence, followed by the palette and help hints as space allows. The errors hint SHALL be offered while the errors view has an error, warning or discovery error to show, and only then, matching the palette's errors action; pending drift alone SHALL NOT offer it. Other segments SHALL be dropped whole, lowest priority first, when space runs out. While search input is focused, the footer SHALL show how to apply and cancel the query and how to quit at every supported width.

#### Scenario: Narrow footer with status
- **WHEN** the list renders at 50, 80 or 120 columns with warnings, Marketplace health segments or an active operation
- **THEN** the palette, help and quit hints remain visible and no hint is cut mid-word

#### Scenario: Errors hint only with diagnostics
- **WHEN** the list has pending drift but no error, warning or discovery error
- **THEN** the footer MUST NOT offer the errors hint, and the palette MUST NOT offer the errors action
- **AND** once a warning is present, both MUST offer it

### Requirement: Tab-free browse telemetry

Opening a resource detail SHALL retain the browse action's action, view, and resource-kind telemetry attributes, but SHALL NOT emit the obsolete tab attribute or a tab-switch action. Other TUI action telemetry and disclosure policy SHALL remain unchanged.

#### Scenario: Detail browse span has no tab attribute

- **WHEN** a user opens a resource detail with TUI action telemetry enabled
- **THEN** the emitted action span identifies browse, detail, and the resource kind without a tab attribute

### Requirement: Discoverable grouping and section toggle actions

Help SHALL advertise `v` for View settings and Space for resource or section toggles while retaining Enter details/fold and other folding controls. The palette SHALL offer View settings and Toggle all in section according to current list context and re-evaluate their guards on submission. Former tab switching SHALL remain unadvertised. Bulk feedback SHALL disclose matched, targeted, and skipped counts without claiming success before authoritative completion.

#### Scenario: Help and palette reflect section context

- **WHEN** the user inspects help and the palette on a resource or section, including an ineligible section or busy state
- **THEN** current grouping and section actions SHALL be discoverable in their eligible context, section submission SHALL use keyboard-path guards, former tab actions SHALL be absent, and counts SHALL reflect the confirmed target set

### Requirement: Privacy-safe bulk action telemetry

When TUI action telemetry is enabled, a submitted bulk toggle SHALL emit one action span with primary and secondary grouping axes, targeted and skipped counts, scope, and direction. It SHALL NOT contain resource names, source labels, filters, config paths, or registry coordinates, and SHALL retain the existing telemetry disclosure policy.

#### Scenario: Bulk action emits one bounded span

- **WHEN** a user submits a bulk toggle containing resources and labels with identifying text
- **THEN** one bulk action span SHALL record only grouping, counts, scope, and direction without that identifying text or obsolete tab attributes

### Requirement: Missing exact snapshot recovery feedback

During TUI startup and reload, an acquisition lock whose exact snapshot is absent from the local cache SHALL produce a warning retaining `lock.snapshot-unavailable` context and instructing the user to press `s` to sync or run `agent-marketplace sync --locked`. The affected selections and drift SHALL remain unverified, never presented as in sync or as a successful authoritative reload. Inspection SHALL NOT acquire the missing exact snapshot, substitute mutable-ref content, or write configuration, acquisition locks, ownership state, or materialized output. Explicit synchronization SHALL remain available through the normal guarded lifecycle. This presentation policy SHALL NOT downgrade untrusted or mismatched exact snapshots, failed synchronization, or CLI check and offline failures.

#### Scenario: Missing locked snapshot is a recoverable warning

- **GIVEN** an acquisition lock records an exact commit not cached by the consumer
- **WHEN** the TUI starts or reloads
- **THEN** it SHALL show a missing-snapshot warning with the synchronization instructions while retaining unverified selections and drift
- **AND** inspection SHALL leave configuration, acquisition locks, the affected source's exact cache, and materialized output unchanged without acquiring or substituting its content

#### Scenario: Sync recovery preserves the acquisition lock

- **GIVEN** an incoming acquisition lock records an uncached commit and its mutable ref has since advanced again
- **WHEN** the consumer explicitly syncs
- **THEN** synchronization SHALL acquire and materialize the locked commit, retain byte-identical acquisition locks, and permit subsequent exact inspection
- **AND** CLI check commands SHALL fail for the missing exact snapshot before synchronization

#### Scenario: Untrusted snapshots remain errors

- **WHEN** the TUI inspects an exact snapshot whose tracked contents no longer match its locked commit
- **THEN** the snapshot SHALL remain an error rather than a recoverable cache-miss warning and drift SHALL remain unverified

#### Scenario: Failed sync remains an error

- **GIVEN** the TUI displays a recoverable missing-snapshot warning
- **WHEN** explicit synchronization fails and inspection still cannot verify the exact snapshot
- **THEN** the synchronization failure SHALL remain an error alongside the cache-miss warning, without an in-sync or synchronization-success claim

### Requirement: Discoverable missing-snapshot recovery shortcut

When the TUI displays a recoverable missing-exact-snapshot warning without unrelated inspection or operation errors, the resource-list footer SHALL prioritize the `s` synchronization shortcut at every supported terminal width while retaining the command palette, help, and quit entry points. The shortcut SHALL use the ordinary synchronization guards and SHALL NOT imply that inspection has verified the missing exact snapshot.

#### Scenario: Sync recovery shortcut remains visible

- **WHEN** the TUI displays a missing-exact-snapshot warning without unrelated errors at 50, 80, or 120 columns
- **THEN** the resource-list footer SHALL show the sync shortcut alongside command palette, help, and quit hints without duplicated sync hints
- **AND** invoking sync SHALL acquire the locked content and replace the warning with the verified post-sync state

### Requirement: Toggle hints follow row eligibility

The detail pane's `space = toggle` hint and the list footer's `space toggle` hint SHALL be offered for a selected resource row exactly when that row's own guards allow a toggle, including an available row and an installable Marketplace discovery row, whose space opens the toggle modal from the list. A row refused for itself, such as a required, malformed, pending-verification, uninstallable Marketplace or collection-only Marketplace row, SHALL offer only its full-view hint in the detail pane, and the footer SHALL drop the toggle hint while it is selected. Transient states, such as an operation in progress or an invalid configuration, SHALL NOT remove the hints. A selected section header SHALL keep the footer toggle hint, because space toggles the section.

#### Scenario: Toggle hints follow row eligibility

- **WHEN** the selected resource row is required, collection-only, pending verification or an uninstallable Marketplace discovery row
- **THEN** the detail pane MUST NOT offer `space = toggle` and MUST still offer `enter = full view`, and the footer MUST NOT offer `space toggle`
- **AND** an eligible enabled row, an available row, an installable Marketplace discovery row and a selected section header MUST keep the toggle hints
