## ADDED Requirements

### Requirement: Web healthcheck requests are bounded

The supplied web image's exec-form healthcheck SHALL configure Node fetch with
a four-second request deadline, before Docker's five-second process timeout.
Failed and aborted requests SHALL exit nonzero without requiring a shell.

#### Scenario: Web healthcheck has an explicit request deadline

- **WHEN** the supplied web image's healthcheck is configured
- **THEN** its native Node fetch MUST use a four-second AbortSignal deadline
- **AND** failed or aborted requests MUST produce a nonzero exit
