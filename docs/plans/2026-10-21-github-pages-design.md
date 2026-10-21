# Public Website On GitHub Pages

## Goal

Publish the existing landing page and public documentation from `main` using
GitHub's native Pages deployment. Keep the running Marketplace service separate:
GitHub Pages hosts static files, not the Go service or its database.

## Approach

Extend the existing Website workflow rather than introduce a second publisher.
Relevant pushes and manual runs on `main` configure Pages, build and validate
the site, upload the public artifact, and deploy through the `github-pages`
environment. Pull requests and manual runs on other branches only validate.

The build reads the base path from Pages configuration. This supports both
`https://adyen.github.io/agent-marketplace/` and a custom domain at `/` without
hardcoding a domain in site content. Changing the domain requires a new build.
Only deployment receives Pages write and OIDC permissions; main runs do not
cancel an active deployment.

## Alternatives

- A generated `gh-pages` branch adds Git state and branch publishing management
  without improving this static deployment.
- An external host can run the Marketplace service, but is unnecessary for the
  public landing page and documentation.

## Operations And Validation

An administrator enables the GitHub Actions Pages source. For a custom domain,
verify ownership, save the Pages custom domain, configure matching DNS, wait for
GitHub's managed certificate, and enable Enforce HTTPS. No certificate secrets or
manual renewals belong in the workflow. Detailed setup is explained in the
implementation handoff, not in a repository README.

Workflow regression tests guard publication conditions, permissions, artifact
ordering, and native actions. Generated-site tests verify navigation and assets
at both root and project paths; existing checks retain documentation parity.
The OpenSpec change `deploy-public-site-to-github-pages` records the normative
publication and URL behavior.
