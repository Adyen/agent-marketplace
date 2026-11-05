## MODIFIED Requirements

### Requirement: No in-process consumer migration surface

The shipped CLI MUST NOT expose `migrate`, its migration target flags, startup conversion prompts, or a TUI migration modal. Consumer operations MUST NOT offer an automatic conversion or fall back to legacy reads/edits. Refusal guidance MAY direct users to convert writable configurations with an older released migrator before upgrading and to have consumer organization configuration converted externally; the shipped CLI MUST NOT invoke or acquire a converter.

#### Scenario: Migration command and flags are absent

- **WHEN** a user inspects command help/completion or invokes `migrate`, `migrate --to v2`, or `migrate --to v4`
- **THEN** help/completion MUST expose no consumer migration command or flags and invocation MUST fail as an unknown command without conversion or configuration changes

#### Scenario: Consumers do not offer in-process conversion

- **WHEN** unsupported consumer configuration is encountered through interactive CLI startup, non-interactive operation, bare-terminal TUI startup, or explicit TUI startup
- **THEN** the consumer MUST show the path-specific format refusal without a conversion prompt, migration modal, or legacy-success fallback
- **AND** configuration-dependent actions MUST remain blocked without new acquisition, edits, or pruning, subject only to established pending-transaction recovery
