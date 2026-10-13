## Context

Measured across local and shared-origin viewports, the supplied wordmark is 217.41px wide on Landing, constrained as low as 200px on Docs pages with a sidebar, and fixed at 154px in Marketplace. The highlighted VitePress `.title` wrappers have additional route-dependent layout width and are not the logo artwork.

## Goals / Non-Goals

**Goals:**
- Keep the visible SVG artwork exactly the same width on Landing, Docs, and Marketplace.
- Preserve the supplied SVG aspect ratio and light/dark variants.
- Keep the width policy in one shared design token.

**Non-Goals:**
- Equalize route-dependent navigation wrapper widths.
- Change sidebar, navigation, or search layout.

## Decisions

- Use 200px as the shared artwork width because it is the narrowest observed Docs-sidebar constraint and fits without changing that navigation layout.
- Put the width in a shared brand token imported by both frontend token adapters.
- Set logo height to auto on both frontends so the SVG view box remains the aspect-ratio authority.
- Test the rendered visible `<img>` width on Landing, a Docs page with a sidebar, and Marketplace. Wrapper measurements are intentionally excluded.

## Risks / Trade-offs

- [The Marketplace wordmark becomes wider than before] → The 200px target fits the narrow navigation alongside its menu control, and responsive browser coverage verifies containment.
- [VitePress defaults could override custom dimensions] → Use a theme-level selector and verify rendered image geometry in the browser.
