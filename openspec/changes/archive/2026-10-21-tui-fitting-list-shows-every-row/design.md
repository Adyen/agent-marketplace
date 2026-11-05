## Context

`restoreSelectionAfterRebuild` sets `listOffset = cursor - anchor.line` so the selected item stays on its screen line. `clampListOffset` then only scrolls far enough to keep the cursor visible. It never lowered the offset when every row would fit, so a positive offset could remain with blank space below the last row.

## Goals / Non-Goals

**Goals:** never hide rows above the viewport when the whole list fits.

**Non-Goals:** filling trailing space in overflowing lists. Near the end of a long list the anchored line can still leave blank space below; keeping the selection steady there is the existing, specified behavior.

## Decisions

- **Reset to the top only when everything fits.** The first attempt lowered the offset whenever the rows from the offset onward left space below. That also moved the anchored line in overflowing lists and broke the specified stage anchoring (`TestVerifiedRowsInsertedAboveSelectionKeepItOnTheSameScreenLine`). Resetting only when the full list fits fixes the reported state and leaves overflowing lists unchanged.
- **Clarify the stage scenario instead of keeping a contradiction.** Its test used a 120x40 model where all rows fit and forced an offset, which is exactly the reported state. The scenario now names an overflowing list, and the test uses a 24-row terminal so its list overflows.

## Risks / Trade-offs

- [Selected item moves on screen when a fitting list grows above it] → intended: showing every row beats keeping a line when nothing needs to scroll.
