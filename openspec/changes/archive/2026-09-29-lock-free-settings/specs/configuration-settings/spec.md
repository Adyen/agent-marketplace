## Purpose

Keeps logging and telemetry configuration available from validated layered settings independently of remote resource acquisition and its locks.

## ADDED Requirements

### Requirement: Settings do not require remote acquisition

The CLI SHALL read effective logging and telemetry settings from authored configuration without fetching remote resources or opening their acquisition locks.

#### Scenario: Settings with an unreadable acquisition lock

- **WHEN** logging or telemetry settings are read while an unrelated remote acquisition lock is unreadable
- **THEN** the settings MUST resolve from the authored configuration without failing on that lock
