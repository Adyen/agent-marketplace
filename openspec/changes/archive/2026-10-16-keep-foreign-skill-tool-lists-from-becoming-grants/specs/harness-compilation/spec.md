## ADDED Requirements

### Requirement: Foreign skill tool lists never become pre-approvals

Claude, Cursor and Gemini read a skill's `allowed-tools` as a pre-approval: listed tools run without a permission prompt. When a skill's `allowed-tools` list carries another harness's tool provenance, Claude, Cursor and Gemini compilation MUST omit the list and MUST report the warning `skill.allowed-tools-omitted`, and the skill MUST still be emitted. A list without provenance (canonical Claude vocabulary) or from the target's own harness MUST be emitted unchanged.

#### Scenario: Devin and Factory skill tool lists are omitted on pre-approval targets

- **WHEN** a Devin skill with `allowed-tools` `read` and `exec`, and a Factory skill with `allowed-tools` `Read` and `Edit`, are converted to Claude, Cursor and Gemini
- **THEN** each emitted skill MUST have no `allowed-tools` field, and compilation MUST report the warning `skill.allowed-tools-omitted`

#### Scenario: Same-harness and canonical skill tool lists are unchanged

- **WHEN** a Claude skill with `allowed-tools` `Read` is converted to Claude, and a Gemini skill with `allowed-tools` `read_file` is converted to Gemini
- **THEN** each emitted skill MUST keep its `allowed-tools` list unchanged, with no `skill.allowed-tools-omitted` warning
