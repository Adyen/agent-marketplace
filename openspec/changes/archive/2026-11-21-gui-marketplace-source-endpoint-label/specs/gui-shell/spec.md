## REMOVED Requirements

### Requirement: Sidebar source labels

**Reason**: Labeling by configured source name gave one endpoint different labels across layers (a generic user key showed the host while a named organization key showed the name). Sources are now labeled by endpoint only, restated under "Sidebar Marketplace endpoint labels".

**Migration**: None for users. The restated requirement keeps the tooltip and contrast requirement.

## ADDED Requirements

### Requirement: Sidebar Marketplace endpoint labels

A sidebar Marketplace source SHALL be labeled by its endpoint host, followed by its port when one is declared and by its last path segment when present, regardless of its configured source name or whether that name is generic. The full location SHALL stay available in the row's tooltip and accessible name, and the tooltip text SHALL have at least 4.5:1 contrast against its background in the light and dark themes.

#### Scenario: Marketplace source shows its endpoint regardless of name

- **WHEN** a Marketplace source is declared under any name, including a generic `marketplace` or `marketplace-<n>` name, in any configuration layer
- **THEN** the sidebar row and the source browse header MUST show the endpoint host

#### Scenario: Source tooltip is readable in both themes

- **WHEN** the user hovers over or focuses a sidebar source row in the light or dark theme
- **THEN** a tooltip MUST show the source's full location with at least 4.5:1 contrast against its background
