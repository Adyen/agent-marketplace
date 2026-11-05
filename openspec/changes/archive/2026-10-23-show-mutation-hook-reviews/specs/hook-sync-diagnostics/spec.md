## ADDED Requirements

### Requirement: Resource mutations report completed hook changes and materialization warnings

The CLI commands `install`, `remove`, and `update` SHALL show the same completed hook review listing and warning diagnostics as `sync` when their materialization adds, replaces, or removes owned hook entries. This SHALL apply to every supported harness target and selecting scope, including ad-hoc, configured, Marketplace, and collection selections. The listing SHALL identify the harness target, scope, file, event, matcher, action, and exact before and after command or prompt. Repository-scope Codex changes SHALL keep `materialize.codex.trust-review`; other hook review diagnostics SHALL keep their existing codes and warning severity. Materialization warnings unrelated to hook changes SHALL also remain visible. Reporting SHALL NOT change acquisition, selection, ownership, configuration formats, or command success criteria. A completed partial materialization SHALL remain reviewable when another resource fails; a refusal before materialization SHALL NOT invent completed hook changes. Hook payloads SHALL appear only in the review listing, never logs or telemetry, and terminal control characters SHALL be escaped.

#### Scenario: Mutation commands list completed hook changes

- **WHEN** install adds an owned hook, update replaces its command, and remove deselects it
- **THEN** each command MUST list its completed operation with the exact applicable command values and the existing review warning
- **AND** repeating an unchanged install or update MUST produce no hook review

#### Scenario: Mutation commands retain materialization warnings

- **WHEN** install, remove, or update materializes a resource with a warning about a lossy harness translation
- **THEN** it MUST show the stable warning code and remediation without turning the warning into a command failure

#### Scenario: Marketplace and collection installs retain hook review

- **WHEN** a Marketplace resource or collection install adds an owned hook
- **THEN** install MUST show the completed hook listing and review diagnostic

#### Scenario: Partial mutation failures retain completed review

- **WHEN** a mutation publishes a valid hook while an independent resource fails materialization
- **THEN** it MUST show the published hook and its review warning while retaining the failure exit status
- **AND** a refusal before materialization MUST produce no completed review

### Requirement: TUI mutation reviews survive authoritative reload

The TUI SHALL show completed hook review rows and materialization warnings for install, removal through selection changes followed by sync, sync, and update. When an operation changes owned hook entries, the scrollable diagnostics view SHALL open with the completed review after the authoritative reload, and users SHALL be able to close and reopen it. An authoritative reload SHALL NOT erase the just-completed review or its warning. A later completed materialization SHALL replace the prior hook review, including clearing it when no hooks changed. A refusal before materialization SHALL preserve the previous completed review and its warning; accompanying warnings SHALL remain visible alongside failures even when no materialization report is available. Sync-and-quit SHALL present a newly completed hook review or materialization warning before exiting. The listing SHALL obey the same terminal escaping, ownership, and payload disclosure rules as CLI review output.

#### Scenario: TUI presents and retains completed hook review

- **WHEN** a user syncs newly selected hooks, updates their commands, removes them through a selection change and sync, or installs them through Marketplace discovery
- **THEN** the completed review and warning MUST be visible after reload and remain available after closing and reopening diagnostics
- **AND** an unchanged subsequent materialization MUST clear the completed hook review

#### Scenario: Sync and quit presents new hook review

- **WHEN** a user confirms sync-and-quit and that sync changes an owned hook or emits a materialization warning
- **THEN** the TUI MUST present the completed hook review or warning and allow the user to close it before quitting
