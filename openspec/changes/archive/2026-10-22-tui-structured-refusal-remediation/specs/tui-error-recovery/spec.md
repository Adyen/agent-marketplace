## ADDED Requirements

### Requirement: Structured refusals render their own remediation

When an error carries its own cause and remediation, as the update refusal for selections that stop resolving does, the TUI errors screen SHALL show the cause, after any prefix of the operation that wraps it, as the diagnostic line, and SHALL show the error's own remediation as that diagnostic's recovery line instead of the generic recovery copy. Each line SHALL remain sanitized as a single line, so no literal line-break escapes appear and untrusted text still cannot pose as a separate diagnostic or recovery line. The list footer SHALL show the same cause. The error's own text, including the command-line output, SHALL be unchanged.

#### Scenario: Structured refusals render their own remediation

- **WHEN** a TUI update is refused because a selection stops resolving, and the user opens the errors screen
- **THEN** the diagnostic MUST name the missing selection after the `update refresh failed:` prefix, and its recovery line MUST carry the `agent-marketplace remove` remediation and the closing `update left the lock and harness outputs unchanged` line
- **AND** the screen MUST NOT show literal `\x0A` escapes or the generic operation recovery copy
