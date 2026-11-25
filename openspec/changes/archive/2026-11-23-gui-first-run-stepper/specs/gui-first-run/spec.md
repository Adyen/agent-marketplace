## ADDED Requirements

### Requirement: First-run stepper

First-run setup SHALL show its steps as a numbered list of Targets, Telemetry, and Sources, with the current step marked as the current step. The steps SHALL NOT be separated by middle dots or other separator characters.

#### Scenario: Stepper shows numbered steps without separators

- **WHEN** first-run setup opens on the targets step
- **THEN** the step indicator MUST list "1 Targets", "2 Telemetry", and "3 Sources" as separate items
- **AND** the current step MUST be marked as the current step
- **AND** the indicator MUST NOT contain a middle dot
