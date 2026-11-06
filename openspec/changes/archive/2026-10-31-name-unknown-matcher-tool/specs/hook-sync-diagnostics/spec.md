## MODIFIED Requirements

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
