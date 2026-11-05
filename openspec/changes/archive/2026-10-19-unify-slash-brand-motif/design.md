## Context

The shared horizontal logo's green path has a `5.73 / 12.6` width-to-height ratio and a 15.3-degree slant. Existing accents use a font glyph whose geometry and spaces vary by local font. Resource coordinates also need a lighter optical stroke beside medium-weight monospace text.

## Goals / Non-Goals

**Goals:**
- Preserve one logo-derived angle and font-relative geometry.
- Keep spacing proportional to local typography.
- Support deliberate optical correction without changing the mark's identity.
- Keep slash presentation in one authoritative requirement.

**Non-Goals:**
- Make differently sized typography produce identical CSS-pixel marks.
- Change surrounding font sizes, routes, or accessible names.
- Apply coordinate optical correction to titles or eyebrows.

## Decisions

The base mark uses a `0.219em` horizontal slant run, `0.145em` stroke, and `0.8em` height. Its total `0.364em` width reproduces the green logo path. CSS custom properties separate slant from stroke so coordinate separators can use a `0.12em` stroke while retaining the same angle and height.

Spacing follows semantic roles:
- Title prefixes use `0.2em` trailing space.
- Coordinate separators use `0.2em` on each side.
- Letter-spaced uppercase eyebrows use `0.375em` trailing space.

Coordinate links use a one-pixel pseudo-element positioned beneath the complete coordinate. This produces a continuous straight underline without following the link's rounded focus boundary.

All slash behavior lives in the `marketplace-web-ui` requirement `Shared slash brand motif`. Focused scenarios retain separate test witnesses for general surfaces, coordinates, prefixed detail titles, and unprefixed detail titles.

## Risks / Trade-offs

The coordinate separator is intentionally thinner than the exact logo stroke. CSS clipping can rasterize slightly differently at small font sizes, but the font-relative dimensions and optical stroke are browser-tested.

## Migration Plan

Replace glyph-based classes with the shared CSS shape and semantic modifiers, update the existing test witnesses, then archive this change to consolidate the normative requirements. No data migration is required.

## Open Questions

None.
