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

A self-hosted Marketplace deployment SHALL serve the Marketplace web interface at `/`, documentation below `/docs/`, and its HTTP API and discovery document at their defined same-origin paths. The deployment SHALL NOT expose the project landing page. A Marketplace operator MAY explicitly configure the web interface at a non-root base path.

#### Scenario: Marketplace deployment uses root UI with same-origin services

- **WHEN** a Marketplace operator deploys the default Marketplace profile
- **THEN** `/` MUST present the Marketplace web interface rather than the project landing page
- **AND** `/docs/` MUST present the documentation
- **AND** Marketplace API and discovery requests MUST reach the Marketplace service rather than the web-interface fallback
- **AND** documentation requests MUST reach the documentation service rather than the web-interface fallback

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

Public Landing navigation SHALL link to the local public documentation below `/docs/`. Public documentation SHALL provide navigation to the public project landing page. Documentation served by a Marketplace SHALL provide navigation to that Marketplace's configured web-interface base. The Marketplace web interface SHALL link to the canonical public documentation at `https://agent-marketplace.io/docs` in a separate browser tab.

#### Scenario: Navigation respects deployment context

- **WHEN** a user follows root or documentation navigation in either deployment profile
- **THEN** Landing Docs navigation MUST resolve below the local `/docs/` route
- **AND** public-site navigation MUST return to the public landing page
- **AND** Marketplace documentation navigation MUST return to the configured Marketplace web-interface base
- **AND** Marketplace web-interface documentation navigation MUST open `https://agent-marketplace.io/docs` in a separate tab

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
