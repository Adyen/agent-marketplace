## Why

The landing page shows the TUI twice. The Marketplace web UI appears only as a hand-built terminal-framed mock in "For organizations", and that mock has drifted from the real catalog. Real screenshots, regenerated from the Nuxt app against committed fixtures, let the page present the web UI as a first-class part of the product.

## What Changes

- The hero gains **Terminal** and **Web** tabs above the product preview. Terminal keeps the current TUI preview and is selected by default; Web shows a browser-framed image of the real catalog. The caption follows the selected tab.
- "For organizations" replaces its terminal-framed mock with a flat browser frame that shows the catalog and a resource detail page with a pinned revision digest and a clean Security result. The three benefit cards and the Deployment guide link remain.
- A Playwright capture script runs the real Nuxt app against committed fixtures and writes light and dark images of each view at desktop and narrow widths. `make site-screenshots` exposes it, and the output is committed under `site/public/landing/`.
- Each web UI image has a light and a dark variant. The visible variant follows VitePress's `.dark` class on `html`, not `prefers-color-scheme`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-web-ui`: adds requirements for the hero preview tabs, the browser-framed web UI depiction on "For organizations", and light/dark depiction parity for regenerated screenshots.

## Impact

- `site/src/pages/landing/sections/` (`HeroSection.vue`, `ForOrganizationsSection.vue`) and `site/src/pages/landing/components/` (reuse of `LandingTabList.vue`; `TerminalFrame.vue` remains for the TUI depictions).
- `site/public/landing/` gains the committed screenshot assets.
- `web/tests/e2e/` gains the capture script, fixtures, and the landing browser spec. No runtime behavior or styles of the Marketplace web app change.
- `Makefile` gains `site-screenshots`; `site/README.md` documents when to rerun it.
- `site/tests/` and `web/tests/e2e/` gain `// spec:` bound tests for the new scenarios.
