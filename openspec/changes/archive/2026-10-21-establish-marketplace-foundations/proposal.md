## Why

Landing and Docs use the approved Adyen fonts through VitePress-owned tokens,
while Marketplace still uses system typography. The browser surfaces need one
shared source for fonts and low-level visual roles without coupling their
framework-specific themes.

## What Changes

- Move the approved Adyen Sans and Mono assets into shared `design/fonts`.
- Define shared font, content-width, gutter, focus, geometry, and motion roles.
- Keep separate Site and Marketplace adapters over those foundations.
- Preserve the existing browser routes, content, controls, and status semantics.

## Impact

The change affects shared design assets and browser typography. It does not
change APIs, persisted data, CLI behavior, or Marketplace decisions.
