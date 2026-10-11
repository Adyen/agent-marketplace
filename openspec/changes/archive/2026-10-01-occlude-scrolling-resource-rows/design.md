## Context

Resource-row links and actions use `z-index: 10`, matching the sticky catalog control wrapper. Later-painted rows can therefore appear above its background. The controls stick at 88px while navigation ends at 64px, leaving a 24px gap where rows are also visible.

## Goals / Non-Goals

**Goals:**

- Keep scrolling row content visually behind sticky catalog controls.
- Cover the gap between navigation and controls without moving visible controls.

**Non-Goals:**

- Change sticky offsets, row layout, or scrolling behavior.

## Decisions

Raise the sticky control wrapper to `z-index: 20`, still below the primary navigation at 50. Establish an isolated stacking context and add an absolutely positioned opaque `::before` backdrop extending 24px above the wrapper. The pseudo-element maps to the wrapper for hit testing and covers the navigation gap without altering layout.

## Risks / Trade-offs

- [The extended backdrop could cover content in the adjacent filter column] → Keep its horizontal inset at zero so it remains within the results column only.

## Migration Plan

Deploy the stacking CSS and browser regression together; rollback removes the pseudo-element and restores the previous z-index.
