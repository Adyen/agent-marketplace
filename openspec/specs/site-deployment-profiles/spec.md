# site-deployment-profiles Specification

## Purpose
Defines the routes, documentation parity, and navigation of the public project website and self-hosted Marketplace deployment profiles.

## Requirements

### Requirement: Public project website contains landing and documentation

The public project website SHALL serve the project landing page at `/` and the public documentation below `/docs/`. It SHALL NOT present, link to, or prefetch a Marketplace web interface at a local `/marketplace/` route.

#### Scenario: Public project site exposes only landing and documentation

- **WHEN** a user visits the public project website
- **THEN** `/` MUST present the project landing page
- **AND** `/docs/` MUST present the documentation
- **AND** public navigation and page metadata MUST NOT advertise or prefetch a local `/marketplace/` application

### Requirement: Marketplace deployment owns the origin root

A self-hosted Marketplace deployment SHALL serve the Marketplace web interface at `/` and its HTTP API and discovery document at their defined same-origin paths. Documentation hosting SHALL be opt-in and disabled by default in the supplied Helm and Compose deployments. Helm's `docs.enabled: true` and the supplied Compose documentation override SHALL enable the documentation service and local web-interface documentation links together. Enabled documentation SHALL be served below origin-root `/docs/`. The deployment SHALL NOT expose the project landing page. A Marketplace operator MAY explicitly configure the web interface at a non-root base path.

#### Scenario: Marketplace deployment uses root UI with same-origin services

- **WHEN** a Marketplace operator deploys the Marketplace profile with local documentation enabled
- **THEN** `/` MUST present the Marketplace web interface rather than the project landing page
- **AND** `/docs/` MUST present the documentation
- **AND** Marketplace API and discovery requests MUST reach the Marketplace service rather than the web-interface fallback
- **AND** documentation requests MUST reach the documentation service rather than the web-interface fallback

#### Scenario: Marketplace documentation hosting is disabled by default

- **WHEN** a Marketplace operator renders a supplied Helm or Compose deployment without enabling local documentation
- **THEN** no documentation workload or service MUST be deployed
- **AND** the web-interface documentation setting MUST be false
- **AND** the supplied Compose proxy MUST start without a documentation upstream and return 404 for `/docs` and `/docs/` descendants

#### Scenario: Marketplace documentation hosting is explicitly enabled

- **WHEN** a Marketplace operator sets Helm `docs.enabled: true` or applies the supplied Compose documentation override
- **THEN** a documentation workload and its internal service connectivity MUST be configured
- **AND** the web-interface documentation setting MUST be true
- **AND** the Compose proxy MUST wait for healthy documentation and route `/docs/` descendants to that service

### Requirement: Embedded documentation is path-contained

The documentation-only artifact SHALL be independently deployable below `/docs/`. Every generated local documentation route, asset URL, and fallback URL owned by that artifact SHALL remain below `/docs/`; the explicit profile-root navigation target and intentional links to external origins are exempt. The artifact SHALL NOT publish the project landing page.

#### Scenario: Documentation build is self-contained below docs

- **WHEN** the documentation-only artifact is built and mounted at `/docs/`
- **THEN** every local route and asset required to browse the documentation MUST resolve below `/docs/`
- **AND** requesting the artifact MUST NOT expose the project landing page

### Requirement: Deployment profiles publish equivalent documentation

For a given source revision, the public project website and Marketplace deployment profiles SHALL publish the same documentation route inventory and article content from the shared documentation source. Profile-specific navigation and chrome MAY differ but SHALL NOT change the documented product behavior.

#### Scenario: Both profiles publish the same documentation

- **WHEN** both deployment profiles are built from the same source revision
- **THEN** they MUST expose the same documentation page set
- **AND** corresponding pages MUST contain the same article content apart from declared profile-specific navigation and chrome

### Requirement: Navigation follows the active deployment profile

Public Landing navigation SHALL link to the local public documentation below `/docs/`. Public documentation SHALL provide navigation to the public project landing page. Documentation served by a Marketplace SHALL provide navigation to that Marketplace's configured web-interface base. Marketplace documentation links SHALL use `https://agent-marketplace.io/docs` by default and origin-root `/docs/` when local documentation is enabled, independent of the web-interface base path. The primary header documentation link SHALL open in a separate browser tab with opener isolation.

#### Scenario: Navigation respects deployment context

- **WHEN** a user follows root or documentation navigation in either deployment profile
- **THEN** Landing Docs navigation MUST resolve below the local `/docs/` route
- **AND** public-site navigation MUST return to the public landing page
- **AND** Marketplace documentation navigation MUST return to the configured Marketplace web-interface base
- **AND** Marketplace web-interface documentation navigation MUST open the public documentation by default or origin-root `/docs/` when enabled

### Requirement: Root-profile upgrades preserve legacy Marketplace links

The supplied root-mounted Marketplace reverse-proxy profiles SHALL redirect
safe legacy `/marketplace` browser routes to the corresponding root-mounted
web-interface routes without changing the origin or dropping the query
string. A legacy browser path containing a decoded backslash or ASCII control
character SHALL be rejected with HTTP 400 and no `Location` header. Encoded
controls and backslashes confined to the query string SHALL NOT cause that
path rejection. API and documentation
routes SHALL retain their dedicated owners.

#### Scenario: Legacy Marketplace links reach the root-mounted interface

- **WHEN** an operator deploys a supplied root-mounted Marketplace reverse-proxy profile and a user requests a safe `/marketplace` or descendant browser route
- **THEN** the proxy MUST redirect to the corresponding root-mounted route on the same origin while preserving the query string
- **AND** API and documentation requests MUST continue to reach their dedicated services

#### Scenario: Unsafe legacy browser paths never redirect

- **WHEN** a legacy Marketplace browser path contains a literal or percent-encoded backslash, including either case of its percent encoding, or a percent-encoded ASCII control character
- **THEN** every supplied root-mounted proxy profile MUST return HTTP 400 without a `Location` header
- **AND** ordinary legacy routes and query strings containing percent-encoded controls or backslashes MUST retain their normal same-origin redirect behavior

### Requirement: Deployment configuration migrations fail explicitly

Deployment packaging SHALL reject obsolete configuration keys whose silent acceptance would omit or misroute a required service. The failure SHALL identify the replacement configuration, while current configuration SHALL continue to render the documented services and routes.

#### Scenario: Legacy documentation values fail with migration guidance

- **WHEN** a Marketplace operator renders the Helm chart with the obsolete `site` or `service.site` values
- **THEN** rendering MUST fail with guidance naming the corresponding `docs` or `service.docs` replacement
- **AND** rendering with current values MUST produce documentation and web services at their configured routes

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

### Requirement: Documentation publishes an LLM-readable index

Each deployment profile that publishes documentation SHALL publish `llms.txt` at its profile root: the public site at its site base, and the documentation-only artifact at `/docs/`. The index SHALL list every documentation page in navigation order. Each entry SHALL link to a Markdown copy of that page that is served beside the page's HTML route and below the profile's base. A Markdown copy SHALL contain the page's source without its front matter.

#### Scenario: LLM index lists every documentation page

- **WHEN** a deployment profile is built
- **THEN** its `llms.txt` MUST list every documentation page exactly once, in the sidebar's navigation order
- **AND** each link MUST resolve to a generated Markdown file below the profile's base

#### Scenario: Markdown copies mirror documentation sources

- **WHEN** a documentation page is published as a Markdown copy
- **THEN** the copy MUST equal the page source with its leading front matter removed
- **AND** images referenced by the copy MUST resolve to generated files

#### Scenario: Both profiles publish the same Markdown copies

- **WHEN** both deployment profiles are built from the same source revision
- **THEN** they MUST publish the same Markdown copy inventory with identical contents
- **AND** their `llms.txt` MUST be identical apart from the profile base prefix
