## Context

The Website workflow already validates the public and documentation-only
VitePress profiles. The public site supports `SITE_BASE`, including project-path
hosting. It currently never uploads or deploys an artifact.

## Decisions

Use GitHub's native configure, upload-artifact, and deploy Pages actions. This
keeps generated files out of Git and uses the job-scoped GitHub token and OIDC
instead of a personal access token. A separate `gh-pages` branch or external
hosting would add state and credentials without serving this requirement.

Keep the existing path-filtered `main` push and pull-request triggers, plus
manual dispatch. Configuration, artifact upload, and deployment are restricted
to non-pull-request runs on `main`. PRs and other manually selected branches
build at `/agent-marketplace/` without querying Pages.

For publishing runs, use the base path returned by Pages configuration, appending
the required slash. An empty base path must become `/`, not the project-path
fallback, so an apex or subdomain works after it is configured in Pages.

Build the public profile once, check its generated routes and assets, and
validate documentation parity against the documentation-only build before
uploading only `.vitepress/dist`. Deployment depends on that successful build.
Only deployment receives `pages: write` and `id-token: write`. Main-branch
workflow runs are serialized without cancelling an active deployment.

## HTTPS And Rollout

Repository settings and DNS remain explicit administrator actions, not pipeline
side effects. The default `github.io` URL has managed HTTPS. A custom domain
requires ownership verification, the Pages custom-domain setting, matching DNS,
certificate provisioning, and Enforce HTTPS. Rebuild after changing the domain
so the generated base matches the newly configured URL.

## Verification

- Parse the workflow in Go tests to guard triggers, publication conditions,
  permissions, environment, action pins, and artifact-validation ordering.
- Build and test the public artifact at `/` and `/agent-marketplace/`, including
  local navigation and asset targets.
- Preserve documentation-only containment and public/documentation parity.
- Run strict OpenSpec checks, `make validate`, and an OCR delegation-mode review.
