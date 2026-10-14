## MODIFIED Requirements

### Requirement: Telemetry URL credential redaction

Operational log attributes and returned errors that identify a telemetry endpoint, retention-policy URL, or queued-delivery URL MUST omit URL userinfo, query, and fragment values as whole components, including any whitespace they contain, while retaining the scheme, host, and path of a parseable URL. A value that cannot be parsed as a URL MUST be replaced by a fixed marker. Wrapped errors MUST keep their error identity. Validation, persisted configuration, and delivery MUST continue to use the configured value.

#### Scenario: Telemetry endpoint credentials are not logged

- **WHEN** a telemetry endpoint or retention-policy URL containing userinfo and a query token is validated, used to build route clients, applied from the environment, accepted by `telemetry enable`, or replayed from the delivery queue
- **THEN** no resulting log attribute or returned error MUST contain the userinfo or query token
- **AND** the logged endpoint MUST still identify its host and path

#### Scenario: Unparseable telemetry URLs fail closed

- **WHEN** a telemetry URL that cannot be parsed, or whose query value contains whitespace, reaches a validation error, log attribute, or queued-delivery error
- **THEN** no part of its userinfo or query value MUST appear in the resulting text
- **AND** an unparseable value MUST appear only as the fixed redaction marker
