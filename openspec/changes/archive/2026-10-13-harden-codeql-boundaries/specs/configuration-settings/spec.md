## ADDED Requirements

### Requirement: Operational log attribute encoding

Untrusted string and error attributes recorded by Marketplace request handling, managed operations, and telemetry endpoint validation and delivery MUST encode carriage returns and line feeds before reaching a logging handler. Ordinary values MUST remain unchanged, and encoding MUST not change the operation's error identity or control flow.

#### Scenario: Untrusted log values cannot create records

- **WHEN** telemetry endpoint validation logs a resolver error containing carriage returns and line feeds
- **THEN** the error attribute MUST contain escaped line breaks instead of raw record separators
- **AND** the existing best-effort endpoint validation behavior MUST remain unchanged
