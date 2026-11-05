## Purpose

Describes the recovery steps shown by the TUI for failed resource-state verification, configuration, source access, acquisition lock, and operation errors while preserving original causes, write guards, and terminal-safe rendering.

## ADDED Requirements

### Requirement: TUI errors provide safe recovery steps

The TUI SHALL describe failed resource-state verification in plain language and provide a concrete next step. Its errors screen SHALL preserve each original cause and show existing nonempty remediation before using typed-cause or diagnostic-category recovery guidance. Guidance SHALL use supported commands, preserve acquisition locks and ownership state, and distinguish administrator-owned organization files from user-writable files. The TUI SHALL sanitize and wrap all recovery text through its existing diagnostic renderer. Recovery guidance SHALL NOT change operation results, error identities, write guards, or diagnostic lifetimes.

#### Scenario: Failed verification provides a diagnostic command
- **WHEN** a post-operation snapshot cannot verify resource state and supplies no specific error
- **THEN** the operation MUST report failure in plain language and direct the user to run `agent-marketplace sync --check` in the workspace, fix the issue, and reload with `r` from the resource list
- **AND** it MUST NOT claim that the operation made no changes or completed successfully

#### Scenario: Errors preserve specific remediation and add safe fallbacks
- **WHEN** the errors screen displays an error or warning
- **THEN** it MUST preserve the original cause and use its nonempty remediation if present
- **AND** otherwise it MUST show typed-cause or category-appropriate next steps with supported commands
- **AND** unclassified errors MUST NOT be presented as configuration errors

#### Scenario: Acquisition lock guidance respects scope and verification
- **WHEN** a typed error reports a missing or stale writable acquisition lock
- **THEN** the errors screen MUST provide a scope-specific `agent-marketplace lock` command without `--upgrade`
- **AND** organization lock or trust failures MUST direct users to their administrator
- **AND** invalid or unreadable locks MUST NOT receive instructions to delete locks or run `update` to bypass verification

#### Scenario: Interrupted writes receive the supported recovery command
- **WHEN** an error including a Marketplace discovery error carries the interrupted-transaction recovery identity
- **THEN** the errors screen MUST direct the user to run plain `agent-marketplace sync`, without `--check`, `--locked`, or `--offline`, before reloading

#### Scenario: Recovery hints cannot control the terminal
- **WHEN** remediation contains terminal controls or line-breaking data
- **THEN** the errors screen MUST sanitize and wrap it using the existing diagnostic rendering policy while keeping safe recovery text readable
