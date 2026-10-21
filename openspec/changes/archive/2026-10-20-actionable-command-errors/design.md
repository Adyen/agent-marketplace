## Context

Remediation exists in three disconnected places: resolver warnings implement `Remediation() string` and print `→` lines, version refusals implement it on `V4ValidationError` and are appended to the CLI error text, and the TUI errors screen falls back to typed-cause and category copy. CLI command errors otherwise print Cobra's bare `Error: <text>`, and `doctor` prints `error: <text>` with nothing after it. About 200 stable diagnostic codes exist; most have no guidance anywhere.

## Goals / Non-Goals

**Goals:**
- One hint lookup shared by the CLI, `doctor`, and the TUI.
- Code-specific guidance for every stable consumer diagnostic code, enforced by a guard test.
- Configuration validation errors that name the authored file and line in plain words.

**Non-Goals:**
- Hand-written guidance for uncoded error sites; they get a command-aware fallback.
- Hints in JSON or other machine-readable output.
- Changing warning `→` remediation lines.
- Server-side Marketplace codes (`marketplace.find.*`, `marketplace.policy.*`, `marketplace.ingest.*`, serve and admin) and harness-compilation translation diagnostics.
- Renaming any diagnostic code.

## Decisions

**A single lookup package.** A new leaf package `internal/remedy` owns the `Remediable` interface (`resolver.Remediable` becomes an alias) and `For(err error, command string) string`. Lookup order: the first nonempty `Remediation()` in the error chain; then the code catalog for the first error in the chain that exposes a stable code; then the command-aware fallback. Coded error types expose their code through one small interface (`DiagnosticCode() string`); types that already carry a `Code` field gain that method rather than being special-cased in the lookup. Alternative considered: put hints on each error type. Rejected because codes are shared across types (for example `lock.*` appears on `lockfile.Error` and `LockViewError`) and a catalog keyed by code is the only shape a guard test can check exhaustively.

**Context-aware catalog entries.** Each entry is a function of a small context (file, line, layer scope, lock path, resource locator) extracted from the error, so guidance names the real file and command, for example `agent-marketplace lock --scope repo`. Organization scope always yields administrator guidance. Entries that need a scope they cannot determine fall back to wording that names the file instead of guessing a flag.

**Rendering at the boundary.** `command_errors.go` already wraps every command hook. It now prints the hint on its own `hint: ` line after the error text instead of the `  → ` suffix, splits joined errors so each gets its own hint, and deduplicates identical hints. Usage and flag errors raised by Cobra before `RunE` are routed through the same printer via `SetFlagErrorFunc` and the `Args` wrapper, with a `--help` hint. `doctor` reuses the same function for its error lists. The TUI's `diagnosticRemediation` calls the catalog after `Remediable` and before its typed-cause copy; the TUI's lock-view copy moves into the catalog so both surfaces print one text.

**Guard test.** A test parses non-test Go sources under `internal/` and `cmd/` for consumer code literals and declared code constants with the in-scope prefixes and fails when any has no catalog entry. A second test renders every entry with sample context and asserts it is nonempty, names `agent-marketplace` or a concrete action, never suggests editing organization files, deleting a lock, or `update` to bypass verification.

**Message text.** `V4ValidationError` gains the authored file. `Error()` prints `<code>: <file>:<line>: <message>` when the file is known and keeps the current form otherwise. Messages built by the Marketplace lock projection and other high-traffic validation sites drop internal terms such as "binding", "evidence", and "projection".

## Risks / Trade-offs

- Changing `Error()` text breaks tests that assert exact messages → update those assertions; codes and `errors.As` identity are unchanged, and scripts are documented to match on codes.
- A wrong hint is worse than none → every catalog entry with a command is covered by the rendering test, and the lock-mismatch hint is proven end to end by following it.
- Cobra flag errors bypass `RunE` → handled by `SetFlagErrorFunc`; covered by the usage-error scenario.
- The issue-785 change on the same seam is unmerged → this change is stacked on it and must be rebased when it lands.
