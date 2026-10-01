## Why

Code-mode online access can keep an obsolete archive digest from cached resource metadata after the Marketplace republishes the archive for the same resource revision. Canonical support files are also omitted because the loader reads only artifact assets.

## What Changes

- Refresh metadata for the exact resource revision before online skill or MCP definition access; persist validated metadata for offline reuse without falling back to stale metadata after an online failure.
- Return canonical support-file paths and bytes alongside legacy asset files, with deterministic bounded lists, existing path and byte limits, and refusal of ambiguous paths.
- Preserve offline verified-cache access, digest verification, assessment and revocation refusal, the two-tool API, and lazy MCP discovery.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `code-mode-mcp`: exact online Marketplace metadata freshness, canonical support-file access, offline reuse, and failure behavior.

## Impact

Changes are limited to the code-mode Marketplace content loader, its regression tests, and directly affected code-mode documentation and specifications. No hosted-service, configuration, installation, promotion, dependency, or MCP tool-discovery changes.
