## Context

See proposal.md — Why. The TUI enables cell-motion mouse reporting, routes vertical wheel input by pointer position, and treats left-button drags in the right-hand details or file-preview pane as text selection. Left presses, motion, and releases already reach the model through the frame-paced wrapper together with the last-rendered selection snapshot.

## Goals / Non-Goals

**Goals:**

- Let a single left click select resource-list rows and bundle-tree entries using the same geometry the renderer uses, including wrapped rows and scroll offsets.
- Reuse existing keyboard actions so a click has exactly the effect of the equivalent key.
- Keep drags outside the right-hand pane inert and leave wheel routing, frame pacing, and text selection unchanged.

**Non-Goals:**

- Double-click detection, clicks in dialogs, overlays, the command palette, the footer, or the right-hand pane.
- Changing the mouse-reporting mode or enabling all-motion tracking.

## Decisions

- **Act on release in the press cell.** A left press over the list or tree records a pending click; motion to another cell cancels it, and a release in the same cell on the same pane performs the action. Acting on press was rejected because it would make a drag that starts in the list change the selection, which the existing ignored-drag contract forbids.
- **Click the selected row to open it.** Bubble Tea reports no click count, so a second click on the already-selected resource stands in for double-click and dispatches the same detail action as Enter. Section headers toggle on the first click, matching Enter on a header.
- **Derive hit-testing from render geometry.** Resource rows map through the list offset and the cumulative row-height table already used by wheel and page navigation; leftover rows below the last fully rendered item are ignored. Bundle entries map through the tree scroll offset below the root label row.
- **Route before text selection.** Navigation clicks are recognized first; every other event continues through the unchanged text-selection handler, so right-pane drags, clipboard writes, and selection invalidation keep their behavior. A navigation click clears any lingering right-hand highlight.

## Risks / Trade-offs

- Geometry drift between renderer and hit-testing could select the wrong row. Hit-testing reuses the renderer's layout and height helpers, and Go and real-terminal tests click wrapped and scrolled rows.
- Asynchronous refreshes between press and release can change the rows under the pointer. The release resolves against the current state, which is what the user sees; a pane change, key press, or wheel event between press and release cancels the click.

## Migration Plan

No persisted state or configuration changes. Rolling back removes click navigation without user-data migration.
