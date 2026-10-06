## Context

The desktop rail uses a fixed sticky top offset and a viewport-based maximum height. Before sticky positioning engages, the rail's actual top is lower because the catalog introduction remains visible, so the fixed calculation overstates the available height.

## Goals / Non-Goals

**Goals:**

- Keep the rail above the viewport bottom at every page position.
- Preserve native independent rail scrolling and sticky behavior.
- Avoid unthrottled layout work during page scrolling.

**Non-Goals:**

- Changing mobile filters, filter selection, or result scrolling.
- Introducing a fixed catalog or split-pane page height.

## Decisions

Measure the rail's current viewport top in the browser and expose it through a CSS custom property. CSS remains responsible for subtracting the bottom gutter and setting maximum height. Recalculate at most once per animation frame on window scroll and resize, so the rail grows to its existing sticky-height limit as the page moves upward without extending behind the viewport beforehand.

A smaller fixed maximum height was rejected because it wastes viewport space after the rail becomes sticky and depends on introduction copy height. A fully JavaScript-managed height was rejected because it would duplicate the CSS gutter policy.

## Risks / Trade-offs

- [The server-rendered frame briefly uses the fallback sticky calculation] → Measure immediately on mount before normal user interaction.
- [Scroll events can be frequent] → Coalesce measurements with `requestAnimationFrame` and perform one geometry read and one custom-property write.

## Migration Plan

Deploy with the responsive regression. Rollback removes the measurement lifecycle and restores the fixed CSS fallback; no data migration is required.
