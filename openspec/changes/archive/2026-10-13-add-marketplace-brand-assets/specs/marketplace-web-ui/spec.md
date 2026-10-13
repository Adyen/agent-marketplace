## ADDED Requirements

### Requirement: Shared browser brand identity
The Marketplace and documentation surfaces SHALL present the agent-marketplace logo in their primary navigation, select a contrasting supplied logo variant for light and dark appearance, and publish the supplied favicon at each surface's configured base path. The logo link MUST retain the accessible product name and existing home destination.

#### Scenario: Browser surfaces present accessible adaptive branding
- **WHEN** a user visits the Marketplace or documentation surface in light or dark appearance
- **THEN** the primary navigation shows the contrasting agent-marketplace logo, its home link is named `agent-marketplace`, and the document references the supplied favicon from that surface's configured base path
