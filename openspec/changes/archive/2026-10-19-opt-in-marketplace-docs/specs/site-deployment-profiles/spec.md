## MODIFIED Requirements

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

### Requirement: Navigation follows the active deployment profile

Public Landing navigation SHALL link to the local public documentation below `/docs/`. Public documentation SHALL provide navigation to the public project landing page. Documentation served by a Marketplace SHALL provide navigation to that Marketplace's configured web-interface base. Marketplace documentation links SHALL use `https://agent-marketplace.io/docs` by default and origin-root `/docs/` when local documentation is enabled, independent of the web-interface base path. The primary header documentation link SHALL open in a separate browser tab with opener isolation.

#### Scenario: Navigation respects deployment context

- **WHEN** a user follows root or documentation navigation in either deployment profile
- **THEN** Landing Docs navigation MUST resolve below the local `/docs/` route
- **AND** public-site navigation MUST return to the public landing page
- **AND** Marketplace documentation navigation MUST return to the configured Marketplace web-interface base
- **AND** Marketplace web-interface documentation navigation MUST open the public documentation by default or origin-root `/docs/` when enabled
