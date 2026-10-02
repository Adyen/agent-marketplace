# Website

This directory contains the VitePress website for `agent-marketplace`. The
landing page is served at `/` and the public documentation at `/docs/`. The
separate Nuxt application in `web/` is served at `/marketplace/` by the
combined Docker proxy.

## Prerequisites

- Node.js
- pnpm 10.26.0 (pinned by `packageManager` in `package.json`)

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

This target installs dependencies with `--frozen-lockfile` and writes the
generated files to `site/.vitepress/dist/`. The build fails on dead links.

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
├── docs/                            # Public documentation, rendered at /docs/
│   ├── index.md                     # Documentation overview
│   ├── terminal-basics.md           # Terminal basics for new users
│   ├── install.md
│   ├── quick-start.md
│   ├── glossary.md                  # One heading per term
│   ├── tutorials/
│   ├── how-to/                      # Consumer guides, plus author/ and operator/
│   ├── reference/
│   ├── explanation/
│   └── images/                      # Screenshots used by pages
├── .vitepress/
│   ├── config.mts                   # VitePress, navigation, and docs sidebar
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
    └── shared/
        ├── components/              # UI shared by multiple routes (layout, footer)
        └── site.ts                  # Site-wide constants
```

Keep `.vitepress/theme/index.ts` limited to registering page layouts and global
styles. Route-specific UI, state, and API clients belong under `src/pages`.
Move code to `src/shared` only after more than one route uses it.

## Documentation

Public documentation lives in `docs/` and follows the
[Diátaxis](https://diataxis.fr/) split into tutorials, how-to guides,
reference, and explanation. A few entry pages (overview, terminal basics,
install, quick start, and glossary) sit directly in `docs/`. Pages render
through the VitePress default theme, so they inherit the shared design tokens
without extra styling. Internal design notes stay in the repository's top-level
`docs/` directory and are not published.

When adding a page:

1. Create the markdown file under the matching `docs/<quadrant>/` directory
   (`tutorials`, `how-to`, `reference`, or `explanation`). Registry-author and
   operator how-to guides go in `docs/how-to/author/` and
   `docs/how-to/operator/`.
2. Add one entry for it to the `sidebar` in `.vitepress/config.mts`. The
   sidebar is ordered by hand and does not pick up new pages automatically.
3. Link other site pages with relative `.md` paths. Link repository files
   outside `site/` by their full GitHub URL, because relative links outside
   `site/` do not resolve on the website and the build fails on dead links.

## Maintainer notes

- `site/src/pages/landing/data/harnesses.json` is a checked-in fixture with no
  generator. Keep it aligned with `pkg/harness-compilation/target` by hand.
- If a `pnpm-workspace.yaml` exists in a parent directory (for example your
  home directory), pass `--ignore-workspace` to `pnpm` so the site resolves its
  own lockfile.
- Keep links to the Nuxt UI rooted at `/marketplace/`, because the shared
  reverse proxy owns that path in local and production-like deployments. Do not
  pass those links through VitePress `withBase`, which would nest them below
  `SITE_BASE`.
