## Context

The primary header is sticky and 64px high. The filter rail uses CSS-only positioning and begins around 213px from the viewport top before reaching its 88px sticky offset.

## Goals / Non-Goals

**Goals:**

- Use CSS only for desktop rail positioning and overflow.
- Keep the rail below the sticky header.
- Keep its final control reachable both before and after sticky positioning engages.

**Non-Goals:**

- Dynamically maximizing rail height after it reaches the sticky position.
- Changing mobile filters or resource-list scrolling.

## Decisions

Use a fixed 88px sticky offset: 64px for the header and 24px spacing. Limit rail height to `max(0px, calc(100vh - 240px))`; the 240px reserve covers its initial desktop position and a bottom gutter. Remove the element ref, lifecycle listeners, animation-frame scheduling, and CSS custom property.

## Risks / Trade-offs

- [The rail remains shorter after sticky positioning engages] → Accept unused vertical space in exchange for a simple CSS-only implementation that keeps filter-only scrolling visible at every page position.

## Migration Plan

Deploy the CSS simplification and responsive regressions together. Rollback restores runtime measurement; no data migration is required.
