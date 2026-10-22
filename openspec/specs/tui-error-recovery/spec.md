# tui-error-recovery Specification

## Purpose
Describes the recovery steps shown by the TUI for failed resource-state verification, configuration, source access, acquisition lock, and operation errors while preserving original causes, write guards, and terminal-safe rendering.

## Requirements

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

#### Scenario: Shared-key diagnostics retain their own recovery guidance
- **WHEN** the errors screen displays multiple diagnostics sharing a stable error key across categories or severities
- **THEN** each diagnostic MUST preserve its original cause and show nonempty or typed remediation when one applies
- **AND** otherwise it MUST show category-appropriate next steps using its own category
- **AND** diagnostic ordering, counts, identities, and lifetimes MUST remain unchanged

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

### Requirement: TUI coded errors share CLI guidance

When an error shown on the TUI errors screen has no nonempty remediation of its own but carries a stable diagnostic code with code-specific guidance, the errors screen SHALL show that guidance, the same text a CLI command prints on its `hint:` line, before any typed-cause or diagnostic-category recovery guidance. Errors without code-specific guidance SHALL keep the existing typed-cause and category fallbacks.

#### Scenario: Coded errors show the same guidance as the CLI

- **WHEN** the errors screen displays an error that carries a stable diagnostic code, such as `v4.marketplace-lock-mismatch`, and no remediation of its own
- **THEN** it MUST show the guidance a CLI command prints for that code instead of the generic category guidance

### Requirement: Structured refusals render their own remediation

When an error carries its own cause and remediation, as the update refusal for selections that stop resolving does, the TUI errors screen SHALL show the cause, after any prefix of the operation that wraps it, as the diagnostic line, and SHALL show the error's own remediation as that diagnostic's recovery line instead of the generic recovery copy. Each line SHALL remain sanitized as a single line, so no literal line-break escapes appear and untrusted text still cannot pose as a separate diagnostic or recovery line. The list footer SHALL show the same cause. The error's own text, including the command-line output, SHALL be unchanged.

#### Scenario: Structured refusals render their own remediation

- **WHEN** a TUI update is refused because a selection stops resolving, and the user opens the errors screen
- **THEN** the diagnostic MUST name the missing selection after the `update refresh failed:` prefix, and its recovery line MUST carry the `agent-marketplace remove` remediation and the closing `update left the lock and harness outputs unchanged` line
- **AND** the screen MUST NOT show literal `\x0A` escapes or the generic operation recovery copy
