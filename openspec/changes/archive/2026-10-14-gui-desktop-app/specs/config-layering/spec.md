## ADDED Requirements

### Requirement: Organization-only GUI release check setting

A top-level `gui` mapping with boolean `version_check` SHALL affect GUI release availability checks only when it is declared in Consumer organization configuration. If the organization value is absent, the default SHALL enable release checking. A `gui.version_check` value in user-personal, repository-committed, or personal-local configuration SHALL be ignored, SHALL produce a warning diagnostic, and SHALL NOT cause that configuration to be rejected.

#### Scenario: Organization setting controls release availability checks

- **WHEN** Consumer organization configuration sets `gui.version_check` to true or false
- **THEN** the effective GUI release-check setting SHALL match that boolean

#### Scenario: Release availability checks default to enabled

- **WHEN** Consumer organization configuration does not set `gui.version_check`
- **THEN** the effective GUI release-check setting SHALL be enabled

#### Scenario: Lower-layer GUI setting is ignored with a warning

- **WHEN** user-personal, repository-committed, or personal-local configuration declares `gui.version_check`
- **THEN** that value SHALL be ignored
- **AND** a warning diagnostic SHALL be reported
- **AND** the configuration SHALL NOT be rejected

### Requirement: Existing unsupported top-level Harness targets remain inspectable

Configuration readers SHALL retain non-empty string identifiers already present in a root's top-level Harness target set when the running client does not support them, and SHALL report a warning identifying the unsupported setting. Unsupported identifiers SHALL NOT become materialization targets or basic telemetry Harness target attributes. Other malformed configuration and unsupported per-entry Harness target narrowing SHALL retain their existing validation behavior. Initialization and explicit Harness target edits SHALL enforce the supported-target and existing-identifier rules in `session-interactions`.

#### Scenario: Existing unsupported identifier is retained without materialization authority

- **WHEN** a root's top-level Harness target set contains a supported target and an identifier unsupported by the running client
- **THEN** the reader SHALL retain both identifiers and report an unsupported-target warning
- **AND** resolution and materialization SHALL use only supported targets
- **AND** basic telemetry SHALL contain only supported Harness target attributes

#### Scenario: Unsupported-only target set grants no materialization authority

- **WHEN** a root's top-level Harness target set contains no supported target
- **THEN** reading the configuration SHALL retain its identifiers with unsupported-target warnings
- **AND** a target-dependent operation SHALL fail for the missing supported target set without materializing Resource files
