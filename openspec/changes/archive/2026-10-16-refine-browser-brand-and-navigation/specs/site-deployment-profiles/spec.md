## MODIFIED Requirements

### Requirement: Navigation follows the active deployment profile

Public Landing navigation SHALL link to the local public documentation below `/docs/`. Public documentation SHALL provide navigation to the public project landing page. Documentation served by a Marketplace SHALL provide navigation to that Marketplace's configured web-interface base. The Marketplace web interface SHALL link to the canonical public documentation at `https://agent-marketplace.io/docs` in a separate browser tab.

#### Scenario: Navigation respects deployment context

- **WHEN** a user follows root or documentation navigation in either deployment profile
- **THEN** Landing Docs navigation MUST resolve below the local `/docs/` route
- **AND** public-site navigation MUST return to the public landing page
- **AND** Marketplace documentation navigation MUST return to the configured Marketplace web-interface base
- **AND** Marketplace web-interface documentation navigation MUST open `https://agent-marketplace.io/docs` in a separate tab
