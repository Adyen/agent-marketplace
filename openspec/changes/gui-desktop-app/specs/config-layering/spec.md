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
