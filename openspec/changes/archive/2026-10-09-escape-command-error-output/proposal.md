## Why

Every CLI command prints its returned error through Cobra as `Error: ...` without escaping. A repository configuration value such as a source name reaches that line verbatim, so a cloned repository can set the terminal title, clear the screen or reorder text when a user runs `status`, `list`, `sync` or any other failing command. `doctor` prints configuration errors the same way. Tables, warnings and telemetry status already escape the same values (#257, #259).

## What Changes

- Route every error returned by the command tree through the terminal-data policy the TUI already applies to errors it hands to Cobra. Printable Unicode stays; controls become visible escapes.
- Keep the line breaks that separate joined errors, and keep `errors.Is`/`errors.As` identity.
- Apply the same escaping to `doctor`'s error lists.

## Capabilities

### New Capabilities

- `command-error-output`: terminal-safe command error text.

## Impact

Human-readable error text only, under confirmed R3.3-errors / #432. Ordinary printable errors print byte-identically. No change to exit codes, error identity, machine-readable output, configuration parsing or selection.
