# Website

This directory contains the VitePress website for `agent-marketplace`. The
landing page is available at `/` and the latest published documentation at
`/docs/`. Development documentation is at `/docs/preview/`, and permanent
release snapshots are at `/docs/<version>/`. The separate Nuxt application in
`web/` is served at `/marketplace/` by the combined Docker proxy.

## Prerequisites

- Node.js 24
- pnpm 10.26.0

## Run locally

From the repository root:

```bash
make site-dev
```

VitePress prints the local URL when the development server starts, usually
`http://localhost:5173`. Open `/docs/preview/` to see edits to `docs/`.
The development server regenerates the preview when those source files change;
release snapshots do not change.

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
├── docs/                            # Development documentation, rendered at /docs/preview/
│   ├── index.md                     # Documentation overview
│   ├── tutorials/
│   ├── how-to/
│   └── reference/
├── versions/                        # Immutable content, assets, sidebar, and manifest per release
├── scripts/docs.mjs                 # Snapshot creation, integrity checks, and build preparation
├── .vitepress/
│   ├── config.mts                   # VitePress, versioned routes, and search
│   ├── sidebar.mjs                  # Hand-written development sidebar
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
[Diátaxis](https://diataxis.fr/) split into tutorials, how-to guides, and
reference. Pages render through the VitePress default theme, so they inherit
the shared design tokens without extra styling. Internal design notes stay in
the repository's top-level `docs/` directory and are not published.

When adding a page:

1. Create the markdown file under the matching `docs/<quadrant>/` directory.
2. Add one entry for it in `.vitepress/sidebar.mjs`. Keep links rooted at `/docs/`;
   the build scopes them to each version.
3. Link other repository files by their GitHub URL; relative links outside
   `site/` do not resolve on the website, and the build fails on dead links.

## Prepare documentation for a release

Commit the release's documentation changes before creating its snapshot. From
`site/`, use the intended version without the leading `v`:

```bash
pnpm docs:snapshot 3.2.0
pnpm docs:check v3.2.0
pnpm run build
```

The snapshot command reads documentation and images from Git `HEAD`, captures
the current `.vitepress/sidebar.mjs`, and prints `Created immutable docs snapshot
3.2.0 from <commit>`. Uncommitted documentation is not included. The check must
report `Verified <count> immutable documentation snapshot(s)`; it fails if the
snapshot differs from the working documentation or sidebar.

Commit the new `versions/3.2.0/` directory before tagging the release. Do not
change documentation between preparing that snapshot and tagging. Website CI
checks stable and RC tags against their snapshots; it does not create snapshots,
commit changes, or deploy the site. Publish through the existing site-image
build and deployment process.

For an RC, use a name such as `3.2.0-rc.14`. To capture documentation from a
specific existing commit or tag, supply it as a second argument:

```bash
pnpm docs:snapshot 3.2.0-rc.14 v3.2.0-rc.14
```

When using an older ref, first ensure the current sidebar matches that release.
The initial `3.2.0-rc.13` snapshot was imported from its tag with the matching
sidebar. Earlier, incomplete documentation releases are not backfilled.

Snapshots are append-only: content, images, and navigation cannot be replaced
under an existing version. Correct development docs in `docs/` and publish a new
version instead. The build verifies every snapshot's SHA-256 manifest; website
CI also rejects changes to snapshots already present in the comparison commit.

The highest semantic version among the snapshots is Latest, including RCs
newer than the last stable release. Its content appears at `/docs/` and at its
permanent numbered path. Internal Markdown links and search remain within the
selected version. Switching versions keeps the same page when it exists;
otherwise it opens that version's overview. Page fragments are dropped when
switching because headings can differ between releases.

## Verify documentation changes

From `site/`:

```bash
pnpm test
pnpm typecheck
pnpm docs:check
pnpm exec playwright install chromium
pnpm run build
pnpm test:e2e
SITE_BASE=/agent-marketplace/ pnpm run build
SITE_BASE=/agent-marketplace/ pnpm test:e2e
```

The tests cover snapshot integrity, overwrite refusal, route fallback, scoped
search, images, source links, browser history, and mobile navigation. Browser
tests must pass for both base paths. To use an existing Chrome/Chromium binary,
set `PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH` or `AGENT_BROWSER_EXECUTABLE_PATH` and
omit the browser installation command.

Build preparation writes only to `.vitepress/cache/content/`, which is ignored
by Git and Docker. Normal builds need neither Git history nor network access to
retrieve documentation. Git is required only for snapshot creation and the
append-only comparison:

```bash
node scripts/docs.mjs check-history origin/main
```

The comparison prints `Published documentation is unchanged from origin/main`
when existing snapshots have not changed.

## Maintainer notes

- `site/src/pages/landing/data/harnesses.json` is currently a checked-in fixture. Keep
  it aligned with `pkg/harness-compilation/target` until generation and drift
  checking are added.
- If a `pnpm-workspace.yaml` exists in a parent directory (for example your
  home directory), pass `--ignore-workspace` to `pnpm` so the site resolves its
  own lockfile.
- Keep links to the Nuxt UI rooted at `/marketplace/`; the shared reverse proxy
  owns that path in local and production-like deployments. Do not pass those
  links through VitePress `withBase`, which would nest them below `SITE_BASE`.
