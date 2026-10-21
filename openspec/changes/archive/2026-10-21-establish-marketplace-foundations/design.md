## Context

Landing and Docs use Adyen Sans and Mono through VitePress-specific assets and
tokens. Marketplace imports shared colors but still uses system typography.

## Decisions

Move the approved font files to `design/fonts` and define the small set of
cross-surface roles in `design/foundations.css`. Site keeps its `--lp-*`
adapter; Marketplace keeps its Nuxt UI and availability adapters. Neither
surface imports the other's framework-specific stylesheet.

Register each variable face with its supported weight range and disable
synthetic styles. Nuxt's Vite development server explicitly allows the shared
design directory, while production continues to bundle the imported assets.
The Site and Marketplace retain their existing component and semantic-color
ownership.
