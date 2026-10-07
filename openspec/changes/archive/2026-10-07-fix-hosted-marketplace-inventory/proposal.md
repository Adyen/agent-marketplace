## Why

A resource acquired through a Marketplace endpoint can cause the CLI to reject its entire telemetry inventory as `invalid_inventory`, because its endpoint is interpreted as a Git origin. Installed resources then disappear from reported adoption despite detailed telemetry being enabled.

## What Changes

- Represent resources acquired through a Marketplace by explicit Marketplace instance ID, registry identity and canonical resource name, without interpreting their endpoint as a Git origin.
- Emit bounded additive source identity attributes on inventory registry and resource observations; retain legacy Git and directory observations.
- Count hosted observations only for the serving Marketplace instance, deduplicating Git and hosted reports for the same Installation ID and resource coordinate.
- Preserve complete-snapshot rejection for genuinely invalid data, freshness, ambiguity handling, limits and unavailable fallback.
- Prepare matching collector validation and attribute retention in the separate k8s-multitenant repository; deployment is not part of implementation.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-api`: hosted resource adoption and compatible complete inventory reporting.

## Impact

The shared CLI/TUI inventory builder, telemetry inventory wire attributes, the installation-metrics query and reader, and the hosted collector schema change. No new dependencies, public search fields, configuration or lockfile formats, acquisition behavior, or materialization ownership changes are required. Deploy collector and API read support before the new CLI.
