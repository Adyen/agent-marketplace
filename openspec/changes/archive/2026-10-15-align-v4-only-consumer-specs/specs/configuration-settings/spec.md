## MODIFIED Requirements

### Requirement: Settings do not require remote acquisition

The CLI SHALL read effective logging and telemetry settings from authored configuration whose required roots and include trees satisfy `consumer-config-format`, without fetching remote resources or opening their acquisition locks. Lock independence MUST NOT permit legacy or versionless consumer roots or explicit non-v4 fragment versions.

#### Scenario: Settings with an unreadable acquisition lock

- **WHEN** logging or telemetry settings are read from valid v4 configuration while an unrelated remote acquisition lock is unreadable
- **THEN** the settings MUST resolve from the authored configuration without failing on that lock
