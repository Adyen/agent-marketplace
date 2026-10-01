## Context

The primary header is sticky and 64px high. The filter rail currently measures its viewport position on scroll and resize, then passes that position to CSS.

## Goals / Non-Goals

**Goals:**

- Use CSS only for desktop rail positioning and overflow.
- Keep the rail below the sticky header and its final control reachable at the sticky position.

**Non-Goals:**

- Dynamically maximizing rail height before it reaches the sticky position.
- Changing mobile filters or resource-list scrolling.

## Decisions

Use a fixed 88px sticky offset: 64px for the header and 24px spacing. Limit rail height to `calc(100vh - 112px)`, retaining 24px below the rail. Remove the element ref, lifecycle listeners, animation-frame scheduling, and CSS custom property.

## Risks / Trade-offs

- [The rail can extend below the viewport before sticky positioning engages] → Accept this in favor of the requested CSS-only implementation; normal page scrolling moves the rail to its bounded sticky position.

## Migration Plan

Deploy the CSS simplification and responsive regression update together. Rollback restores runtime measurement; no data migration is required.
