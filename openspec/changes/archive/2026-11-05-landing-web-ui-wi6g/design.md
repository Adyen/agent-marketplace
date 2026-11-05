## Context

See proposal.md for motivation. Current structure: `HeroSection.vue` renders `TuiPreview` directly; `ForOrganizationsSection.vue` wraps a hand-built catalog mock in `TerminalFrame`. `LandingTabList.vue` already implements the spec'd tab model (`idPrefix`, roving tabindex, wrapping keys, animated indicator) for the harness tabs. `site/tests/public-site.test.ts` asserts built HTML; `web/tests/e2e/playwright.config.ts` already starts the built Nuxt app, `mock-marketplace.mjs`, and the public-site preview.

## Goals / Non-Goals

Goals: one reusable tab model for both hero and harness tabs; real, regenerable, light/dark depictions; no change to the Marketplace web app runtime.

Non-goals: the "discover in the browser" section, TUI mock replacement, admin UI images (see the change's non-goals).

## Decisions

- **Reuse `LandingTabList` for the hero** with `idPrefix` `hero` and a separate `modelValue`, so hero and harness tab lists stay independent. The indicator and keyboard behavior come with the component. Alternative considered: a new hero-specific tab component, rejected as duplicate logic.
- **Theme parity by CSS, not `<picture>`.** Each web UI depiction renders two `<img>` elements (`.depiction--light`, `.depiction--dark`); CSS hides one based on `html.dark`. Alternative: `<picture media="(prefers-color-scheme)">` rejected because VitePress toggles a class and the media query would show the wrong theme.
- **Hero Web depiction is out of the critical path.** It sits in a panel that is not displayed until the Web tab is selected, and it uses `loading="lazy"` with explicit dimensions. The Terminal preview remains the first render.
- **Image format.** Prefer WebP. If the toolchain has no deterministic WebP encoder available without adding a dependency, commit optimized PNG and record the reason in the PR; the choice is made once in the capture script and the spec does not depend on it.
- **Determinism.** The capture script pins viewport, device scale factor, locale, timezone, reduced-motion emulation, fixture timestamps, and disables animations and caret blink before taking screenshots. Re-running on an unchanged tree must produce identical bytes, which the reproducibility scenario checks.
- **Fixtures** live under `web/tests/e2e/` and contain no real third-party or Adyen-internal names. The fixture set covers several registries, skills, commands, MCP servers, a collection, and one resource with a clean security assessment.
- **Test binding.** Browser behavior scenarios (hero tabs, reduced motion, dark-class switching) are bound to a Playwright spec under `web/tests/e2e/` against the public-site preview. Static HTML scenarios (dimensions, alt text, lazy loading, neutral host, benefit cards) are bound in `site/tests/public-site.test.ts`. The reproducibility scenario is bound to a test that drives the capture script and compares manifests and hashes.

## Risks / Trade-offs

- [Image bytes drift across Chromium versions] -> pin the Playwright and Chromium versions already in the repo and document in `site/README.md` when to rerun.
- [Image payload grows the public site] -> document the total added payload in the PR (AC6) and keep images compressed.
- [Hidden Web panel is not visible to keyboard users until selected] -> it is controlled by the same tab list as the Terminal preview, so it appears when its tab is focused and selected.

## Migration Plan

None. The change adds assets and replaces one section's mock; rollback is reverting the commit.

## Open Questions

None.
