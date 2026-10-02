# Shared design

`semantic-colors.css` is the framework-neutral source of truth for the colors
that the landing site and the Marketplace UI use. Its values come from the
landing site's light and dark green palettes.

Each application owns an adapter:

- `site/.vitepress/theme/styles/design-tokens.css` maps shared roles to
  VitePress and landing-site tokens.
- `web/app/assets/css/design-tokens.css` maps shared roles to Tailwind and
  Nuxt UI tokens. It imports `web/app/assets/css/marketplace-theme.css` after
  the shared file to supply Marketplace-only surface and text values.

Application styles import their adapter, and each adapter imports the shared
file directly. Do not copy or generate this file into either application, and
do not import one application's adapter from the other.

Add framework-neutral color roles here. Keep typography, spacing, radii, and
framework variables in the relevant adapter. Components should use shared roles
or adapter tokens instead of raw color literals.

Marketplace surface overrides belong in the Marketplace-only theme, not in this
shared file, so that the landing-site colors stay unchanged.

The default token values are the light palette. Adding the `dark` class to an
ancestor selects the dark palette. Both applications set it on the document
element and store an explicit choice under the `vitepress-theme-appearance`
key, so the selected theme carries over when you move between the landing site
and the Marketplace.

Each application owns its top navigation presentation. The VitePress default
theme styles the documentation site, while
`web/app/assets/css/navigation.css` styles the Marketplace header, responsive
menu, and appearance switch. Keep links and theme state in application code,
where each application's router and rendering runtime controls them.
