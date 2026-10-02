## Context

See proposal.md — Why. The mouse-navigation contract already uses cell-motion reporting for pointer-targeted wheel input, while this change adds selection to the right-hand details and file-preview panes. Selection must be based on the displayed text and geometry, and must not alter wheel routing or resource-list frame pacing.

## Goals / Non-Goals

**Goals:**

- Keep selection state small and bound to the pane content and dimensions it represents.
- Map pointer cell positions to complete displayed Unicode graphemes, clip selection to pane content, and reuse one rendering path for highlighting and extraction.
- Deliver copied text through Bubble Tea's existing clipboard command.

**Non-Goals:**

- Change mouse-reporting mode, wheel handling, or resource-list pacing.
- Select text in lists, bundle trees, footers, or overlays, or add word/line selection and drag-edge auto-scroll.
- Add clipboard dependencies, configuration, or Ghostty/Herdr changes.

## Decisions

- **Keep cell-motion reporting and handle selection in the TUI.** Track the left-button press, motion, and release in `Model.Update`; retain the existing wheel dispatch and pacing paths. Alternate-scroll was rejected because it loses pointer coordinates in Herdr and makes wheel-generated keys indistinguishable from real arrow keys. Enabling all-motion reporting is unnecessary.
- **Store a compact, frame-bound selection.** Keep anchor and end in terminal-cell coordinates, the plain-text snapshot and pane/selection identity, and dragging/copied-highlight state. Bind the selection to the last-rendered pane bounds; use a last-rendered `View.OnMouse` message only if required to keep those bounds aligned with the visible frame.
- **Select displayed text, not source markup.** Build the cell-to-text mapping from the rendered selectable region using existing ANSI helpers and `uniseg`. Clip to true content dimensions and preserve complete graphemes. Reuse the final render helper for cropping, extraction, and highlight placement; use the existing theme surface/onSurf styles or reverse video for visible highlighting.
- **Copy on release through the existing clipboard transport.** Send the selected plain text with `tea.SetClipboard`; do not add a clipboard package. A click without motion does not copy. Do not use `viewport.SetHighlights`, which is search-oriented and scrolls to make highlights visible.
- **Invalidate at the update boundary.** Clear stale selection when pane or selection identity, dimensions, or plain rendered content changes; explicitly clear on wheel input and scroll keys. Clip an active drag to its originating pane. Escape clears selection before its normal action, while Ctrl+C keeps its hard-exit behavior. Leave the existing resource-list pacing helper unchanged.

## Risks / Trade-offs

- ANSI styling and wide or combining graphemes can make cell-to-text mapping inaccurate. Derive mapping and highlighting from the same ANSI-aware, grapheme-aware displayed-text representation, then cover forward/backward and Unicode cases with focused tests.
- Resizes or asynchronous content replacement can leave a stale highlight or copy the wrong text. Bind selection to content identity and dimensions, invalidate at update boundaries, and verify the encoded terminal journey.
- Clipboard behavior depends on terminal or multiplexer OSC52 support. Use the existing Bubble Tea transport and assert exact output bytes without changing terminal configuration.
- Mapping work can affect input or rendering latency. Keep the state and mapping scoped to the visible selectable region and compare input/render benchmarks against the baseline.

## Migration Plan

No persisted state, configuration, or clipboard dependency is introduced. Rolling back the TUI change removes the interaction without requiring user-data migration.
