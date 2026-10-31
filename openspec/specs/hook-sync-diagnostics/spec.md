# hook-sync-diagnostics Specification

## Purpose
Describe how resource mutations, `sync`, and `sync --check` report hook changes, materialization warnings, compilation failures, and matcher refusals. Failures identify their harness target, resource, and source; hook reviews state the action and applicable before and after payloads. Hook translation, publication, and ownership are specified elsewhere.

## Requirements

### Requirement: Hook compilation errors retain target and source context

Error diagnostics for hook compilation SHALL be shown by `sync` and `sync --check` with the failing harness target, the hook resource, and the known source file, at default verbosity. Identical repeats for the same target SHALL collapse, while different targets SHALL remain distinguishable. Rendered diagnostics SHALL remain value-free and SHALL NOT quote matcher, command, timeout, or support payloads, except that a matcher decode refusal MAY name a single tool name declared by a shipped harness catalog, as the matcher refusal requirement defines.

#### Scenario: Hook errors retain target and source

- **WHEN** a hook fails compilation during `sync` or `sync --check`
- **THEN** the output MUST identify the failing target, the hook resource, and the known source file, and `sync --check` MUST NOT change any files

#### Scenario: Different target failures remain distinguishable

- **WHEN** the same hook fails compilation for two harness targets
- **THEN** both targets MUST remain visible, and repeated failures for the same target MUST still collapse into one line

### Requirement: Matcher refusal distinguishes decoding from target coverage

A hook whose matcher cannot be translated SHALL keep its existing diagnostic code, severity, and refusal behavior. The diagnostic SHALL state whether the source matcher could not be decoded or whether the target tool catalog could not exactly cover the decoded matcher, and each cause SHALL carry its own static, actionable hint. Neither cause SHALL quote the matcher or any other payload value, except that a decode failure caused by a tool name that a shipped catalog of another harness declares SHALL name that one tool, the harness that declares it, and the source harness. A name that no shipped catalog declares SHALL keep the generic message, and no cause SHALL quote the rest of the matcher or any command value.

#### Scenario: Matcher refusal identifies its translation stage

- **WHEN** the source matcher fails to decode, or the target cannot exactly cover a decoded matcher
- **THEN** the output MUST keep the `hook.matcher-untranslatable` code and show the cause and hint for that stage without the matcher or command values, while same-harness output and valid cross-harness translations MUST remain unchanged

#### Scenario: Matcher refusal names the unknown tool

- **WHEN** a hook's source matcher contains a tool name the source harness does not declare but another shipped harness catalog does, and the hook is translated for another harness
- **THEN** the output MUST keep the `hook.matcher-untranslatable` code and suppression, name that tool and the harness that declares it, and MUST NOT quote the other matcher tokens or the command

#### Scenario: Names outside every catalog are not quoted

- **WHEN** a hook's source matcher contains a tool name that no shipped harness catalog declares
- **THEN** the output MUST show the generic decode refusal and MUST NOT quote that name

### Requirement: Hook drift describes pending operations accurately

Hook review output SHALL label each pending hook change as an addition, a replacement, or a removal. The `sync --check` drift summary SHALL count writes and removals of outputs rather than skills, and a pending removal SHALL explain that deselecting hooks removes their owned entries and owned support files without changing ownership authority. Runs with no pending hook change SHALL print no hook review, and read-only check output SHALL NOT include executable values beyond the reviewed before and after payloads.

#### Scenario: Hook review labels additions replacements and removals

- **WHEN** a selected hook is added, its compiled entry is replaced, or it is deselected
- **THEN** the hook review MUST identify the matching action, and a pending removal MUST explain the owned entry and support cleanup

#### Scenario: Drift summaries count outputs rather than skills

- **WHEN** `sync --check` finds pending output writes, removals, or support cleanup
- **THEN** the summary MUST report writes and removals, return an error that counts outputs rather than missing skills, and keep the error-only and clean behaviors unchanged

### Requirement: Sync checks distinguish load refusal from hook drift

When configuration, registry loading, or resource resolution has a hard error
that prevents ordinary `sync` from materializing, `sync --check` SHALL refuse
before predicting materialized output changes. It SHALL report the actual
refusal diagnostics and SHALL NOT print a pending hook review, a deselection
explanation, or an output drift count derived from the incomplete inventory.
Installed hook files and existing ownership state SHALL retain their bytes,
modes, and modification times. This does not make warning-only inspection fatal
or suppress genuine hook drift after successful loading and resolution.

#### Scenario: Unloadable hook source refuses without pending changes

- **GIVEN** a selected hook is already installed
- **WHEN** its registry cannot load because the source contains ambiguous modeled fields
- **THEN** `sync` and `sync --check` MUST refuse with `json.duplicate-key`, preserve installed files and existing ownership state including bytes, modes, and modification times, and report no pending hook removal, deselection explanation, or output drift count

#### Scenario: Loadable hook sources retain genuine drift reports

- **GIVEN** an installed hook whose source loads successfully
- **WHEN** the hook changes or is deselected
- **THEN** `sync --check` MUST report the genuine replacement or removal and a nonzero drift count without changing installed files or ownership state, and subsequent `sync` MUST apply the reviewed operation

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

### Requirement: Compact JSON hook reviews are terminal safe

Hook review payloads displayed as compact JSON SHALL escape non-printable terminal controls, including C1 and bidi controls. The review SHALL preserve printable JSON syntax, escaped values, and printable Unicode. This SHALL apply to completed and pending reviews, including before and after payloads, without changing materialized hook contents.

#### Scenario: Fallback JSON preserves readable syntax while escaping controls

- **WHEN** a hook review displays a compact JSON handler containing C1 or bidi controls
- **THEN** CLI and TUI reviews MUST show visible escapes instead of literal controls
- **AND** printable JSON quotes, backslashes, and Unicode MUST remain readable
- **AND** materialized hook contents MUST preserve their original values
