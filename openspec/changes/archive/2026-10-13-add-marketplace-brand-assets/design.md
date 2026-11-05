## Context

The shared-origin deployment serves the VitePress Landing and Docs surfaces from `/` and the Nuxt Marketplace from a configurable application base, normally `/marketplace/`. Each frontend is built independently and supports light and dark appearance. The supplied horizontal SVG rendered at different widths across the three surfaces, its painted paths sit slightly high within the viewBox, and VitePress navigation wrappers legitimately vary with sidebar layout.

## Goals / Non-Goals

**Goals:**
- Keep each frontend build self-contained while using identical source assets.
- Resolve assets through each frontend's configured base path.
- Render the visible wordmark at one width and optical alignment across Landing, Docs, and Marketplace.
- Preserve one accessible product name and existing home-link behavior.

**Non-Goals:**
- Equalize route-dependent navigation wrapper widths.
- Change sidebar, navigation, search, or landing-page layout.
- Modify the supplied SVG files or use the vertical logo exports.
- Add external asset hosting, runtime fetching, or dependencies.

## Decisions

- Package the supplied horizontal black and white SVGs plus favicon SVG and PNG fallbacks in both frontend public directories so each deployable artifact remains self-contained.
- Use VitePress's light/dark logo support and the Marketplace's existing root `dark` class to select the contrasting variant.
- Define a shared 200px logo-width token because that is the narrowest observed Docs-sidebar constraint; set height to auto so the SVG viewBox controls aspect ratio.
- Define a shared 1px downward optical offset because the painted paths sit slightly high inside the image viewBox; translate the logo without changing document flow.
- Keep VitePress navigation wrapper geometry untouched and test the visible `<img>` width and transform rather than wrapper dimensions.
- Build favicon and logo URLs from each frontend's configured base and preserve `agent-marketplace` as the home link's accessible name.

## Risks / Trade-offs

- [Duplicated assets could drift] → Keep source bytes identical in both public directories and verify both browser surfaces load expected assets.
- [The wider Marketplace wordmark could crowd narrow navigation] → Use the Docs-constrained width and exercise responsive browser coverage.
- [Replacing text with an image could reduce accessibility] → Retain an explicit accessible product name and verify home destinations.
- [A future SVG may have different internal whitespace] → Keep dimensions and optical offset explicit in shared tokens and browser coverage.
- [A base-path error could pass locally] → Exercise the shared-origin deployment where Marketplace runs below `/marketplace/`.

## Migration Plan

Deploy both frontend images together. Rollback consists of reverting the branding assets and navigation configuration; no stored data or API migration is involved.
