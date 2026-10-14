## Why

Telemetry endpoint and retention-policy URLs are written raw to operational logs and embedded in validation and delivery errors. A URL carrying userinfo or a query token therefore persists those credentials in the log file and in error text, contrary to the rule that credentials are never logged.

## What Changes

- Remove URL userinfo, query, and fragment values from telemetry endpoint, retention-policy, and queued-delivery URLs in operational log attributes and returned errors, using the existing shared URL redaction policy.
- Redact wrapped URL-parser and HTTP-client errors without changing their error identity.
- Fix the shared URL redaction policy so a credentialed URL with an explicit port keeps its scheme and has its query redacted.
- Keep the configured value unchanged for validation, persistence, and delivery.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `configuration-settings`: telemetry URL credentials are redacted from logs and errors.

## Impact

Telemetry configuration validation, route-client construction, environment overrides, `telemetry enable`, and queued-delivery replay. Every existing shared-redaction consumer gains the explicit-port fix. Command display output and persisted configuration are unchanged.
