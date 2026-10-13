## MODIFIED Requirements

### Requirement: Shared browser brand identity
The Landing, Docs, and Marketplace surfaces SHALL present the agent-marketplace logo artwork at one consistent displayed width and optical vertical alignment in their primary navigation, select a contrasting supplied logo variant for light and dark appearance, and publish the supplied favicon at each surface's configured base path. Route-dependent navigation wrappers MAY retain different widths. The logo link MUST retain the accessible product name and existing home destination.

#### Scenario: Browser surfaces present accessible adaptive branding
- **WHEN** a user visits Landing, a Docs page with a sidebar, or Marketplace in light or dark appearance
- **THEN** the primary navigation shows the contrasting agent-marketplace logo artwork at the same displayed width and optical vertical alignment on every surface, its home link is named `agent-marketplace`, and the document references the supplied favicon from that surface's configured base path
