## ADDED Requirements

### Requirement: Shared paging for empty-query Marketplace discovery

The GUI SHALL use the shared Marketplace pager and catalog cache for its existing empty-query discovery. Accepted results SHALL become available progressively. A later-page failure SHALL leave accepted results usable, mark discovery incomplete, and SHALL NOT write a complete catalog cache entry for the failed load. Existing endpoint policy and request-generation checks SHALL apply to all pages.

#### Scenario: Empty-query discovery receives multiple Marketplace pages

- **WHEN** the existing GUI discovery runs without a search query against an allowed catalog with more than one page and a later page is delayed
- **THEN** the first page MUST be available before the later page finishes and the later resources MUST become available without entering a search query

#### Scenario: A failed later page leaves empty-query discovery incomplete

- **WHEN** a later page fails after an allowed Marketplace has returned results for an empty-query discovery request
- **THEN** the GUI MUST retain the received results and show the catalog as incomplete, and a new load MUST NOT treat the failed partial result as a complete cached catalog
