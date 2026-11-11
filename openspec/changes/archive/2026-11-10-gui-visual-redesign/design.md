# Design

## Context

See proposal.md for motivation. The GUI snapshot has no field that says whether updates exist. `agent-marketplace update --dry-run` already prepares an update without publishing anything and compares the previously locked and candidate resources, but that comparison lives in the CLI's `main` package, so the session core cannot call it. The local API already keeps one piece of state outside the core snapshot, Marketplace discovery, which it merges into every snapshot event and republishes when it changes.

## Goals / Non-Goals

Goals: one source of truth for "what would an update change" shared by the CLI preview and the GUI; an update check that never writes and never competes with guarded actions; presentation changes that follow `gui/DESIGN.md`.

Non-goals: changing what Update or Sync do, merging them, or syncing after install; source navigation; packaging changes other than icon assets; Windows packaging (none exists, so no `.ico` is produced).

## Decisions

- **Shared preview core.** Move the comparison part of the CLI update preview (prior/candidate resolution, comparison sides, Git lock changes, live sources) out of `cmd/agent-marketplace` into an internal package that both the CLI and the session import. The CLI keeps its rendering and its output stays byte-identical. Alternative considered: count only changed Git locks plus Marketplace pin updates from `pipeline.UpdateResult`. That is cheaper, but a lock can advance without changing any selected resource, so the hint would promise updates that change nothing.
- **What is counted.** The number of resource changes the preview reports as added or changed in the user-personal scope. Removed resources are not "updates available".
- **When the check runs.** After the final snapshot the local API accepts at launch, after an explicit refresh, and after the GUI's own completed guarded operation or mutation. Focus reloads do not start a check, so the network is not hit on every window focus. At most one check runs at a time; a trigger during a running check marks one rerun. The check does not start while a guarded operation runs. It records the operation/mutation generation it started under, and its result is dropped when a later operation or mutation began.
- **How it runs.** The same dry-run path as `update --dry-run` restricted to the user scope: pending dependency journals are refused without replay, missing Marketplace evidence is prepared in memory, and preview mode never commits. Any error leaves the count at zero and is logged at debug level only.
- **Exposure.** The snapshot DTO gains a required integer `availableUpdates` (zero when unknown), maintained by the local API next to the discovery state and republished as a snapshot event when it changes. The generated client is regenerated from `api/gui-local.yaml`. The checker is injected into the API so tests can substitute it; the GUI host wires the production checker.
- **Toolbar.** Update moves before Sync. Sync remains the only solid control. Update keeps the secondary style and shows a mono `--gui-signal` count badge only when `availableUpdates > 0`; the button's `aria-label` stays "Update".
- **Panes.** The shell grid uses CSS variables for the sidebar and inspector widths. Each pane edge gets a `role="separator"` handle with `aria-orientation="vertical"`, `aria-valuenow/min/max`, an accessible name ("Resize sidebar", "Resize inspector"), and `tabindex="0"`. Sidebar: default 260, min 200, max 400 px. Inspector: default 360, min 280, max 560 px. Arrow keys move 16 px (the direction that widens the pane widens it), Home/End jump to min/max. Pointer drag uses pointer capture. Widths are stored as integers under `am-gui-sidebar-width` and `am-gui-inspector-width`; a missing or non-numeric value uses the default, and a numeric value out of range is clamped. The folded inspector and the narrow-window overlay keep their current behavior; the overlay uses the stored width clamped to the viewport.
- **Resource action dialog.** `GuiModal` makes its icon optional and the dialog passes none. Primary text, for install: "Install <display name> from <registry>." and "After the next sync, it is available in <harness display names>." Remove, exclude, and restore get the matching plain sentences. The plan preview moves into a `<details>` disclosure, summary "Technical details", collapsed by default; the risk acknowledgement stays visible. A 16 px `--gui-signal` spinner (the toast spinner style, static under reduced motion) shows beside the status text while the plan loads or the confirmed action is submitting, and is removed on success or error. Accessible names of the buttons do not change.
- **Add source.** The Settings Add source button uses `.gui-button--secondary` with the plus icon, matching the "Add a source" empty-state button.
- **Icon.** A new vector source (`packaging/gui/icon.svg`) in the landing design's language: a rounded-square ink tile with the green signal mark. The `.icns` (via `iconutil` from a generated iconset) and every existing hicolor PNG size are regenerated from it. Filenames and packaging metadata do not change.

## Risks / Trade-offs

- [The dry-run fetches Git remotes and Marketplace indexes] → It runs in the background, at most once at a time, never on focus reload, and never during a guarded operation.
- [Moving the preview core could change CLI output] → Existing CLI preview tests must pass unchanged; the move keeps the functions' behavior.
- [Hidden plan preview hides risk information] → The risk acknowledgement toggle stays outside the disclosure.

## Migration Plan

None. Stored pane widths are new keys; absent keys use the defaults.
