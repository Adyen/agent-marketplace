## MODIFIED Requirements

### Requirement: Marketplace header navigation

The Marketplace primary header SHALL use its accessible logo link as the Marketplace home action without also showing a `Marketplace` text link. It SHALL provide a visibly marked Docs link that bypasses Nuxt routing and opens in a separate browser tab with opener isolation. Documentation links in the header, catalog footer, and CLI setup action SHALL resolve to `https://agent-marketplace.io/docs` by default. With the runtime boolean `NUXT_PUBLIC_DOCS_ENABLED=true`, they SHALL instead resolve to origin-root `/docs/`, including when the web interface uses a non-root base path. An explicit false value SHALL retain public documentation.

#### Scenario: Marketplace header offers non-redundant external documentation navigation

- **WHEN** a user views the Marketplace primary header with local documentation omitted or false
- **THEN** the logo remains a Marketplace home link and no `Marketplace` text link is shown
- **AND** the Docs link has a visible external-link marker, resolves to `https://agent-marketplace.io/docs`, opens in a new tab, and prevents the opened page from accessing its opener
- **AND** catalog footer and CLI setup documentation links resolve to that same public documentation

#### Scenario: Marketplace links to locally hosted documentation when enabled

- **WHEN** local documentation is enabled and a user follows a Marketplace documentation link
- **THEN** the header, catalog footer, and CLI setup action MUST link to origin-root `/docs/` rather than public documentation or a path beneath the web-interface base
- **AND** primary-header documentation navigation MUST open the local documentation in a new tab with opener isolation
