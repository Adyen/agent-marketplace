## Context

See proposal.md. The TUI already has separate progressive startup and ordinary snapshot consumers, Huh forms drained synchronously by modal handlers, and testable model/update and PTY boundaries. The approved fixes keep those boundaries and preserve the final resolution, materialization dry run, and assessed inventory path.

## Goals / Non-Goals

**Goals:** Restrict provisional-stage callbacks to progressive startup; keep modal and palette input behavior stable; restore harness option visibility after resize; strengthen the accepted review tests using real consumers and isolated environments.

**Non-Goals:** Changing the single-view v4 direction, restoring tabs, adding production wheel counters or test-only production APIs, changing dependencies, or changing Marketplace resolution and materialization policy.

## Decisions

- Pass an explicit progressive flag to the staged snapshot reader. The ordinary snapshot wrapper supplies false; the staged startup wrapper supplies true. Build the pipeline inspection options locally, register browsing/provisional/config callbacks only for progressive startup, and always retain the resolved callback and final assessed snapshot. This avoids duplicate speculative local catalog/config/identity work on ordinary reload without replacing authoritative resolution or inventory.
- Mark only the fixed toggle and config-editor Select fields non-filterable. Leave the synchronous form-command drain unchanged; bounded subprocess tests exercise slash through the actual Model handlers and ensure Escape still cancels without a write.
- Centralize harness multiselect height policy in a small shared helper called before either full or compact modal rendering: full layout uses the complete option count; compact layout uses `max(1, height-modalFrameRows-2)`. Height is presentation state; selected IDs and cursor remain owned by the existing model/form.
- In the command palette, dispatch only literal `up` and `down` messages as navigation. All other keys, including `j` and `k`, continue to the focused text input.
- For wheel delivery evidence, wrap the real Model in a test-only Bubble Tea model, feed encoded SGR bytes through an `os.Pipe` with the normal parser, discard terminal output, disable signal handling, and bound execution with a context. Assert the exact delivered wheel-button sequence and final state; this measures messages delivered to Model.Update, not physical frames. Keep the PTY journey as an independent user-visible check with complete detail-field assertions.
- Keep Sync phase-observer tests serial and isolate HOME plus every XDG/Codex root beneath one temporary directory. Fix the palette, stale-progress, search, and theme fixtures at their existing test boundaries.

Alternatives rejected: reflective Huh-message discrimination or changing the generic command drain; shrinking the full option set after compact rendering; production wheel instrumentation; direct synthetic `Update` calls as a substitute for parser-delivery evidence; and adding a release-version or view-switch change.

## Risks / Trade-offs

- [A burst could be lost before reaching the TUI model] → Assert the exact expected button sequence at the wrapper immediately around Model.Update, and separately assert the final PTY details.
- [A Huh regression could stall synchronous command draining] → Exercise slash in a subprocess with a bounded parent timeout so the baseline failure cannot hang the suite.
- [Environment isolation could leak across tests] → Keep the two Sync tests serial and restore every changed process-global value through test cleanup.
