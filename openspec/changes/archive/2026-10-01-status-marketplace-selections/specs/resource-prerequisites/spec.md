## MODIFIED Requirements

### Requirement: Satisfaction by kind and name

A prerequisite SHALL be met when any selected resource in the same resolution has the same kind and name, regardless of the registry, path, configuration layer, or Marketplace selection that supplies it. The `registry` and `path` hints SHALL NOT restrict satisfaction.

#### Scenario: Prerequisite supplied by another registry

- **WHEN** a skill requires an MCP server with a `registry` hint and a different registry supplies a selected MCP server with that name
- **THEN** the marketplace MUST NOT report the prerequisite as unmet

#### Scenario: Same name with another kind

- **WHEN** a skill requires MCP server `docs` and only a skill named `docs` is selected
- **THEN** the marketplace MUST report the MCP prerequisite as unmet

#### Scenario: Doctor treats a Marketplace selection as a satisfied prerequisite

- **WHEN** a selected resource declares a prerequisite that only a Marketplace-selected resource satisfies and `doctor` runs
- **THEN** `doctor` MUST NOT report the prerequisite as unmet
