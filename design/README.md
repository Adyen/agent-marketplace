# Shared design colors

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

The default token values are the light palette. Add `dark` or `am-theme-dark`
to an ancestor to select the dark palette. The VitePress adapter follows its
theme class; the Marketplace UI uses `am-theme-dark` because it is dark-only.
