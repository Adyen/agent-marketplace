## ADDED Requirements

### Requirement: Complete asynchronous Marketplace startup browsing

For validated configuration, the TUI SHALL start cache-first background discovery of all discoverable resources from configured allowed Marketplace endpoints without a search query. Cached rows and completed pages SHALL become browsable progressively without waiting for all endpoints or blocking input. Initial snapshot stages SHALL NOT restart equivalent full-catalog loads, and idle browsing SHALL NOT poll continuously.

#### Scenario: Cold startup browses beyond the first page

- **WHEN** the TUI starts with a cold cache and an allowed Marketplace containing more resources than one discovery page
- **THEN** every discoverable resource MUST become reachable without typing a search or knowing a name
- **AND** local rows and completed pages MUST remain navigable while later pages or another endpoint are delayed
- **AND** arriving rows MUST preserve installed-resource deduplication, scope badges, and the user's selection

#### Scenario: Warm startup and empty endpoints remain usable

- **WHEN** startup has valid cached discovery rows or an endpoint returns an empty catalog
- **THEN** cached rows MUST render before a delayed refresh and an empty endpoint MUST finish without a retry loop
- **AND** initial snapshot stages and idle browsing MUST NOT trigger repeated equivalent full-catalog loads

### Requirement: Startup browse inventory survives search and reload

The complete browse inventory SHALL remain available when a search is applied and cleared. Arriving browse pages SHALL respect the current filter, selection, folds, and detail identity. Results superseded by a newer endpoint configuration or reload SHALL NOT restore removed endpoints or overwrite newer accepted rows.

#### Scenario: Search during loading and clear restore complete browsing

- **WHEN** the user searches while startup pages are still arriving and later clears the query
- **THEN** each arriving page MUST respect the current query without resetting the selection or open detail
- **AND** clearing MUST restore all discovered browse resources with saved browsing folds, rather than only the last query's results or the first page

#### Scenario: Superseded browse results cannot restore removed endpoints

- **WHEN** a reload changes or removes a Marketplace endpoint while its startup pages are delayed
- **THEN** the old pages MUST NOT restore its resources or replace newer accepted state
- **AND** the replacement configuration MUST retain the existing validation and endpoint identity requirements

### Requirement: Startup discovery failures remain isolated and read-only

Startup discovery SHALL retain local rows, successful endpoint results, and usable cached or already received pages when another endpoint or later page fails. It SHALL expose incomplete loading and failures rather than claim completeness. It SHALL preserve endpoint policy and existing verification and installation guards, and SHALL NOT install resources or write authored configuration, acquisition locks, or materialized files.

#### Scenario: Offline partial and denied discovery preserve useful browsing

- **WHEN** startup encounters an offline endpoint, a failed later page, or a denied endpoint
- **THEN** useful local, cached, and successful results MUST remain browsable with visible incomplete or failure feedback
- **AND** denied endpoints MUST NOT be contacted, quitting MUST cancel pending work, and no authored configuration, acquisition lock, or materialized resource MUST change because of browsing
