## Why

Some resources are unusable without another marketplace resource, such as a
skill that reads Confluence through an MCP server. Collections can bundle an
install but cannot express or diagnose that functional dependency.

## What Changes

- Add `metadata.requires` to cataloged resources.
- Report unmet prerequisites during install, sync, status, and doctor.
- Keep prerequisite metadata advisory: it never changes resource selection or
  removes an explicit exclude.

## Capabilities

### Added Capabilities

- `resource-prerequisites`: Defines prerequisite metadata and diagnostics.

## Impact

- Affected code: catalog ingestion, resolver, install reporting, sync, status,
  doctor.
- Affected docs: precedence and command behavior.
- No external dependencies.
