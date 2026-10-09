## Context

`newRootCmd` leaves `SilenceErrors` false, so Cobra prints `err.Error()` of whatever a command hook returns. The TUI already wraps the error it returns to Cobra (`sanitizedExitError`) using `sanitizeTerminalData` in single-line mode. CLI commands have no equivalent.

## Decisions

1. Reuse the TUI terminal-data policy by exporting its single-line form as `tui.TerminalSafeLine`. Do not add a second escaping algorithm; `terminalSafeText` (ASCII quoting) stays for tables, where non-ASCII escaping is intended.
2. Keep Cobra as the only printer. After the tree is built, wrap each command's `Args`, `PersistentPreRunE`, `PreRunE`, `RunE`, `PostRunE` and `PersistentPostRunE` so returned errors are escaped before Cobra sees them. Silencing Cobra and reprinting would drop its usage hint for unknown commands.
3. Escape each line separately and keep `\n` between them. Configuration loads routinely join several errors; collapsing them into one line would degrade every multi-error report. Carriage returns, tabs and all other controls inside a line are escaped.
4. Wrap only when escaping changes the text, so ordinary errors keep their exact value. The wrapper unwraps to the original error.
5. Cobra-generated parse errors are not wrapped; they quote user-typed input with `%q`.

## Verification

The command regression failed at the real Cobra sink for `status`, `list` and `sync`, and at `doctor`'s configuration error list, before the change. A hook matrix proves every wrapped hook. An ordinary non-ASCII error prints unchanged. The declarative E2E case fails without the fix and passes with it.
