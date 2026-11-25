## Why

Compound resource status text pairs a state with a parenthesized or semicolon-joined qualifier, for example `enabled (pending sync)`, `required (org)`, and `enabled in config; verification pending`. These read as codes rather than plain words, and the GUI and TUI show the same strings.

## What Changes

- A compound status reads "<State>, <qualifier in plain words>" with no parentheses or semicolons.
- `enabled (pending sync)` becomes `enabled, waiting for sync`; `required (org)` becomes `required by org`; `enabled in config; verification pending` becomes `enabled in config, waiting for verification`; `excluded; verification pending` becomes `excluded, waiting for verification`; `available; verification pending` becomes `available, waiting for verification`.
- The TUI detail pane, the GUI rows, and the site demos use the new strings. The CLI `list` table keeps its existing `required (org)` column text, which is a separate output contract.

## Capabilities

### Modified Capabilities

- `status-reporting`: compound resource status wording.

## Impact

`internal/session/rows.go` status labels and the Go, GUI, TUI QA, and site tests and fixtures that quote them. No status values, filters, or drift semantics change.

## Release note

Compound resource statuses in the TUI and desktop app read as plain words, for example "enabled, waiting for sync" and "required by org", instead of parenthesized or semicolon-joined codes.
