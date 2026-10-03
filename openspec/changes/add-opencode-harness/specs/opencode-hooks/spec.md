## Purpose

Defines safe declarative OpenCode hook support, faithful blocking behavior, additive installation, and native execution evidence required before the first supported release.

## ADDED Requirements

### Requirement: The first release includes proven command hooks

OpenCode full harness support SHALL include a documented supported hook subset in the first release. At minimum, command-type `PreToolUse` hooks SHALL run before protected tool effects with faithful matcher, input, allow/block, and failure semantics. Each advertised event, handler form, output effect, and consumer path SHALL have pinned native execution evidence; unsupported forms SHALL report severity-error `hook.opencode.unsupported` and produce no artifact. Whole-kind hook deferral SHALL NOT satisfy release acceptance.

#### Scenario: A blocking pre-tool hook prevents the protected effect

- **WHEN** a supported matching `PreToolUse` command hook returns a blocking decision before a protected tool operation
- **THEN** native execution SHALL prevent the protected side effect and report the hook outcome
- **AND** a matching allow control SHALL permit the intended operation while a nonmatching control SHALL follow its documented policy

#### Scenario: Claimed tool consumer paths share the proven contract

- **WHEN** a supported hook claims coverage of normal tools, MCP tools, or subagent tool execution
- **THEN** each claimed path SHALL demonstrate the supported before/after timing, matcher, input, and output semantics through the pinned native runtime
- **AND** an uncovered path SHALL NOT be advertised as enforced

#### Scenario: Unsupported hook semantics fail visibly

- **WHEN** a selected hook uses an unrepresentable lifecycle event, matcher, handler type, or execution field
- **THEN** conversion or synchronization SHALL report `hook.opencode.unsupported`, exit nonzero, and emit no misleading replacement
- **AND** a blocking lifecycle hook SHALL NOT be replaced by asynchronous notification

### Requirement: Generated hooks separate executable adapters from declarations

Native hook adapters SHALL contain reviewed fixed executable behavior and validated declarations treated only as data. Registry metadata SHALL NOT become executable adapter source. The supported source layout SHALL allow non-executing ingestion of validated declarations with a recognized adapter version. Arbitrary JS/TS plugins and altered or incompatible adapter representations SHALL NOT be imported or executed; they SHALL be diagnosed explicitly. Source recognition SHALL NOT establish destination ownership.

#### Scenario: Hostile declaration strings remain inert adapter data

- **WHEN** a hook declaration contains quotes, code-like text, control characters, or reference-like strings
- **THEN** native output SHALL retain them as validated declaration data without creating additional executable adapter code
- **AND** ingestion SHALL run no source code

#### Scenario: Adapter recognition does not authorize overwrite

- **WHEN** an existing native plugin matches a generated adapter template but has no destination ownership proof
- **THEN** synchronization SHALL treat it as unmanaged and refuse a same-path overwrite
- **AND** supported generated source declarations SHALL remain ingestible without adopting the destination

### Requirement: Hook contributions are additive and source-bound

Supported hook contributions SHALL retain deterministic ordering, selection identity, matcher grouping, and source-bound support files across registries, collections, scopes, and both materialization modes. Removing one selection SHALL retain contributions still selected by another scope or selector. Rejected or unavailable support input SHALL preserve previously installed affected hooks and ownership proof, report `materialize.hook.support-refused`, and permit only safely independent valid work.

#### Scenario: Removing one contribution preserves the others

- **WHEN** multiple sources contribute supported hooks and one selected contribution is removed
- **THEN** only that contribution and its exclusively owned support output SHALL be removed
- **AND** remaining contributions SHALL execute in the documented order alongside unrelated native plugins

#### Scenario: Support refusal retains the installed hook generation

- **WHEN** a hook update encounters unsafe, unreadable, or replaced support input
- **THEN** synchronization SHALL report `materialize.hook.support-refused` and retain the installed affected hook, support bytes, and ownership proof
- **AND** contained valid support aliases and independent valid hook contributions SHALL retain their intended installation behavior

### Requirement: Required hook enforcement fails closed

Hooks advertised as required blocking enforcement SHALL NOT permit protected effects when their adapter is absent or inactive, their handler cannot execute, or their output/failure violates the supported contract. The compatibility contract SHALL specify activation, malformed output, missing executables, nonzero exits, cancellation, and child-process cleanup. The supported runner SHALL use a 60-second default handler deadline, accept explicit positive deadlines no greater than 600 seconds, and limit a tool event to 600 seconds total, 64 handler invocations, 1 MiB serialized input, and 1 MiB combined stdout/stderr per handler. Excess SHALL refuse rather than truncate into apparent success. Runtime configurations that disable external plugins SHALL be identified as incompatible with required enforcement. A runtime that cannot provide the promised behavior SHALL block release; warning-only degradation SHALL NOT satisfy acceptance. Adapters SHALL NOT grant permissions or rewrite provider/authentication configuration to simulate equivalence.

#### Scenario: Activation failure cannot become successful enforcement

- **WHEN** required hook initialization fails or the runtime disables its external plugin adapter
- **THEN** the combination SHALL fail the required-enforcement compatibility gate and SHALL NOT be advertised as enforced
- **AND** release evidence SHALL demonstrate that supported required-enforcement configurations prevent protected effects on activation failure

#### Scenario: Handler failure is bounded and blocks protected effects

- **WHEN** a required blocking handler is missing, times out, is canceled, emits malformed or excessive output, or exits unsuccessfully under the documented contract
- **THEN** native execution SHALL prevent the protected effect, terminate attempt-owned child processes, and produce a bounded non-sensitive failure
- **AND** successful valid handlers SHALL still permit their documented effects

#### Scenario: Native permissions remain authoritative with other plugins

- **WHEN** generated hooks coexist with handwritten plugins and native permission restrictions
- **THEN** hooks SHALL retain their documented order and blocking behavior without granting broader native permissions or changing unrelated plugin, provider, or authentication configuration

#### Scenario: Runner budgets refuse excess without truncation

- **WHEN** a hook declaration exceeds supported timeout/invocation limits or execution exceeds the supported event, input, or output budget
- **THEN** compilation SHALL reject unsupported declarations with `hook.opencode.unsupported` or native execution SHALL block the required protected effect with bounded failure and cleanup
- **AND** below-limit and exact-boundary controls SHALL retain the documented intended behavior

### Requirement: Hook compatibility proof uses isolated native execution

The supported hook compatibility contract SHALL name the verified native version and executable identity used for execution evidence. Native tests SHALL isolate configuration, HOME/XDG/state/cache, environment, dependencies, and endpoints, use synthetic handlers and credentials, impose process bounds, and prevent uncontrolled network access or dependency installation. Interface declarations, generated-file snapshots, or configuration parsing alone SHALL NOT constitute proof of native hook execution.

#### Scenario: The release gate proves hooks in a controlled runtime

- **WHEN** first-release hook compatibility is validated
- **THEN** the gate SHALL execute allow/block controls and every advertised mapping and failure contract using the verified pinned native executable in an isolated runtime
- **AND** no host configuration, host secret, uncontrolled dependency installation, or external endpoint SHALL be used as test input or output
