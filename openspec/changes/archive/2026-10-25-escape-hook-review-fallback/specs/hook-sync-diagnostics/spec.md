## ADDED Requirements

### Requirement: Compact JSON hook reviews are terminal safe

Hook review payloads displayed as compact JSON SHALL escape non-printable terminal controls, including C1 and bidi controls. The review SHALL preserve printable JSON syntax, escaped values, and printable Unicode. This SHALL apply to completed and pending reviews, including before and after payloads, without changing materialized hook contents.

#### Scenario: Fallback JSON preserves readable syntax while escaping controls

- **WHEN** a hook review displays a compact JSON handler containing C1 or bidi controls
- **THEN** CLI and TUI reviews MUST show visible escapes instead of literal controls
- **AND** printable JSON quotes, backslashes, and Unicode MUST remain readable
- **AND** materialized hook contents MUST preserve their original values
