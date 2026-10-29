# opencode-hooks Specification

## Purpose
Defines safe declarative OpenCode hook support, faithful blocking behavior, additive installation, and native execution evidence required before the first supported release.

## Requirements

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

Native hook adapters SHALL contain reviewed fixed executable behavior and validated declarations treated only as data. Registry metadata SHALL NOT become executable adapter source. The supported source layout SHALL allow non-executing ingestion of validated strict-JSON declarations with a recognized adapter version. Nullable or non-string matchers, comments, and trailing commas SHALL be refused rather than accepted with different native meanings. Arbitrary JS/TS plugins and altered or incompatible adapter representations SHALL NOT be imported or executed; they SHALL be diagnosed explicitly. Source recognition SHALL NOT establish destination ownership.

#### Scenario: Hostile declaration strings remain inert adapter data

- **WHEN** a hook declaration contains quotes, code-like text, control characters, or reference-like strings
- **THEN** native output SHALL retain them as validated declaration data without creating additional executable adapter code
- **AND** ingestion SHALL run no source code

#### Scenario: Adapter recognition does not authorize overwrite

- **WHEN** an existing native plugin matches a generated adapter template but has no destination ownership proof
- **THEN** synchronization SHALL treat it as unmanaged and refuse a same-path overwrite
- **AND** supported generated source declarations SHALL remain ingestible without adopting the destination

### Requirement: Active hooks follow native plugin semantics

Successfully loaded hooks SHALL block protected effects when their handler cannot execute or violates the supported output/failure contract. The compatibility contract SHALL specify activation, malformed output, missing executables, nonzero exits, deadlines, and handler process-group cleanup. The supported runner SHALL use a 60-second default handler deadline, accept explicit positive deadlines no greater than 600 seconds, and limit a tool event to 600 seconds total, 64 handler invocations, 1 MiB serialized input, and 1 MiB combined stdout/stderr per handler. Excess SHALL refuse rather than truncate into apparent success. Absent, disabled, or failed plugins follow OpenCode's native behavior and SHALL be documented as inactive; Agent Marketplace SHALL NOT claim global required-enforcement guarantees or introduce a separate enforcement system. Adapters SHALL NOT grant permissions or rewrite provider/authentication configuration.

#### Scenario: Activation failure cannot become successful enforcement

- **WHEN** hook initialization fails or the runtime disables its external plugin adapter
- **THEN** the compatibility matrix SHALL record the observed native behavior and SHALL NOT advertise the inactive callback as enforced
- **AND** native evidence SHALL include working active allow/block controls and inactive-plugin controls

#### Scenario: Handler failure is bounded and blocks protected effects

- **WHEN** a required blocking handler is missing, times out, emits malformed or excessive output, or exits unsuccessfully under the documented contract
- **THEN** native execution SHALL prevent the protected effect, terminate its handler process group, and produce a bounded non-sensitive failure
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

### Requirement: Hook translation follows the shipped catalogs

OpenCode hook events, tool matchers, deadlines, and command variables SHALL be translated from the hook's source harness through the shipped hook and tool catalogs. A matcher SHALL be emitted only when the OpenCode tools it selects cover exactly the source tool set; a file-modification matcher that does not also cover `apply_patch` SHALL be refused with severity-error `hook.opencode.unsupported`. Declarations SHALL spell each tool as the fixed adapter compares it. Ingested declaration matchers SHALL be empty, `*`, or exact tool names joined by `|` that the adapter does not rewrite; any other matcher SHALL be refused with severity-error `ingest.opencode.invalid-resource`.

#### Scenario: Hooks from other harnesses translate through the catalogs

- **WHEN** a native pre-tool hook from another source layout matches that harness's shell tool and is compiled for OpenCode
- **THEN** the declaration SHALL use event `PreToolUse` and matcher `Bash`

#### Scenario: Partial file-modification matchers are refused

- **WHEN** a hook matching only `Edit` or only `Write` is compiled for OpenCode
- **THEN** compilation SHALL report `hook.opencode.unsupported` and emit no declaration
- **AND** a hook matching `Edit|Write` SHALL emit a declaration matching `Edit`, `Write`, and `apply_patch`

#### Scenario: Ambiguous declaration matchers are refused at ingestion

- **WHEN** an OpenCode declaration matcher is regex-like, comma-separated, or names a native tool ID the adapter rewrites before matching
- **THEN** ingestion SHALL report `ingest.opencode.invalid-resource` and import no hook

### Requirement: Hook contributions and their support are additive and source-bound

Supported hook contributions SHALL retain deterministic ordering, selection identity, and matcher grouping across registries, collections, scopes, and both materialization modes. Removing one selection SHALL retain contributions still selected by another scope or selector. Each selected hook's support files SHALL be the regular files under its source definition's directory, excluding the definition, native hook files of the source layout, and the top-level JSON files of an OpenCode declaration directory. They SHALL be installed with their verified bytes and permission bits under `hook-support/<source>/<hook>/` beneath the OpenCode root, as portable regular copies at repository scope in both materialization modes. Support SHALL never be installed into the declarations or plugin directories, and commands SHALL NOT point into the Agent Marketplace cache or a source directory. Unsafe, unreadable, replaced, or conflicting support input SHALL be refused with severity-error `materialize.hook.support-refused` or `materialize.hook.support-conflict`, preserving previously installed affected declarations, support, the adapter, and their ownership proof, while safely independent valid work proceeds.

#### Scenario: Removing one contribution preserves the others

- **WHEN** multiple sources contribute supported hooks and one selected contribution is removed
- **THEN** only that contribution's declaration and exclusively owned support SHALL be removed
- **AND** remaining contributions SHALL execute in the documented order alongside unrelated native plugins

#### Scenario: Hook support is installed beside the adapter

- **WHEN** a selected hook's command references a script beside its source definition through the plugin root
- **THEN** synchronization SHALL install the script under `hook-support/<source>/<hook>/` and the installed command SHALL run it from any working directory
- **AND** deselecting the hook SHALL remove its declaration and owned support

#### Scenario: Support refusal retains the installed hook generation

- **WHEN** a hook update encounters unsafe, unreadable, or replaced support input
- **THEN** synchronization SHALL report `materialize.hook.support-refused` and retain the installed declaration, support bytes, adapter, and ownership proof

#### Scenario: Sibling declarations are never installed

- **WHEN** a selected OpenCode hook's source directory also contains other hook declarations and support files
- **THEN** synchronization SHALL install only the selected declaration, its support files, and the fixed adapter
- **AND** no sibling declaration SHALL be copied or activated

### Requirement: Handlers receive the project directory

The fixed adapter SHALL set `AGENT_MARKETPLACE_PROJECT_DIR` for every handler. Its value SHALL be the directory containing the adapter's `.opencode` root, or the OpenCode instance directory for an adapter in any other configuration directory. Repository-scope plugin-root references SHALL resolve through it to the installed support directory. Machine-scope plugin-root references, and those of a repository selection relocated outside the repository, SHALL use the absolute installed support path. Project-directory references SHALL translate to it. A plugin-root reference without an installed support directory SHALL be refused with `hook.opencode.unsupported` and `hook.support-path-unrepresentable`.

#### Scenario: Support and project references follow the adapter project directory

- **WHEN** an installed repository-scope hook references its support or the project directory and OpenCode is started in the repository or one of its subdirectories
- **THEN** the handler SHALL reach the installed support and project paths through `AGENT_MARKETPLACE_PROJECT_DIR`
- **AND** a machine-scope hook SHALL reach its support through the absolute installed path

### Requirement: Declaration changes are reviewed

When synchronization adds, replaces, or removes an owned OpenCode hook declaration, it SHALL print the hook review listing with the harness target, scope, declaration file, event, matcher, and exact command before and after, and SHALL emit warning `materialize.hook.review`. `sync --check` SHALL list pending declaration changes, emit `materialize.hook.review-pending`, write nothing, and exit nonzero. Unchanged declarations SHALL produce no listing, and handwritten declaration files SHALL never be listed.

#### Scenario: Declaration changes are listed for review

- **WHEN** a selected OpenCode hook is added, its command changes, or it is deselected
- **THEN** synchronization SHALL list the add, replace, or remove with the exact commands and emit `materialize.hook.review`
- **AND** `sync --check` SHALL list the pending change, emit `materialize.hook.review-pending`, write nothing, and exit nonzero, while an unchanged sync lists nothing

### Requirement: Offline native probe cancellation retains container ownership

Cancelling the offline OpenCode compatibility probe during container creation, including immediate cancellation before startup bookkeeping completes, MUST preserve cleanup ownership until creation completes or reaches its existing deadline. Direct and process-group cancellation MUST exit nonzero, remove only the container created by that invocation, and MUST NOT start the cancelled container. An unrelated container whose name collides MUST NOT be removed.

#### Scenario: Probe cancellation waits for owned container creation

- **WHEN** direct or process-group cancellation reaches the probe while container creation is pending, including immediately after creation starts
- **THEN** the probe MUST retain its nonzero cancellation outcome and clean up the owned container once its ID is available
- **AND** it MUST NOT start the cancelled container or remove a container it did not create
