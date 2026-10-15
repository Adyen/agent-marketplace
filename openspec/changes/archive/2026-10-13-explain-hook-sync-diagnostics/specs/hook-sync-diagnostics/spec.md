## ADDED Requirements

### Requirement: Hook compilation errors retain target and source context

Error diagnostics for hook compilation SHALL be shown by `sync` and `sync --check` with the failing harness target, the hook resource, and the known source file, at default verbosity. Identical repeats for the same target SHALL collapse, while different targets SHALL remain distinguishable. Rendered diagnostics SHALL remain value-free and SHALL NOT quote matcher, command, timeout, or support payloads.

#### Scenario: Hook errors retain target and source

- **WHEN** a hook fails compilation during `sync` or `sync --check`
- **THEN** the output MUST identify the failing target, the hook resource, and the known source file, and `sync --check` MUST NOT change any files

#### Scenario: Different target failures remain distinguishable

- **WHEN** the same hook fails compilation for two harness targets
- **THEN** both targets MUST remain visible, and repeated failures for the same target MUST still collapse into one line

### Requirement: Matcher refusal distinguishes decoding from target coverage

A hook whose matcher cannot be translated SHALL keep its existing diagnostic code, severity, and refusal behavior. The diagnostic SHALL state whether the source matcher could not be decoded or whether the target tool catalog could not exactly cover the decoded matcher, and each cause SHALL carry its own static, actionable hint. Neither cause SHALL quote the matcher or any other payload value.

#### Scenario: Matcher refusal identifies its translation stage

- **WHEN** the source matcher fails to decode, or the target cannot exactly cover a decoded matcher
- **THEN** the output MUST keep the `hook.matcher-untranslatable` code and show the cause and hint for that stage without the matcher or command values, while same-harness output and valid cross-harness translations MUST remain unchanged

### Requirement: Hook drift describes pending operations accurately

Hook review output SHALL label each pending hook change as an addition, a replacement, or a removal. The `sync --check` drift summary SHALL count writes and removals of outputs rather than skills, and a pending removal SHALL explain that deselecting hooks removes their owned entries and owned support files without changing ownership authority. Runs with no pending hook change SHALL print no hook review, and read-only check output SHALL NOT include executable values beyond the reviewed before and after payloads.

#### Scenario: Hook review labels additions replacements and removals

- **WHEN** a selected hook is added, its compiled entry is replaced, or it is deselected
- **THEN** the hook review MUST identify the matching action, and a pending removal MUST explain the owned entry and support cleanup

#### Scenario: Drift summaries count outputs rather than skills

- **WHEN** `sync --check` finds pending output writes, removals, or support cleanup
- **THEN** the summary MUST report writes and removals, return an error that counts outputs rather than missing skills, and keep the error-only and clean behaviors unchanged
