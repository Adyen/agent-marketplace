## ADDED Requirements

### Requirement: Required hook selections need enforceable activation

Synchronization and checking SHALL refuse required hook selections with severity-error `hook.opencode.unsupported` when the supported native runtime can disable or skip the adapter without blocking protected effects. The refusal SHALL retain previously installed affected hook output and ownership state, and SHALL NOT execute source code or claim that required enforcement succeeded.

#### Scenario: Required hook selections refuse unenforceable activation

- **WHEN** a required hook selection targets OpenCode with unenforceable adapter activation
- **THEN** synchronization and read-only checking SHALL report `hook.opencode.unsupported` and exit nonzero
- **AND** affected installed output SHALL remain unchanged while independent supported resources remain eligible for installation

### Requirement: Multi-resource hook conversion retains all accepted contributions

Conversion to the OpenCode harness target SHALL produce one fixed adapter and every accepted hook declaration without treating the shared adapter as a resource collision. Accepted declarations that exceed the supported aggregate invocation budget SHALL report severity-error `hook.opencode.unsupported` before publication rather than install an unusable generation.

#### Scenario: Two accepted hooks share one adapter

- **WHEN** a source layout contains two independently named supported command hooks
- **THEN** conversion SHALL retain both declarations and exactly one fixed adapter
- **AND** the output SHALL remain ingestible without executing source code

#### Scenario: Excess declarations are rejected before publication

- **WHEN** accepted OpenCode declarations exceed the 64-handler invocation budget
- **THEN** conversion SHALL report `hook.opencode.unsupported` and emit no unusable hook generation
- **AND** 64 supported declarations SHALL remain representable

#### Scenario: Synchronization rejects excess declarations in either mode

- **WHEN** repository or machine synchronization selects more than 64 accepted OpenCode hook declarations in file or symlink mode
- **THEN** synchronization and checking SHALL report severity-error `hook.opencode.unsupported` before publishing the unusable hook generation
- **AND** installed hooks and the adapter SHALL remain unchanged while independent supported resources remain eligible

### Requirement: Successful handlers may decline stdin

An active command handler that exits successfully without consuming all serialized input SHALL retain the same allow/block semantics as a handler that consumes its input. Input pipe closure SHALL NOT alone be interpreted as a refusal; unsuccessful exit and malformed or blocking output SHALL still refuse.

#### Scenario: A successful handler ignores a large valid input

- **WHEN** an active supported handler exits successfully without reading a below-limit input larger than the operating-system pipe buffer
- **THEN** the protected tool effect SHALL be permitted
- **AND** nonzero-exit and blocking-output controls SHALL still prevent the effect
