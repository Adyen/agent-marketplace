## Why

A v4 directory source whose `skills/` directory contains Git metadata reports `skills/.git` as a malformed skill in the CLI and TUI. That metadata is not a resource, and the false error obscures valid skills.

## What Changes

- Exclude Git metadata directories from skill candidates when ingesting a source layout, including directory sources, while retaining diagnostics for malformed authored skills.
- Preserve explicitly selected source roots and existing path safety checks.

## Capabilities

### New Capabilities

- `directory-source-discovery`: Defines how directory sources distinguish Git metadata from authored skills during discovery.

### Modified Capabilities

None.

## Impact

Claude source-layout ingestion and catalog discovery through the CLI and TUI. No configuration schema, selection, or materialization changes.
