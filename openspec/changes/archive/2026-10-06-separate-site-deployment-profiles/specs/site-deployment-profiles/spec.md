## Purpose

Defines the routes, documentation parity, and navigation of the public project website and self-hosted Marketplace deployment profiles.

## ADDED Requirements

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

Public documentation SHALL provide navigation to the public project landing page. Documentation served by a Marketplace SHALL provide navigation to that Marketplace's root. The Marketplace web interface SHALL provide a same-origin documentation link below `/docs/`.

#### Scenario: Navigation respects deployment context

- **WHEN** a user follows root or documentation navigation in either deployment profile
- **THEN** public-site navigation MUST return to the public landing page
- **AND** Marketplace documentation navigation MUST return to the Marketplace web interface
- **AND** Marketplace web-interface documentation navigation MUST resolve to the documentation served by that Marketplace
