## Context

The normal TUI runs `Model` directly through Bubble Tea and the terminal guard. Bubble Tea calls `View` after each input message, independently of its terminal drawing frame rate. Resource-list wheel updates are cheap; full list, detail, and footer construction dominates the measured burst drain. The existing final-result handler reads `Model.syncQuitErr`, so introducing a program wrapper must preserve access to that final model.

The disposable raw-input spike identifies the bottleneck, but does not prove physical-terminal latency. The older Bubble Tea baseline loses events under the same extreme raw SGR burst, so its timings are not an equal-work comparison. Fresh delivery measurements compare the current pre-fix commit with the integrated fix using the same input and outcome assertions.

## Goals / Non-Goals

Goals are ordered one-entry wheel navigation, frame-paced list presentation, a trailing final repaint, immediate other updates, and unchanged runtime error and terminal ownership. Retain deterministic regression tests, a labeled full-path benchmark, and a real-terminal journey.

Do not change scroll policy, resource state, dependencies, other panes' wheel behavior, or existing configuration and materialization logic. Do not drop events or merge opposite-direction deltas. Do not make wall-clock performance thresholds correctness gates.

## Decisions

1. Add a small `framePacedModel` at program construction with `newFramePacedModel(inner tea.Model)`. Keep the inner model, cached `tea.View`, a per-instance clock, last frame time, and one generation-tagged pending repaint. Every real update still reaches the inner model exactly once and its returned model and command remain authoritative.
2. Pace only accepted vertical wheel events when the inner model is the resource-list `Model` before and after the update, at a supported terminal size and without the command palette overlay. Non-list models and other panes bypass pacing. Reuse the existing wheel handler for all movement, bounds, modifiers, and pointer routing.
3. Refresh expensive presentation at `time.Second / 60`. Within an interval, keep the cached view and schedule one trailing frame. At a due frame, render the newest inner state. Other messages immediately invalidate the old pending generation and publish a fresh view. Preserve all view metadata, including alternate-screen and mouse modes. Do not start recurring timers.
4. Wire the wrapper through `runTUIModel`. Unwrap its final inner model before `finishTUIRun` inspects sync-and-quit failure. Leave terminal restoration around the complete program lifetime. Other callers and probe models retain their existing outcomes.
5. Test with a controlled clock and real resource-list models. Cover both boundaries, wrapped rows, final repaint without further input, stale timers, immediate key/resize/snapshot/pane updates, executable commands, and final error identity. Exercise the actual program-construction seam so an unused wrapper cannot satisfy the tests.
6. Keep the lead-authored raw-input measurement recipe as the starting point for a smaller durable full-path benchmark. Its setup uses an isolated model and pipe input, encoded wheel events, independently expected counts and selection, and labeled discard output. Retain before/after artifacts outside the repository. The direct real-terminal journey proves the displayed selected resource and reversals without a machine-dependent latency assertion.
7. The mouse-navigation contract remains one entry or one line per event. Replace only the obsolete rendering-spec prohibition on mouse tracking with the already implemented cell-motion contract. Update existing TUI documentation with behavior rather than host-specific measurements.

## Risks / Trade-offs

- A cached frame can lag selection by one frame interval. Keys and asynchronous state changes force a current view and must not act on a copied obsolete model.
- Old timer messages can arrive after a pane change or newer refresh. Generation invalidation prevents them from reviving stale content.
- Returning the wrapper as the final program model could swallow `syncQuitErr`. Final-result unwrapping and error-identity tests are required.
- Burst measurement excludes physical terminal painting when output is discarded. Real-terminal evidence must be described separately, and complete event counts are required for every compared run.
- The prototype paces all wheel messages. Production narrows it to the active resource list so bundle navigation and overlays do not acquire new timing behavior.
