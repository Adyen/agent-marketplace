## ADDED Requirements

### Requirement: Documentation publishes an LLM-readable index

Each deployment profile that publishes documentation SHALL publish `llms.txt` at its profile root: the public site at its site base, and the documentation-only artifact at `/docs/`. The index SHALL list every documentation page in navigation order. Each entry SHALL link to a Markdown copy of that page that is served beside the page's HTML route and below the profile's base. A Markdown copy SHALL contain the page's source without its front matter.

#### Scenario: LLM index lists every documentation page

- **WHEN** a deployment profile is built
- **THEN** its `llms.txt` MUST list every documentation page exactly once, in the sidebar's navigation order
- **AND** each link MUST resolve to a generated Markdown file below the profile's base

#### Scenario: Markdown copies mirror documentation sources

- **WHEN** a documentation page is published as a Markdown copy
- **THEN** the copy MUST equal the page source with its leading front matter removed
- **AND** images referenced by the copy MUST resolve to generated files

#### Scenario: Both profiles publish the same Markdown copies

- **WHEN** both deployment profiles are built from the same source revision
- **THEN** they MUST publish the same Markdown copy inventory with identical contents
- **AND** their `llms.txt` MUST be identical apart from the profile base prefix
