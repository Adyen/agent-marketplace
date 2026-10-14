## MODIFIED Requirements

### Requirement: The release check can be suppressed

The CLI SHALL neither contact GitHub nor write a notice when `AGENT_MARKETPLACE_NO_UPDATE_CHECK` is set to a non-empty value, when `CI` is set to a non-empty value, when the effective `update_check.enabled` configuration setting is `false`, or when the invoked command is `serve`, a shell completion request, or `--version`. `update_check` SHALL be accepted only in v4 organization and user-personal configuration; when both set `enabled`, the user-personal value SHALL take precedence, and when neither sets it the check SHALL be enabled. Setting `enabled: true` SHALL NOT override the environment, `CI`, or command suppression.

#### Scenario: Opt-out variable suppresses the check

- **WHEN** `AGENT_MARKETPLACE_NO_UPDATE_CHECK` is set to a non-empty value
- **THEN** the CLI SHALL NOT contact GitHub and SHALL NOT write a notice

#### Scenario: Continuous integration suppresses the check

- **WHEN** `CI` is set to a non-empty value
- **THEN** the CLI SHALL NOT contact GitHub and SHALL NOT write a notice

#### Scenario: Excluded commands suppress the check

- **WHEN** the invoked command is `serve`, a shell completion request, or `--version`
- **THEN** the CLI SHALL NOT contact GitHub and SHALL NOT write a notice

#### Scenario: User configuration disables the check

- **WHEN** the user-personal configuration sets `update_check.enabled` to `false`
- **THEN** the CLI SHALL NOT contact GitHub and SHALL NOT write a notice

#### Scenario: Organization configuration disables the check

- **WHEN** the organization configuration sets `update_check.enabled` to `false` and the user-personal configuration does not set it
- **THEN** the release check SHALL be disabled

#### Scenario: User configuration overrides the organization setting

- **WHEN** the organization configuration sets `update_check.enabled` to `false` and the user-personal configuration sets it to `true`
- **THEN** the release check SHALL be enabled

#### Scenario: Repository configuration cannot set the release check

- **WHEN** a repository or personal-local configuration contains `update_check`
- **THEN** loading that layer SHALL fail with `v4.layer-field`
