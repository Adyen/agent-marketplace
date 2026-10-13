## Context

The shared-origin deployment serves the VitePress landing and documentation site from `/` and the Nuxt Marketplace from a configurable application base, normally `/marketplace/`. Each frontend is built and deployed independently, so each must package its own static branding assets. Both surfaces support manually selected light and dark appearance.

## Goals / Non-Goals

**Goals:**
- Keep branding assets local to each independent frontend build.
- Resolve every asset through the owning frontend's configured base path.
- Preserve one accessible product name and the existing home link behavior.

**Non-Goals:**
- Redesign the navigation layout or landing-page content.
- Add external asset hosting, runtime asset fetching, or new dependencies.
- Use the vertical logo exports outside primary navigation.

## Decisions

- Package the supplied horizontal black and white SVGs plus the favicon SVG and PNG fallbacks in both frontend public directories. Duplication keeps each deployable artifact self-contained; a cross-build asset pipeline would add coupling for five small immutable files.
- Render both Nuxt logo variants and use the existing root `dark` class to select the visible image. This follows the application's manual appearance state instead of relying only on the operating-system media query.
- Configure VitePress's native theme logo with light and dark sources and disable the adjacent text title because the supplied horizontal mark already contains the product name.
- Build favicon and logo URLs from each frontend's configured base. Root-absolute hard-coded paths were rejected because they break standalone or prefixed deployments.

## Risks / Trade-offs

- [Duplicated assets could drift] → Keep filenames and source bytes identical in both public directories and assert both browser surfaces load the expected assets.
- [Replacing text with an image could reduce accessibility] → Preserve `agent-marketplace` as the logo link's accessible name and verify the existing home destinations.
- [A base-path error could work locally but fail behind the shared proxy] → Exercise the shared-origin E2E configuration where the Marketplace runs below `/marketplace/`.

## Migration Plan

Deploy both frontend images together. Rollback consists of reverting the frontend asset and navigation configuration changes; no stored data or API migration is involved.
