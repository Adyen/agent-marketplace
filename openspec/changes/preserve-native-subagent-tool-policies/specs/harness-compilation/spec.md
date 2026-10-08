## ADDED Requirements

### Requirement: Native subagent tool policies remain faithful

Harness Compilation MUST preserve an explicit native subagent tool allowlist when the source layout and harness target match and support that policy. Preservation MUST retain exact tool identifiers, including MCP identifiers, and their order without requiring those identifiers to appear in the cross-harness mapping table. An absent tool list MUST remain absent and an explicit empty tool list MUST remain explicitly empty. Native policy preservation MUST NOT bypass other security-restriction validation.

#### Scenario: Same harness native allowlists survive conversion

- **WHEN** a supported source layout declares an explicit native subagent allowlist containing native or exact MCP tool identifiers
- **THEN** same-harness conversion MUST emit the subagent with exactly that ordered allowlist rather than suppressing it, mapping it to different tools, or removing the policy

#### Scenario: Empty and absent native policies remain distinct

- **WHEN** a supported native subagent declares either an explicit empty allowlist or no allowlist
- **THEN** same-harness conversion MUST preserve that distinction in the materialized artifact

#### Scenario: Native policies survive installation and synchronization

- **WHEN** a native subagent with an explicit allowlist is installed and synchronized into the matching harness target in repository or user scope
- **THEN** the generated artifact MUST retain the exact allowlist in either materialization mode and subsequent check-only synchronization MUST report no drift
