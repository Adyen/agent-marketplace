## Context

Landing and Docs use one VitePress application and one shared layout. The
Marketplace application imports the shared brand artwork but owns a separate
Nuxt theme. The approved product palette deliberately uses custom Agent
Marketplace Black `#121a18` rather than the standard Adyen black.

## Goals / Non-Goals

**Goals:**
- Give Landing and Docs one public-site visual foundation.
- Keep palette, typography, spacing, and shell roles centralized.
- Preserve VitePress navigation, appearance switching, and deployment profiles.

**Non-Goals:**
- Restyle the Marketplace application.
- Change documentation prose, routes, or navigation destinations.
- Redesign individual Landing sections in this change.

## Decisions

The shared brand file owns the three approved product colors. A public-site
token adapter maps those colors and the Adyen font faces into semantic roles
and VitePress variables. Landing and Docs share those roles because they are
one product; route-specific layout remains in Landing and shell styles.

The VitePress layout adds one public-site scope. Documentation-specific
structure is identified through VitePress's existing `VPDoc` marker rather than
duplicating route state in custom classes. Navigation and footer presentation
use the same public-site roles while VitePress continues to own their behavior.

## Risks / Trade-offs

Variable fonts add two static assets to the public site. `font-display: swap`
keeps content visible while they load. VitePress internal class selectors can
change on framework upgrades, so the production build and browser regression
retain coverage for the shared shell.

## Migration Plan

Add the font assets and tokens, apply them through the shared layout, update the
browser witness, and archive this change. No data migration is required.
