## ADDED Requirements

### Requirement: Printed errors name a next step

Agent Marketplace MUST follow every error printed by a CLI command, and every error listed in `doctor`'s configuration and organization error sections, with at least one line that starts with `hint: ` and names a supported command or a concrete action. When an error carries its own nonempty remediation, that remediation MUST be the hint. Otherwise, when the error carries a stable diagnostic code, the hint MUST be the guidance for that code, naming the affected file, layer scope, or resource when the error identifies them. Otherwise the hint MUST name a diagnostic command suited to the failed command. Every stable diagnostic code that consumer commands can report MUST have code-specific guidance. Guidance for organization-owned configuration, acquisition locks, or trust failures MUST direct the user to their administrator and MUST NOT suggest editing or deleting organization files. Guidance MUST NOT suggest deleting an acquisition lock or running `update` to bypass verification. When several errors are printed together, each error MUST be followed by its own hint, and a hint identical to one already printed for the same command MUST NOT be repeated. Hints MUST use the terminal-safe single-line policy that applies to command errors. Hints MUST NOT appear in machine-readable output and MUST NOT change error identity for `errors.Is` and `errors.As` or exit status. Warning remediation lines MUST keep their existing format.

#### Scenario: Coded errors print code-specific guidance

- **WHEN** a command fails with an error that carries a stable diagnostic code and no remediation of its own, such as `lock.stale` for the repository lock
- **THEN** the `Error:` line MUST be followed by a `hint:` line with that code's guidance, naming the affected lock scope and the command that reconciles it

#### Scenario: A resource missing from the lock names its file and the command that records it

- **WHEN** a repository configuration selects a hosted Marketplace resource that has no record in an existing repository acquisition lock that already records its endpoint, and `doctor` or a read-only command reports `v4.marketplace-lock-mismatch`
- **THEN** the error MUST name the configuration file and line that select the resource and the acquisition lock it is missing from
- **AND** the hint MUST tell the user to run a writing `agent-marketplace sync` while online, without `--check`, `--locked`, or `--offline`, and commit the updated lock
- **AND** after the user runs that command, the same check MUST no longer report the error

#### Scenario: A Marketplace endpoint missing from the lock names the command that records it

- **WHEN** a repository configuration selects resources from a hosted Marketplace endpoint that the repository acquisition lock does not record, or the lock is missing, and `doctor` or a read-only command reports `v4.marketplace-lock-mismatch` or `v4.marketplace-lock-required`
- **THEN** the hint MUST tell the user to run a writing `agent-marketplace sync` while online, without `--check`, `--locked`, or `--offline`, and commit the updated lock

#### Scenario: Uncoded errors print a diagnostic next step

- **WHEN** a command fails with an error that carries neither remediation nor a stable diagnostic code
- **THEN** the `Error:` line MUST be followed by a `hint:` line naming a diagnostic command suited to that command, such as `agent-marketplace doctor` or rerunning with `--debug`

#### Scenario: Usage errors point to command help

- **WHEN** a command is invoked with an unknown flag, a missing argument, or too many arguments
- **THEN** the `hint:` line MUST name `--help` for that command

#### Scenario: Joined errors print each distinct hint once

- **WHEN** a command fails with several joined errors, two of which share the same guidance
- **THEN** each error MUST stay on its own line followed by its hint
- **AND** the shared guidance MUST be printed only once

#### Scenario: Organization failures direct users to their administrator

- **WHEN** a command or `doctor` reports an error about the organization configuration, organization acquisition lock, or organization trust
- **THEN** the hint MUST tell the user to ask their administrator
- **AND** it MUST NOT suggest editing, deleting, or bypassing organization files

#### Scenario: Doctor configuration errors carry hints

- **WHEN** `doctor` lists errors in its configuration or organization error sections
- **THEN** each listed error MUST be followed by its `hint:` line

#### Scenario: Hints leave machine-readable output and identity unchanged

- **WHEN** a command that supports JSON output fails, or a caller inspects the returned error
- **THEN** the JSON output MUST contain no hint text
- **AND** the exit status and `errors.Is` and `errors.As` identity MUST match the error without its hint

#### Scenario: Hints cannot control the terminal

- **WHEN** guidance includes a configuration value carrying terminal controls or a line break
- **THEN** the printed hint MUST show those controls as visible escapes on the hint line

#### Scenario: Every consumer diagnostic code has guidance

- **WHEN** a stable diagnostic code can be reported by a consumer command
- **THEN** that code MUST have code-specific guidance that names a supported command or a concrete action

### Requirement: Configuration validation errors name their file and line

A consumer configuration validation error MUST name the authored configuration file and, when known, the line, in the form `<file>:<line>`, and MUST describe the problem in plain words that do not require knowledge of internal terms. Its stable diagnostic code MUST stay unchanged at the start of the message.

#### Scenario: Validation errors name the authored file

- **WHEN** a repository configuration file fails consumer validation at a known line
- **THEN** the printed error MUST start with its unchanged diagnostic code and name that file and line as `<file>:<line>`
