## Why

Marketplace slash accents currently depend on each surrounding font's `/` glyph and authored spaces, so their angle, weight, and spacing differ across Landing, catalog titles, resource coordinates, and detail titles. The slash is one cross-surface brand-system concern and needs one geometry, intentional optical treatment, and one authoritative requirement.

## What Changes

- Reproduce the green stroke from the shared horizontal logo as a font-relative CSS shape.
- Use role-specific font-relative spacing: `0.2em` for titles and coordinate separators, and `0.375em` for letter-spaced eyebrows.
- Apply a lighter optical stroke only to coordinate separators while preserving the shared angle and height.
- Render coordinate hover emphasis as one straight underline rather than a rounded inset border.
- Consolidate all slash behavior under one `marketplace-web-ui` requirement.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-web-ui`: Becomes the sole authority for slash geometry, spacing, placement, accessibility, optical correction, and hover treatment.
- `marketplace-browser`: Stops duplicating resource-coordinate slash presentation.

## Impact

The change affects shared brand CSS, Landing and Marketplace slash markup, resource-row hover styling, frontend tests, and OpenSpec ownership. Routes, resource data, accessible names, and surrounding typography remain unchanged.
