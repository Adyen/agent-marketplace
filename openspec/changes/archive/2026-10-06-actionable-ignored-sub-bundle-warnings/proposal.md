## Why

The ignored-sub-bundle warning does not identify which resources were skipped or which root resource prevents recursive discovery. Consumers need registry-relative examples and a concrete way to select an ignored bundle.

## What Changes

- Identify an example root resource and the ignored bundles with their affected resource kinds and names.
- Include the registry URL, ref, and entry path, with bounded, sorted examples and omitted-item counts.
- Explain how to restructure the root or target an ignored bundle directly.
- Preserve captured source authority during diagnostic discovery and isolate the remote-registry regression test's cache.

## Capabilities

### New Capabilities

- `catalog-discovery`: actionable, bounded ignored-sub-bundle diagnostics.

### Modified Capabilities

None.

## Impact

Catalog diagnostics change for CLI and TUI consumers. Selection and materialization behavior remain unchanged. Existing discovery source boundaries remain enforced.
