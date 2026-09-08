# Shared design

`semantic-colors.css` is the framework-neutral source of truth for colors used
by the landing site and Marketplace UI. Its values come from the landing
site's light and dark green palettes.

Each application owns an adapter:

- `site/.vitepress/theme/styles/design-tokens.css` maps shared roles to
  VitePress and landing-site tokens.
- `web/app/assets/css/design-tokens.css` maps shared roles to Tailwind and
  Nuxt UI tokens.

Application styles import their adapter, and each adapter imports the shared
file directly. Do not copy or generate this file into either application, and
do not import one application's adapter from the other.

Add framework-neutral color roles here. Keep typography, spacing, radii, and
framework variables in the relevant adapter. Components should consume shared
roles or adapter tokens instead of introducing raw color literals.

The default token values are the light palette. Add `dark` to an ancestor to
select the dark palette. Both applications use it on the document element and
persist explicit choices under `vitepress-theme-appearance`, so the selected
theme survives navigation between the landing site and Marketplace.

VitePress's default theme owns the canonical top navigation, responsive menu,
and appearance switch. `marketplace-navigation.css` adapts the Nuxt header to
that baseline. Keep links and theme state in application code, where each
router and rendering runtime owns its behavior.
