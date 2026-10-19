## Context

See `proposal.md` for motivation and `specs/tui-mouse-navigation/spec.md` for the behavior contract. The errors screen is a full-screen viewport fitted above a footer. Text selection already snapshots displayed lines and region identity, applies highlights to rendered frames, and emits clipboard writes through the TUI's terminal input path; that region currently covers details and file previews, not the errors screen.

## Goals / Non-Goals

**Goals:**

- Reuse the existing selection state, displayed-text extraction, highlighting, and clipboard path for the errors body.
- Keep the selection snapshot aligned with the rendered errors frame, its viewport offset, and the footer boundary.
- Preserve the existing cell-motion mouse input, wheel routing, frame pacing, and other selection regions.

**Non-Goals:**

- Make help, confirmation, or Marketplace consent overlays selectable.
- Change diagnostic production, sanitization, clipboard transport, mouse reporting, configuration, or filesystem behavior.

## Decisions

- Give the active errors view a full-width selection region at the top of the terminal whose height ends immediately before the footer. Use the existing rendered, fitted errors frame as the selection snapshot and highlight source so extraction follows displayed output rather than diagnostic source strings.
- Identify an errors selection by active pane, terminal dimensions, and errors viewport offset. This prevents a selection from surviving a resize, screen switch, or keyboard scroll. Content replacement is detected by the existing frame-refresh snapshot comparison, avoiding a separate cancellation mechanism.
- Route native terminal mouse events through the existing frame-paced adapter and selection engine. The adapter's snapshot must describe the active errors frame; the same engine then handles wrapped Unicode, highlight rendering, text extraction, and the existing clipboard command. Do not add a second selection or clipboard implementation.
- Keep the existing mouse-reporting and clipboard mechanisms unchanged. A separate mouse mode or clipboard dependency would duplicate established behavior and risk changing selection in other views.

## Risks / Trade-offs

- [Risk] A mismatch between the errors frame and its selection snapshot could include the footer or copy text that is not displayed. → Build both from the fitted errors view and verify the actual encoded terminal drag path, including footer exclusion.
- [Risk] A stale snapshot could outlive an errors scroll or diagnostic refresh. → Include the viewport offset in the region identity and rely on the existing frame-refresh comparison for changed displayed lines; cover cancellation and exits with model and terminal-path tests.
- [Risk] Sharing the selection region dispatch could affect existing panes or overlays. → Keep the errors region active only while the errors pane is displayed and retain the existing behavior for every other region.
