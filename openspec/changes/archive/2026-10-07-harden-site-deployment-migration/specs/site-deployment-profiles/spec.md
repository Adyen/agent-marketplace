## ADDED Requirements

### Requirement: Root-profile upgrades preserve legacy Marketplace links

The supplied root-mounted Marketplace reverse-proxy profiles SHALL redirect legacy `/marketplace` browser routes to the corresponding root-mounted web-interface routes without changing the origin or dropping the query string. API and documentation routes SHALL retain their dedicated owners.

#### Scenario: Legacy Marketplace links reach the root-mounted interface

- **WHEN** an operator deploys a supplied root-mounted Marketplace reverse-proxy profile and a user requests `/marketplace` or a descendant browser route
- **THEN** the proxy MUST redirect to the corresponding root-mounted route on the same origin while preserving the query string
- **AND** API and documentation requests MUST continue to reach their dedicated services

### Requirement: Deployment configuration migrations fail explicitly

Deployment packaging SHALL reject obsolete configuration keys whose silent acceptance would omit or misroute a required service. The failure SHALL identify the replacement configuration, while current configuration SHALL continue to render the documented services and routes.

#### Scenario: Legacy documentation values fail with migration guidance

- **WHEN** a Marketplace operator renders the Helm chart with the obsolete `site` or `service.site` values
- **THEN** rendering MUST fail with guidance naming the corresponding `docs` or `service.docs` replacement
- **AND** rendering with current values MUST produce documentation and web services at their configured routes

## MODIFIED Requirements

### Requirement: Navigation follows the active deployment profile

Public documentation SHALL provide navigation to the public project landing page. Documentation served by a Marketplace SHALL provide navigation to that Marketplace's configured web-interface base. The Marketplace web interface SHALL provide a same-origin documentation link below `/docs/`.

#### Scenario: Navigation respects deployment context

- **WHEN** a user follows root or documentation navigation in either deployment profile
- **THEN** public-site navigation MUST return to the public landing page
- **AND** Marketplace documentation navigation MUST return to the configured Marketplace web-interface base
- **AND** Marketplace web-interface documentation navigation MUST resolve to the documentation served by that Marketplace
