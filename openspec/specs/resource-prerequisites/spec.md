# resource-prerequisites Specification

## Purpose

Define how catalog resources declare functional prerequisites and how commands
report prerequisites that are not selected without changing configuration.

## Requirements
### Requirement: Resources declare prerequisites

The marketplace SHALL read prerequisites from `metadata.requires` on cataloged
resources.

#### Scenario: Structured MCP prerequisite

- **WHEN** a resource declares an item with `kind: mcp` and a non-empty name
- **THEN** the catalog MUST retain its kind, name, and optional provider hints

#### Scenario: Shorthand prerequisite

- **WHEN** a resource declares `"mcp:server-name"`
- **THEN** it MUST be equivalent to a structured MCP prerequisite without
  provider hints

### Requirement: Prerequisites do not change selection

The marketplace SHALL treat prerequisite declarations as advisory and SHALL NOT
change resource selection because of them.

#### Scenario: Install finds an unmet prerequisite

- **WHEN** an installed resource declares a prerequisite that is not selected
- **THEN** install MUST emit a remediable warning
- **AND** it MUST NOT select the prerequisite

#### Scenario: Prerequisite is explicitly excluded

- **WHEN** a selected resource requires an explicitly excluded resource that is not `required: true`
- **THEN** the marketplace MUST report the unmet prerequisite
- **AND** it MUST preserve the explicit exclude

#### Scenario: Excluded prerequisite is required

- **WHEN** a selected resource requires an excluded resource provided by a `required: true` entry
- **THEN** existing `required: true` precedence MUST keep the prerequisite selected
- **AND** the marketplace MUST NOT report it as unmet

### Requirement: Unmet prerequisites are visible

The marketplace SHALL report selected resources whose prerequisite kind and
name are not selected.

#### Scenario: Sync reports without mutation

- **WHEN** sync finds an unmet prerequisite
- **THEN** it MUST emit a remediable warning
- **AND** it MUST NOT enable the prerequisite

#### Scenario: Status and doctor diagnostics

- **WHEN** status or doctor finds an unmet prerequisite
- **THEN** it MUST identify both resources and an installation command
