# gui-first-run Specification

## Purpose
Defines guided initialization of a missing user-personal configuration for people beginning in the desktop GUI, including Harness target selection, telemetry consent, and an optional source step.

## Requirements

### Requirement: First-run entry condition

The GUI SHALL show first-run setup when the user-personal configuration root is absent.

#### Scenario: Absent user-personal root starts first run

- **WHEN** no user-personal configuration root exists
- **THEN** the GUI SHALL show first-run setup

### Requirement: Harness target selection

First-run setup SHALL preselect detected Harness targets and let the user change them. Completion SHALL require at least one Harness target.

#### Scenario: Detected Harness targets are preselected and editable

- **WHEN** first-run setup detects installed Harness targets
- **THEN** those targets SHALL be preselected and the user SHALL be able to change the selection
- **AND** completion with no selected Harness target SHALL be refused

### Requirement: Community telemetry consent follows init

First-run setup SHALL show a Community telemetry consent step exactly when the interactive path of `agent-marketplace init` would ask for consent for a newly created user-personal root. When Consumer organization configuration sets an explicit Telemetry level, the GUI SHALL skip that step. Otherwise, the step SHALL present no preselected answer and SHALL require an explicit choice before completion; an affirmative choice SHALL enable basic telemetry, and a negative choice SHALL decline consent.

#### Scenario: Organization telemetry level skips consent

- **WHEN** Consumer organization configuration explicitly sets a Telemetry level
- **THEN** first-run setup SHALL skip the Community telemetry consent step

#### Scenario: Unmanaged first run presents init-equivalent consent

- **WHEN** Consumer organization configuration does not set a Telemetry level and first-run setup will create the user-personal root
- **THEN** the GUI SHALL present the same Community telemetry consent choice as interactive `agent-marketplace init`
- **AND** no answer SHALL be preselected and completion SHALL require an explicit choice
- **AND** an affirmative choice SHALL enable basic telemetry while a negative choice SHALL decline it

### Requirement: Initialization output matches init

Completing first-run setup SHALL create the v4 user-personal root through the same initialization path as `agent-marketplace init`. Identical Harness targets and an identical Community telemetry consent answer SHALL produce byte-identical configuration to `agent-marketplace init`; source entries are excluded from this comparison.

#### Scenario: Harness targets and consent match init output

- **WHEN** `agent-marketplace init` and GUI first run create the user-personal root with identical Harness targets and the same Community telemetry consent answer
- **THEN** their resulting configuration bytes SHALL be identical before optional source entries are added

### Requirement: Optional source step

After the Harness target and consent steps, first-run setup SHALL offer an optional source step. The source step SHALL be skipped when Consumer organization configuration already provides sources, and any source edit SHALL use the same mutation path as the `gui-configuration` source form. Source drafts SHALL remain in memory until the user presses Finish setup. After successful Harness and consent initialization, a failed optional source addition SHALL NOT roll back the initialized root or prior successful source additions.

#### Scenario: Organization sources skip the source step

- **WHEN** Consumer organization configuration already provides sources
- **THEN** first-run setup SHALL skip the source step

#### Scenario: Optional source step uses the configuration form

- **WHEN** the source step is available and the user adds or removes a source
- **THEN** the change SHALL use the same user-personal mutation path as the `gui-configuration` source form
- **AND** the user SHALL be able to complete setup without adding a source

#### Scenario: Optional source failure keeps initialized configuration

- **WHEN** Harness and consent initialization succeeds and an optional source addition fails
- **THEN** the GUI SHALL NOT roll back the initialized user-personal root or prior successful source additions
- **AND** the GUI SHALL report the source failure and allow retry through source settings
- **AND** the failed addition SHALL NOT be shown as a registered source

### Requirement: Cancelling first run writes nothing

Cancelling first-run setup before completion SHALL leave the user-personal configuration and other files unchanged.

#### Scenario: Cancel first run before completion

- **WHEN** a user cancels first-run setup before completing all shown steps
- **THEN** no configuration or generated Resource files SHALL be written
