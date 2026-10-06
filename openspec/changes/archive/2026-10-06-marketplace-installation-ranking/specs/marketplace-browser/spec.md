## ADDED Requirements

### Requirement: Browse by reported installation adoption

The Marketplace browser SHALL request installation ordering for resource discovery, preserve text relevance before adoption, and render available aggregate installation counts including an observed zero. Count presentation SHALL explain that it describes reporting installations with the resource enabled in their latest configuration within 30 days. Unavailable counts SHALL be absent. Kind and registry filters and incremental loading SHALL preserve the requested ordering.

#### Scenario: Browser displays counts while preserving filtered pagination

- **WHEN** a user browses resources, searches by text, applies kind or registry filters, or loads more results
- **THEN** the browser SHALL request installation ordering for every search page and preserve the API's result order
- **AND** it SHALL display available installation counts, including zero, with their reported-adoption meaning
- **AND** items without counts SHALL NOT display a fabricated zero or block browsing
