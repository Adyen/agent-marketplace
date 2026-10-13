## ADDED Requirements

### Requirement: Versioned per-user view preferences

The TUI SHALL read view settings once at startup from `$XDG_STATE_HOME/agent-marketplace/tui.json`, falling back to `~/.local/state/agent-marketplace/tui.json`. Version 1 SHALL store `view.groupBy` and `view.thenBy`, with Status and None defaults. Fold state SHALL remain session-only. Only acceptance of changed settings SHALL write this file, using atomic replacement. Preference loading and saving SHALL NOT access authored configuration or acquisition locks. A missing file SHALL use defaults silently. An unreadable, malformed, unsupported-version, or invalid-axis file SHALL use defaults with one footer warning and SHALL remain untouched until the user confirms changed settings. A save failure SHALL be visible and SHALL preserve the previous file without failing the TUI.

#### Scenario: Confirmed preferences survive restart without config writes

- **WHEN** a user confirms changed grouping settings and restarts with an XDG state root or the home-directory fallback
- **THEN** the versioned preference file SHALL load the saved grouping, folds SHALL not persist, and authored config and acquisition locks SHALL remain untouched

#### Scenario: Missing invalid unreadable and failed-save preference files

- **WHEN** the preference file is missing, malformed, unsupported, invalid, or unreadable, the user cancels or accepts unchanged preferences, or an atomic save fails
- **THEN** startup SHALL continue with defaults and a warning only for a bad existing file, cancellation and unchanged acceptance SHALL write nothing, and failure SHALL leave existing bytes unchanged with visible feedback
