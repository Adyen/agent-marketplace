## ADDED Requirements

### Requirement: TUI coded errors share CLI guidance

When an error shown on the TUI errors screen has no nonempty remediation of its own but carries a stable diagnostic code with code-specific guidance, the errors screen SHALL show that guidance, the same text a CLI command prints on its `hint:` line, before any typed-cause or diagnostic-category recovery guidance. Errors without code-specific guidance SHALL keep the existing typed-cause and category fallbacks.

#### Scenario: Coded errors show the same guidance as the CLI

- **WHEN** the errors screen displays an error that carries a stable diagnostic code, such as `v4.marketplace-lock-mismatch`, and no remediation of its own
- **THEN** it MUST show the guidance a CLI command prints for that code instead of the generic category guidance
