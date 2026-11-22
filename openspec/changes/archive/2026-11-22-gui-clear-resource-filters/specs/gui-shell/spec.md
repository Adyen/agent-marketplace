## ADDED Requirements

### Requirement: Clear all resource filters

While any source, kind, or status filter is active, the filter bar above the resource list SHALL offer a Clear filters control in addition to each filter's own clear control. Activating it SHALL remove every active filter at once and return to the unfiltered resource list.

#### Scenario: Clear filters removes every active filter

- **WHEN** the user has a kind filter and a status filter active and activates Clear filters
- **THEN** both filters MUST be removed, the filter bar MUST disappear, and the resource list MUST show all resources
