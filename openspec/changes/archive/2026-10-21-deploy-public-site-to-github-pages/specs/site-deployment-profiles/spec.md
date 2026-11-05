## ADDED Requirements

### Requirement: Public website publication follows main

The public project website SHALL deploy through native GitHub Pages after a
successful website build and validation triggered by a relevant push to `main`
or a manual workflow run on `main`. Pull requests and manual runs on other
branches SHALL validate without uploading a Pages artifact or deploying.
Only the deployment job SHALL receive Pages write and OIDC token permissions.
A failed build or validation SHALL prevent deployment.

#### Scenario: Main publishes the validated public artifact

- **WHEN** the Website workflow runs for a relevant push to `main` or a manual run on `main`
- **THEN** it MUST validate the public site and documentation profiles before uploading the public-site artifact
- **AND** native Pages deployment MUST depend on the successful build and use the GitHub Pages environment
- **AND** no generated-content branch or personal deployment credential MUST be required

#### Scenario: Other branches and pull requests never publish

- **WHEN** the Website workflow runs for a pull request or a manual run outside `main`
- **THEN** it MUST build and validate without configuring Pages, uploading a Pages artifact, or deploying
- **AND** its build job MUST NOT receive Pages write or OIDC token permissions

### Requirement: Public website routes follow Pages configuration

A publishing build SHALL derive its deployment base from the configured GitHub
Pages URL. The public landing page, documentation navigation, and generated local
asset references SHALL work both below the default project path and at the root
of a configured custom domain.

#### Scenario: Project-path Pages site resolves navigation and assets

- **WHEN** the public site is built for `/agent-marketplace/`
- **THEN** landing, documentation, and generated local asset references MUST remain below that base and resolve to generated files

#### Scenario: Custom-domain Pages site resolves navigation and assets

- **WHEN** the public site is built for the root of a configured custom domain
- **THEN** landing, documentation, and generated local asset references MUST resolve at `/` without adding the repository name
