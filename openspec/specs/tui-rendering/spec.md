# tui-rendering Specification

## Purpose
Defines accessible terminal rendering and lifecycle guarantees for the keyboard and mouse interface using the user's terminal color palette.

## Requirements

### Requirement: Bounded and safe rendering
The TUI SHALL fit its active view and modal within 50 columns by 12 rows and sanitize remote text, paths, diagnostics, errors, configuration values and Markdown-derived text before terminal output. Printable Unicode SHALL remain readable. Below the minimum size the TUI SHALL show only a resize-required view, suspend normal input, and allow q and Ctrl+C.

#### Scenario: Hostile input at minimum size
- **WHEN** an untrusted row name or diagnostic contains terminal control bytes and the terminal is 50×12
- **THEN** the control bytes render as visible escapes without changing terminal state or hiding required actions

#### Scenario: Undersized terminal
- **WHEN** the terminal shrinks below 50×12 during a modal
- **THEN** the resize-required view replaces the modal without accepting workflow input, and resizing back restores the prior workflow

### Requirement: Theme and composed views

The TUI SHALL use semantic status, accent, selection, muted and border roles mapped to the terminal's ANSI palette, retain distinguishable no-color status labels, and compose list, modal and notification views without hiding required keys or displacing selection. The TUI SHALL use the cell-motion mouse-reporting behavior defined by `tui-mouse-navigation` without enabling all-motion tracking.

#### Scenario: Colorless and narrow rendering

- **WHEN** the display has no color or is at the minimum supported dimensions
- **THEN** status remains readable and modal and quit controls remain within physical bounds

#### Scenario: Mouse reporting agrees with wheel navigation

- **WHEN** the normal TUI program enters the alternate screen
- **THEN** it MUST enable cell-motion mouse reporting for wheel navigation without enabling all-motion tracking

### Requirement: Terminal restoration
The TUI SHALL restore captured termios, disable bracketed paste, show the cursor, and leave the alternate screen on clean quit, handled signal, startup failure or returned error. Only the interrupt sentinel SHALL be treated as a clean interrupt; other errors SHALL preserve their identity and be escaped before printing.

#### Scenario: Error after entering alternate screen
- **WHEN** the interactive program returns an error while the alternate screen is active
- **THEN** terminal settings are restored and the original error remains detectable

### Requirement: Consistent modal frame
Every modal SHALL render in one frame whose width is bounded by the terminal, with a complete border, a title, one focus indicator per option, each accelerator shown once, and a single hint row. Paths that do not fit SHALL be shortened in the middle so their file name stays visible.

#### Scenario: Modal at common sizes
- **WHEN** any modal opens at 50×12, 80×24 or 120×34 with long configuration paths
- **THEN** every modal line fits the terminal, the border is complete, and the file names of shortened paths remain visible

### Requirement: Editor handoff screen
While an external config editor runs, the TUI SHALL show a screen on the alternate screen that names the file being edited, rather than the terminal contents from before the TUI started. After the editor exits the TUI SHALL leave that screen and resume.

#### Scenario: Editor running
- **WHEN** a user opens a config layer in an external editor
- **THEN** the terminal shows the waiting screen naming the file until the editor exits, and the pre-launch screen is not shown

### Requirement: Scope-prefixed registry-qualified resource labels

Resource list rows SHALL display the existing status glyph, a three-cell letter scope column, the resource kind, and `registry/name`, in that order. The scope column SHALL retain `G`, `P`, `L`, their combinations, `P-`, and `R`, with a blank column for available or malformed rows. It SHALL NOT use word-based scope chips or omit scope information to fit a row. The registry prefix SHALL use the recorded display name, otherwise the registry URL's last segment without its `.git` suffix, otherwise `(local)`, and SHALL NOT append registry subpaths. The detail pane SHALL retain complete registry URL and path information. Selected and unselected rows SHALL use the same label content, keep long names readable at supported dimensions, preserve Unicode, escape hostile terminal data, and highlight name matches at the correct positions after the prefix.

#### Scenario: Aligned safe labels at normal and narrow sizes

- **WHEN** configured, multi-scope, locally excluded, required, available, and malformed rows render selected and unselected at normal and minimum supported sizes, including missing registry metadata, registry subpaths, long Unicode names, search matches, and hostile registry text
- **THEN** rows show the aligned letter scope before kind and registry-qualified name, blank scopes remain aligned, subpaths are omitted, hostile text appears as visible escapes, complete names remain readable, and highlights apply to matching name characters rather than the prefix

### Requirement: Frame-paced list integration preserves TUI lifecycle outcomes

The normal TUI program SHALL use resource-list wheel presentation pacing without changing other panes' wheel behavior, editor handoff, interruption handling, or terminal restoration. A failed sync-and-quit operation SHALL still return its sanitized error with the original error identity after terminal restoration.

#### Scenario: Pacing retains terminal and error ownership

- **WHEN** the normal TUI program exits after paced resource-list input, including a sync-and-quit failure, an interrupt, or an editor handoff followed by exit
- **THEN** it MUST retain the existing terminal-restoration and interruption behavior
- **AND** a sync-and-quit failure MUST remain detectable through its original error identity without exposing raw terminal controls
- **AND** wheel input in other panes MUST keep its existing immediate rendering and routing

### Requirement: Bounded selectable grouping and bulk modals

Source and Kind sections and their nesting SHALL render within supported dimensions without changing existing scope-prefixed registry-qualified resource labels. Section labels, filters, and modal content SHALL sanitize terminal control data and preserve printable Unicode. View settings and bulk toggle SHALL use the existing bounded modal frame and keep necessary choices, confirmation, cancellation, and hard-quit controls reachable at 50×12, 80×24, and 120×34.

#### Scenario: Groupings and modals remain safe at supported sizes

- **WHEN** each grouping and either new modal renders at minimum and common terminal sizes with long Unicode or hostile source labels, paths, and filters
- **THEN** rendered output SHALL fit physical bounds, hostile controls SHALL be escaped, nested sections and resource scope columns SHALL remain readable, and required modal actions SHALL remain reachable

### Requirement: Prerequisite detail sections

The TUI detail pane SHALL show a "Requires" section listing each valid declared prerequisite of the shown resource in declaration order with exactly one marker: `met` when a selected resource with the same kind and name satisfies it, `implied` when an expansion-selected implied resource satisfies it, `preview` when expansion is disabled and the prerequisite is in the implied preview, `excluded` when its unmet reason is `excluded`, and `unmet` for any other unmet reason. The detail pane SHALL show a "Required by" section listing the selected dependents recorded in prerequisite provenance, never derived from collection provenance. A section with no entries SHALL NOT be shown. Malformed prerequisite declarations SHALL keep their existing diagnostic. Registry-authored text SHALL remain terminal-escaped and credential-bearing source hints SHALL be redacted before display.

#### Scenario: Requires section marks each prerequisite state

- **WHEN** a shown resource declares prerequisites that are met, implied, previewed, excluded, and unmet
- **THEN** the Requires section MUST list them in declaration order, each with its one corresponding marker

#### Scenario: Required by section uses prerequisite provenance

- **WHEN** a shown resource is required by selected resources and is also a collection member
- **THEN** the Required by section MUST list the requiring resources
- **AND** collection membership MUST remain shown as collection provenance and MUST NOT appear in Required by

#### Scenario: Resources without relationships show no prerequisite sections

- **WHEN** a shown resource has no valid declared prerequisites and no dependents, or only malformed declarations
- **THEN** neither section MUST be shown
- **AND** a malformed declaration MUST keep its existing diagnostic

#### Scenario: Prerequisite text is escaped and redacted

- **WHEN** a prerequisite or dependent carries terminal control sequences or a credential-bearing source hint
- **THEN** the detail pane MUST render the text terminal-escaped and the credential redacted

### Requirement: Prerequisite list markers

A TUI list row for an implied resource selected by expansion SHALL carry a `req` badge beside any existing collection badge. A row whose resource has an unmet prerequisite SHALL carry a warning marker. A resource present only in the implied preview SHALL NOT carry a `req` badge. Rows with no prerequisite relationship SHALL render unchanged.

#### Scenario: Implied and unmet rows are marked

- **WHEN** the list contains an implied resource selected by expansion and a resource with an unmet prerequisite
- **THEN** the implied row MUST carry a `req` badge and the requiring row MUST carry a warning marker

#### Scenario: Preview-only and unrelated rows are not marked

- **WHEN** expansion is disabled and a resource appears only in the implied preview, and another row has no prerequisite relationship
- **THEN** the preview resource MUST NOT carry a `req` badge
- **AND** the unrelated row MUST render unchanged
