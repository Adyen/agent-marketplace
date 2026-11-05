## Why

An explicit selection can outlive its upstream resource or fail catalog
validation. Sync reports the stale selection, but the recommended removal
currently rejects it because the resource is not resolved.

## What Changes

- Allow named removal of explicit missing or malformed selections using their
  configured kind, source identity, and layer.
- Reuse scope, ambiguity, required-resource, and selection-edit rules.
- Infer the configured kind for undiscoverable named `--from` removals.

## Capabilities

### New Capabilities

- `resource-removal`: removal of explicit selections without a discoverable resource.

## Impact

Shared CLI removal pipeline, command regressions, and CLI E2E coverage.
Consumer configuration, acquisition locks, and materialization formats do not change.
