## Why

The public landing page and documentation already build in CI, but no workflow
publishes them. Maintainers need native GitHub Pages deployment from `main`
without a generated-content branch, deployment credentials, or a separate host.

## What Changes

- Extend the Website workflow to publish its validated public-site artifact
  through GitHub Pages after relevant pushes to `main` or a manual run on `main`.
- Keep pull requests and manual runs on other branches validation-only.
- Resolve the deployment base from Pages configuration so the default project
  URL and a custom domain use the correct routes and assets.
- Explain repository activation, domain verification, DNS, and managed HTTPS in
  the implementation handoff rather than a repository README.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `site-deployment-profiles`: define publication from the default branch and
  correct public-site URLs for project-path and custom-domain hosting.

## Impact

- `.github/workflows/site.yml`, site artifact tests, workflow regression tests,
  and internal design notes.
- No changes to the CLI, Marketplace service, self-hosted documentation, or
  frontend design.
- A repository administrator must select GitHub Actions as the Pages source.
