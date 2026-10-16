## Why

Reported installation counts disappear in production because the historical inventory response exceeds the metrics reader's 8 MiB limit. Legitimate rule observations also invalidate adoption results even though rules are accepted telemetry and are not Marketplace-selectable resources.

## What Changes

- Select the latest observation-valued inventory generations in the metrics backend before returning resource observations; retain tied generations so ambiguous installations remain excluded.
- Exclude legitimate rule observations from adoption without invalidating counts for Marketplace-selectable resources.
- Preserve the 30-day freshness definition, deduplication, registry origin matching, HTTP limits, caching, and discovery fallback.
- Add query-semantic and consumer-path regression coverage.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-api`: current reported installation counts remain available when superseded inventories exceed the response budget but the latest-inventory projection fits; valid rule observations do not invalidate selectable resource counts.

## Impact

Changes are limited to installation metrics querying, adoption validation, their tests, and the Marketplace API spec. No public API field, operator configuration, telemetry emission, dependency, frontend, or production deployment change is required.
