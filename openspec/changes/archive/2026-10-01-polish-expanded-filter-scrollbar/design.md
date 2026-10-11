## Context

The collapsed preview has room for seven registry rows. The expanded desktop filter list is independently scrollable, but its default scrollbar is visually heavy and its previous height leaves usable viewport space empty.

## Goals / Non-Goals

**Goals:**

- Fill the collapsed filter area with seven registry options before **Show all**.
- Extend the expanded scrollport through the available sticky viewport area.
- Keep a discoverable but visually quiet scrollbar using CSS only.

**Non-Goals:**

- Detect active scrolling with JavaScript.
- Change mobile scrollbar behavior.

## Decisions

Set the initial registry limit to seven. Bound the expanded desktop filter component with `max-height: max(0px, calc(100dvh - 152px))`, accounting for the sticky header, filter heading, and bottom spacing. Use a 6px WebKit scrollbar and `scrollbar-width: thin`; keep its thumb transparent at rest and reveal it through `:hover` or `:focus-within`. Hover and focus are the CSS-only interaction signals available across the supported browser styles.

## Risks / Trade-offs

- [Always-visible scrollbar settings at the operating-system level can reserve a narrow track] → Keep the track transparent and only 6px wide.
- [Hover is unavailable on touch input] → Native touch scrolling remains functional, while keyboard focus reveals the scrollbar for non-pointer navigation.

## Migration Plan

Deploy the constant, CSS, and browser regressions together. Rollback restores five collapsed options and the default scrollbar.
