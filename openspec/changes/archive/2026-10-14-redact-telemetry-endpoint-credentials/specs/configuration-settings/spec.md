## ADDED Requirements

### Requirement: Telemetry URL credential redaction

Operational log attributes and returned errors that identify a telemetry endpoint, retention-policy URL, or queued-delivery URL MUST omit URL userinfo, query, and fragment values while retaining the scheme, host, and path. Wrapped errors MUST keep their error identity. Validation, persisted configuration, and delivery MUST continue to use the configured value.

#### Scenario: Telemetry endpoint credentials are not logged

- **WHEN** a telemetry endpoint or retention-policy URL containing userinfo and a query token is validated, used to build route clients, applied from the environment, accepted by `telemetry enable`, or replayed from the delivery queue
- **THEN** no resulting log attribute or returned error MUST contain the userinfo or query token
- **AND** the logged endpoint MUST still identify its host and path
