# harness-compilation Specification

## Purpose
Defines faithful native serialization of canonical agent resources, keeping scalar metadata separate from explicit typed execution authority.

## Requirements

### Requirement: Unicode YAML separators remain scalar data

Harness Compilation MUST preserve U+0085, U+2028 and U+2029 in YAML frontmatter scalar strings without allowing them to create extra native metadata keys or execution authority. Native strict YAML parsing MUST recover the exact scalar input. Ordinary scalar formatting and explicit typed native sidecars MUST retain their existing behavior. This requirement adds no conversion policy or processing budget.

#### Scenario: Unicode line separators cannot create workflow authority

- **WHEN** a command description contains a YAML Unicode separator followed by inert workflow-metadata text
- **THEN** public Windsurf compilation MUST produce frontmatter whose strict YAML parse retains the exact description without adding an auto_execution_mode field

#### Scenario: Explicit typed workflow authority remains native

- **WHEN** a command's typed Windsurf sidecar declares manual execution with an ordinary description
- **THEN** public compilation MUST retain the ordinary description and explicit manual execution metadata in valid native YAML

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
