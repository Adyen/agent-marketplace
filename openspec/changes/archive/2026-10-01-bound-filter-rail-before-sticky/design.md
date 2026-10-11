## Context

The rail starts around 213px from the viewport top in the compact desktop test layout, while its sticky position is 88px. A maximum height derived from 88px is too tall before the page scrolls.

## Goals / Non-Goals

**Goals:**

- Keep the entire rail scrollport visible when only the filters are scrolled.
- Retain a CSS-only solution.

**Non-Goals:**

- Dynamically maximize rail height after it becomes sticky.
- Change filter selection or expansion behavior.

## Decisions

Use `max-height: max(0px, calc(100vh - 240px))`. The 240px reserve covers the measured initial rail top plus a bottom gutter across the supported desktop breakpoint, while the existing `top: 88px` still controls sticky placement.

## Risks / Trade-offs

- [The rail remains shorter after becoming sticky] → Accept unused vertical space in exchange for simple CSS and correct filter-only scrolling at every page position.

## Migration Plan

Deploy the CSS constant and regression together; rollback restores the sticky-position height calculation.
