## MODIFIED Requirements

### Requirement: Shared browser brand identity
The Landing and Docs public-site surfaces SHALL use the approved Adyen variable
Sans and Mono fonts and the shared Agent Marketplace Green `#00D16A`, custom
Black `#121A18`, and white palette through common semantic roles in light and
dark appearance. Their shared navigation and footer SHALL use those same roles.
The Landing, Docs, and Marketplace surfaces SHALL present the
agent-marketplace logo artwork at one consistent displayed width of 160 CSS
pixels and optical vertical alignment in their primary navigation, select a
contrasting supplied logo variant for light and dark appearance, and publish
the supplied favicon at each surface's configured base path. Route-dependent
navigation wrappers MAY retain different widths. The logo link MUST retain the
accessible product name and existing home destination. Marketplace application
typography and surface colors SHALL remain owned by its separate application
theme.

#### Scenario: Browser surfaces present accessible adaptive branding
- **WHEN** a user visits Landing, a Docs page with a sidebar, or Marketplace in light or dark appearance
- **THEN** Landing and Docs use the approved Adyen variable fonts and shared Agent Marketplace palette
- **AND** their navigation and footer use the same public-site semantic roles
- **AND** the Marketplace application retains its application-owned typography and surfaces
- **AND** the primary navigation shows the contrasting agent-marketplace logo artwork at a displayed width of 160 CSS pixels and the same optical vertical alignment on every surface, its home link is named `agent-marketplace`, and the document references the supplied favicon from that surface's configured base path
