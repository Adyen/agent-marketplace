## MODIFIED Requirements

### Requirement: Format refusals name a concrete next step

A `v4.api-version-required` or `v4.api-version` refusal MUST include remediation guidance in CLI command error output, on its `hint:` line, and in the TUI diagnostics view, shown once per refusal. For a root in the user-personal, repository-committed, or personal-local layer, or a fragment in such a root's include tree, the guidance MUST name agent-marketplace v4.1.4 as the older released migrator, give the preview command `agent-marketplace migrate --to v4 --scope <user|repo|local> --dry-run` for the owning layer to run with that older binary, link to `https://github.com/Adyen/agent-marketplace/blob/main/site/docs/how-to/migrate-configuration.md`, and state that conversion can refuse include trees or unrepresentable input. For the organization root or a fragment in its include tree, the guidance MUST tell the user to ask their administrator to convert and deploy that configuration, link the same guide, and MUST NOT give a migrate command. The guidance MUST NOT advise merely adding or changing `apiVersion`. Other diagnostics MUST NOT include the version-refusal guidance; they carry their own code-specific guidance.

#### Scenario: Writable-layer version refusals name the older migrator

- **WHEN** a CLI command or the TUI refuses a user-personal, repository-committed, or personal-local root, or a fragment in its include tree, with `v4.api-version-required` or `v4.api-version`
- **THEN** the refusal MUST show guidance naming agent-marketplace v4.1.4, the `migrate --to v4 --scope <layer> --dry-run` command for the owning layer, and the conversion guide link
- **AND** the guidance MUST state that conversion can refuse include trees or unrepresentable input and MUST NOT advise merely changing `apiVersion`

#### Scenario: Organization version refusals direct users to their administrator

- **WHEN** a CLI command or the TUI refuses the organization root, or a fragment in its include tree, with `v4.api-version-required` or `v4.api-version`
- **THEN** the refusal MUST tell the user to ask their administrator to convert and deploy that configuration and link the conversion guide
- **AND** it MUST NOT give a migrate command

#### Scenario: Other diagnostics keep their existing guidance

- **WHEN** a command or the TUI reports a diagnostic other than `v4.api-version-required` or `v4.api-version`, such as `v4.legacy-key` or `v4.parse`
- **THEN** its output MUST NOT include the version-refusal guidance
- **AND** a version refusal shown by a command that already lists remediation in its output MUST show the guidance once
