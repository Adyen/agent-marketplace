# configuration-settings Specification

## Purpose
Keeps logging and telemetry configuration available from validated layered settings independently of remote resource acquisition and its locks.

## Requirements

### Requirement: Settings do not require remote acquisition

The CLI SHALL read effective logging and telemetry settings from authored configuration without fetching remote resources or opening their acquisition locks.

#### Scenario: Settings with an unreadable acquisition lock

- **WHEN** logging or telemetry settings are read while an unrelated remote acquisition lock is unreadable
- **THEN** the settings MUST resolve from the authored configuration without failing on that lock

### Requirement: Operational log attribute encoding

Untrusted string and error attributes recorded by Marketplace request handling, managed operations, and telemetry endpoint validation and delivery MUST encode carriage returns and line feeds before reaching a logging handler. Ordinary values MUST remain unchanged, and encoding MUST not change the operation's error identity or control flow.

#### Scenario: Untrusted log values cannot create records

- **WHEN** telemetry endpoint validation logs a resolver error containing carriage returns and line feeds
- **THEN** the error attribute MUST contain escaped line breaks instead of raw record separators
- **AND** the existing best-effort endpoint validation behavior MUST remain unchanged

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
