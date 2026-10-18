## Context

The CLI `update` passes the selection guard to the lock lifecycle. The TUI dependency lifecycle builds its own upgrade lock mode for every source and does not enable the guard, so a TUI update still publishes a lock whose selections no longer resolve.

## Goals / Non-Goals

**Goals:** the TUI update refuses under the same rule and with the same text as `update`, with no byte changes on refusal.

**Non-Goals:** guard logic or wording changes, CLI changes, new TUI UI, and TUI sync, install, or toggle changes.

## Decisions

- **Enable the existing guard.** The TUI upgrade lock mode sets the same guard flag that `update` sets. Sync does not, so a TUI sync stays unchanged.
- **Reuse the refusal text.** The TUI shows the lifecycle error as the update failure, so the refusal reaches the user without new presentation code. Its CLI remediation commands still apply from a terminal.

## Risks / Trade-offs

- A TUI update that changes a lock pays the guard's two extra resolutions, as `update` does.
