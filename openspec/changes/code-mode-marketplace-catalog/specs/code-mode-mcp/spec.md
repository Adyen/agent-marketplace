## ADDED Requirements

### Requirement: Complete Marketplace catalog snapshots

Code mode SHALL refresh the catalog from every configured Marketplace endpoint's `/api/v1/catalog`, following cursor pages to completion. It SHALL publish only complete, identity-consistent results for each endpoint; a failed scan SHALL mark the snapshot stale rather than silently omit the endpoint. Cached complete catalogs SHALL be available on later sessions without waiting for a network request.

#### Scenario: Marketplace resources beyond the first page are searchable

- **WHEN** a Marketplace catalog includes a skill and an MCP server after its first page and refresh completes
- **THEN** code-mode search lists both resources using their Marketplace refs, and the MCP server advertises `tools: "unknown"`

#### Scenario: Cold catalog reports stale and later session reuses refresh

- **WHEN** a configured Marketplace has no cached catalog and code mode starts
- **THEN** its initial search reports `catalog.stale`; once the background refresh completes, subsequent search includes its resources, and a later session can read the complete saved catalog before refresh

#### Scenario: Incomplete or inconsistent catalog remains stale

- **WHEN** a Marketplace scan fails, exceeds a bounded scan limit, changes catalog version between pages, returns an invalid cursor, or disagrees with the configured Marketplace instance ID
- **THEN** no partial scan is published as complete, and search reports `catalog.stale` for that endpoint

### Requirement: Marketplace catalog execution

Code mode SHALL expose available Marketplace skill and MCP server refs from completed scans and SHALL retain its verified artifact and assessment gate before using them. Unavailable, revoked, or rejected entries SHALL NOT become executable through a catalog refresh.

#### Scenario: Marketplace skill and declared server execute through the gate

- **WHEN** a script loads an available Marketplace skill and calls a tool on an available declared Marketplace MCP server
- **THEN** the skill content and verified server definition are used, and the session records Marketplace provenance for both

#### Scenario: Unavailable or rejected Marketplace resource is not executable

- **WHEN** a catalog entry is unavailable, revoked, or rejected by its cached assessment
- **THEN** it is not offered as an executable catalog ref, and execution cannot bypass the existing resource gate

### Requirement: JSON-shaped script tool results

Declared MCP server bindings SHALL return JSON-shaped tool metadata and results to JavaScript, using the fields advertised by code mode rather than Go struct field names or encoded schema bytes.

#### Scenario: listTools returns documented JSON fields

- **WHEN** a script calls `listTools()` on a declared MCP server
- **THEN** each tool has lowercase `name`, `description`, and `inputSchema` fields, with `inputSchema` as a JSON object

#### Scenario: Tool calls preserve JSON field names

- **WHEN** a declared MCP tool returns a structured result
- **THEN** the script receives its JSON field names rather than Go struct field names
