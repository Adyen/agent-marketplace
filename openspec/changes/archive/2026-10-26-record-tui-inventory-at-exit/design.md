## Context

`pipeline.Resolve` and `pipeline.RecordResolvedInventory` record inventory whenever the context carries telemetry. The TUI runs many pipeline operations on contexts derived from `Model.ctx`, and `applySnapshotData` records every accepted final snapshot, so a session produces one inventory per configuration it passes through.

## Decisions

- **Context suppression.** `pipeline.WithoutInventory(ctx)` marks a context with an unexported key. `recordResolvedInventory` returns early for marked contexts, which covers the automatic `Resolve` path and explicit recording. CLI `ResolveOptions` are unchanged.
- **Pending holder.** `internal/tui` keeps a pointer-shared, mutex-guarded holder on `Model` (the `Model` value is copied by Bubble Tea, so the pointer is what the exit path sees). `set` replaces the pending result with each accepted final snapshot, including failed ones, so a failed final snapshot clears any earlier candidate. `flush` records the pending result once and clears it.
- **Exit wiring.** `tui.Run` captures the unsuppressed session context for the flush and gives the model the suppressed context. The flush is deferred after the session span's `End` so it runs first; the flush happens before telemetry shutdown in `main`.
- **No timer.** Recording happens only at exit, per the decided behavior.

## Risks

- A session killed by a signal that bypasses `Run` returns records nothing. The spec allows this.
