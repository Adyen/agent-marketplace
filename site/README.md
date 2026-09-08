# Website

This directory contains the VitePress website for `agent-marketplace`. The
landing page is available at `/`; the marketplace application will live at
`/marketplace`.

## Prerequisites

- Node.js
- pnpm 10.26.0

## Run locally

From the repository root:

```bash
make site-dev
```

VitePress prints the local URL when the development server starts, usually
`http://localhost:5173`.

Alternatively, run the site commands directly:

```bash
cd site
pnpm install
pnpm run dev
```

## Build

Create a production build:

```bash
make site-build
```

The generated files are written to `site/.vitepress/dist/`.

To build for a project-path deployment, set `SITE_BASE`:

```bash
SITE_BASE=/agent-marketplace/ make site-build
```

To preview a completed build:

```bash
cd site
pnpm run preview
```

## Structure

```text
site/
├── index.md                         # Route shell for /
├── marketplace.md                   # Add with the /marketplace implementation
├── .vitepress/
│   ├── config.mts                   # VitePress and navigation configuration
│   └── theme/
│       ├── index.ts                 # Thin VitePress-to-page bridge
│       └── styles/                  # Design tokens and VitePress overrides
└── src/
    ├── pages/
    │   ├── landing/
    │   │   ├── LandingPage.vue
    │   │   ├── components/          # Landing-only building blocks
    │   │   ├── data/                # Landing-only fixtures and transforms
    │   │   └── sections/            # Top-level landing sections
    │   └── marketplace/             # Add with the marketplace feature
    │       ├── MarketplacePage.vue
    │       ├── api/
    │       ├── components/
    │       ├── composables/
    │       └── state/
    └── shared/
        ├── components/              # UI shared by multiple routes
        └── site.ts                  # Site-wide constants
```

Keep `.vitepress/theme/index.ts` limited to registering page layouts and global
styles. Route-specific UI, state, and API clients belong under `src/pages`.
Move code to `src/shared` only after more than one route uses it.

## Maintainer notes

- `site/src/pages/landing/data/harnesses.json` is currently a checked-in fixture. Keep
  it aligned with `pkg/harness-compilation/target` until generation and drift
  checking are added.
- Docs and Marketplace currently link to landing-page sections. Point
  Marketplace at `/marketplace` when that route is implemented.
