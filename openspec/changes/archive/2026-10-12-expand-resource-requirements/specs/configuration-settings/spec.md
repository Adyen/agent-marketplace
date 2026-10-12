## MODIFIED Requirements

### Requirement: Settings do not require remote acquisition

The CLI SHALL read effective logging, telemetry, and resource-requirement expansion settings from authored configuration without fetching remote resources or opening their acquisition locks.

#### Scenario: Settings with an unreadable acquisition lock

- **WHEN** logging, telemetry, or `requires.expand` settings are read while an unrelated remote acquisition lock is unreadable
- **THEN** the settings MUST resolve from the authored configuration without failing on that lock

## ADDED Requirements

### Requirement: Resource-requirement expansion is a layered v4 setting

A v4 configuration MAY declare `requires.expand` as a boolean. Agent Marketplace SHALL merge this setting across organization, user-personal, repository-committed, and personal-local layers using existing settings precedence. Recursive includes SHALL contribute below their containing configuration, and an absent value SHALL NOT override a value from a lower-precedence source. An explicit `false` SHALL remain distinguishable from an absent value.

#### Scenario: Expansion setting follows layered and include precedence

- **WHEN** configuration layers or recursively included fragments declare different `requires.expand` values
- **THEN** the effective setting MUST follow existing settings precedence, with an included fragment below its containing file
- **AND** an explicit `false` MUST override a lower-precedence `true`

#### Scenario: Expansion is disabled when no layer sets it

- **WHEN** no applicable configuration layer or include declares `requires.expand`
- **THEN** resource-requirement expansion MUST default to `false`

#### Scenario: Invalid expansion values are rejected

- **WHEN** a v4 configuration declares `requires.expand` with a non-boolean value
- **THEN** strict configuration validation MUST reject the invalid setting

### Requirement: Legacy configuration retains the release default without migration writes

A v2 or v3 configuration SHALL use the release default of `false` for resource-requirement expansion. Migration of a legacy configuration MUST NOT seed `requires.expand` into the resulting configuration.

#### Scenario: Legacy configuration uses the release default

- **WHEN** a v2 or v3 configuration is read without a v4 `requires.expand` setting
- **THEN** resource-requirement expansion MUST remain disabled

#### Scenario: Migration does not add the expansion setting

- **WHEN** a v2 or v3 configuration is migrated to v4 and the source did not explicitly define `requires.expand`
- **THEN** the migrated configuration MUST omit `requires.expand`
