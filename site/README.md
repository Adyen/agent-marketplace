# Website

This directory contains the VitePress landing page and documentation for
`agent-marketplace`. The public-site profile serves the landing page at `/` and
documentation at `/docs/`. The documentation-only profile serves the same
pages below `/docs/` beside a self-hosted Nuxt Marketplace at `/`.

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

Build the documentation-only profile for a self-hosted Marketplace:

```bash
make site-docs-build
```

This target writes the generated files to `site/.vitepress/dist-docs/` with
all owned routes and assets below `/docs/`.

To preview the public-site profile:

```bash
cd site
pnpm run preview
```

To preview the documentation-only profile:

```bash
cd site
SITE_PROFILE=docs pnpm run preview
```

## Structure

```text
site/
├── index.md                         # Route shell for /
├── docs/                            # Public documentation, rendered at /docs/
│   ├── index.md                     # Documentation overview
│   ├── quick-start.md
│   ├── install.md                   # More install options
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
reference, and explanation. A few entry pages (overview, quick start,
more install options, and glossary) sit directly in `docs/`. Pages render
through the VitePress default theme, so they inherit the shared design tokens
without extra styling. Internal design notes stay in the repository's top-level
`docs/` directory and are not published.

When adding a page:

1. Create the markdown file under the matching `docs/<quadrant>/` directory
   (`tutorials`, `how-to`, `reference`, or `explanation`). Registry-author and
   operator how-to guides go in `docs/how-to/author/` and
   `docs/how-to/operator/`.
2. Add one entry for it to the `sidebar` in `.vitepress/config.mts`, inside
   the subgroup that matches its topic (for example, How-to guides →
   Consumers → Troubleshoot and repair). The sidebar is ordered by hand and
   does not pick up new pages automatically. Subgroups only affect the
   sidebar, so keep the file in its quadrant directory. If you add a subgroup,
   use the same heading in the matching list in `docs/index.md`. The sidebar
   also drives `llms.txt` and the Markdown copies, and the build fails if a
   page has no sidebar entry.
3. Link other site pages with relative `.md` paths. Link repository files
   outside `site/` by their full GitHub URL, because relative links outside
   `site/` do not resolve on the website and the build fails on dead links.

## Maintainer notes

- `site/src/pages/landing/data/harnesses.json` is a checked-in fixture with no
  generator. Keep it aligned with `pkg/harness-compilation/target` by hand.
- If a `pnpm-workspace.yaml` exists in a parent directory (for example your
  home directory), pass `--ignore-workspace` to `pnpm` so the site resolves its
  own lockfile.
- The public-site profile must not link to a local Marketplace application.
  The documentation-only profile links to the self-hosted Marketplace at `/`.
  Keep its documentation links below `/docs/`.
