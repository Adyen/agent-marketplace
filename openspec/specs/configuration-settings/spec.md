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
