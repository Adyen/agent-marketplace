## Context

The collapsed filter list already limits registries so **Show all** is visible. Unconditional `max-height` and `overflow-y` make that short list scroll independently, diverging from the earlier natural page behavior.

## Goals / Non-Goals

**Goals:**

- Let collapsed filters and resources move together with page scrolling.
- Render every registry after expansion and make that long list independently scrollable.
- Use CSS for all positioning and overflow behavior.

**Non-Goals:**

- Add geometry measurement or scroll listeners.
- Change mobile filter scrolling.

## Decisions

Expose the existing `showAllRegistries` state as `data-expanded` on the filter component root. The desktop page stylesheet applies no height or overflow to the collapsed rail. It applies a viewport-bounded `max-height` and `overflow-y: auto` only to the expanded component root, scoped to the desktop media query. This leaves the Filters heading visible while the kind and registry controls scroll.

The data attribute is only a selector hook for existing click state; it introduces no new runtime behavior.

## Risks / Trade-offs

- [The expanded controls use less height than the full sticky rail] → Reserve the fixed header, catalog position, Filters heading, and bottom gutter so the expanded scrollport remains visible before and after sticky positioning.

## Migration Plan

Deploy the selector hook, conditional CSS, fixture, and browser regressions together. Rollback restores unconditional rail overflow.
