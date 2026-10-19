## Why

Telemetry URL redaction treated whitespace as the end of a URL, so a query value containing a space left its remainder visible, and URL-parser or request-construction errors quoted the raw configured URL. Unparseable values have no reliable component boundaries, so redaction must fail closed rather than guess.

## What Changes

- Redact telemetry URL values structurally: userinfo, query, and fragment are replaced as whole components, even when they contain whitespace.
- Replace a telemetry URL value that cannot be parsed with a fixed marker.
- Apply the same structured redaction to the URL embedded in wrapped URL-parser and HTTP-client errors, including queued-delivery request construction.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `configuration-settings`: telemetry URL redaction is structural and fails closed for unparseable values.

## Impact

Telemetry validation errors, route-client and environment-override logs, `telemetry enable`, and queued-delivery replay errors. Free-text redaction for other consumers is unchanged.
