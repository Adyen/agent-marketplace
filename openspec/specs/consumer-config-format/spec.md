# consumer-config-format Specification

## Purpose
Defines the consumer configuration format the shipped CLI accepts: every consumer root declares exact `agent-marketplace/v4`, included fragments inherit it, unsupported roots and fragments refuse with path-specific diagnostics before any new work, pending dependency transactions keep their recovery semantics, and no in-process consumer migration surface exists.

## Requirements

### Requirement: Consumer roots declare exact v4

Every existing consumer organization, user-personal, repository-committed, and personal-local configuration root MUST declare the string scalar `apiVersion: agent-marketplace/v4`. An omitted root field MUST be refused with `v4.api-version-required`; an explicit v1/v2/v3, unknown, empty, null, non-string, or otherwise non-exact value MUST be refused with `v4.api-version`. Diagnostics MUST identify the offending configuration file and the `apiVersion` field, with line context when available. Unsupported roots MUST NOT be decoded as another schema, ignored as absent, or accepted by an editor. Existing absent-file and dangling-layer behavior remains governed by `config-mutation`; an existing versionless file is not an absent layer. YAML syntax and ambiguity refusals remain applicable.

#### Scenario: v4 roots are accepted in every consumer layer

- **WHEN** valid organization, user-personal, repository-committed, or personal-local roots declare exact v4 and use fields permitted in their layers
- **THEN** consumer reads and supported edits MUST retain their selections, policy, and layer provenance
- **AND** valid control workflows MUST retain existing source/resource scope and ordered selection outcomes

#### Scenario: Versionless roots are refused in every consumer layer

- **WHEN** an operation interprets an existing root in any consumer layer whose `apiVersion` field is omitted, including a root whose included fragment declares v4
- **THEN** it MUST refuse the root with `v4.api-version-required` identifying that root and `apiVersion`
- **AND** it MUST NOT infer the root version from its fields, location, another layer, or a fragment

#### Scenario: Explicit non-v4 root versions are refused

- **WHEN** a root in any consumer layer declares v1, v2, v3, an unknown version, an empty/null/non-string value, or a whitespace/case lookalike of v4
- **THEN** the operation MUST refuse with `v4.api-version` identifying that root and field before interpreting legacy installation fields
- **AND** the root MUST NOT be accepted by a direct configuration editor or an alternate consumer read

### Requirement: Included fragments inherit only v4

An explicitly included fragment under a validated v4 root MAY omit `apiVersion` or declare exact v4. An explicit non-v4 or invalid value MUST refuse the include tree with `v4.api-version` identifying the fragment and field. Fragment acceptance MUST preserve include authority, relative source paths, ordered selection decisions, source binding and layer provenance. Edits MUST remain owned by the selected layer root and acquisition locks by that logical root; fragment acceptance MUST NOT relax layer-specific field restrictions.

#### Scenario: v4 fragments preserve include authority

- **WHEN** a valid v4 root includes fragments that omit the version or explicitly declare v4 and declare selections absent from the containing root
- **THEN** those fragment selections and relative-directory sources MUST remain effective under existing include precedence
- **AND** a supported selection edit MUST update only the owning root, preserve fragment bytes, and keep its acquisition lock in the logical layer

#### Scenario: Explicit non-v4 fragments refuse the include tree

- **WHEN** a v4 root includes a direct or nested fragment declaring v1/v2/v3, an unknown version, or an invalid explicit version value alongside otherwise valid selections
- **THEN** the operation MUST refuse with `v4.api-version` naming that fragment and `apiVersion`
- **AND** valid siblings MUST NOT permit new acquisition, configuration/lock publication, or harness writes/prunes for that operation

### Requirement: Format refusal precedes new consumer work

Operations that interpret consumer configuration MUST validate all required roots and include trees before remote acquisition or any new configuration/acquisition-lock staging or publication, harness writes, or harness pruning. Format refusal MUST produce a non-zero CLI outcome or a visible TUI error that gates configuration-dependent actions. Authored/ad-hoc reads, direct edits, and initialization of existing roots MUST NOT bypass this rule. Existing config/lock/harness bytes and metadata MUST remain unchanged by the refused new operation, and absent writable roots/locks MUST NOT be created. Ordinary operational logging is permitted. Pre-existing dependency transaction handling is governed by the pending-recovery requirement, not a blanket no-write promise.

#### Scenario: Configuration reads refuse without acquisition

- **WHEN** a configuration-dependent read, including ad-hoc browsing, encounters an unsupported root or fragment while a valid source would otherwise require remote Git or Marketplace acquisition
- **THEN** it MUST report the path-specific format diagnostic rather than use the remaining layers
- **AND** it MUST make no acquisition request and leave configuration, acquisition locks, and harness output unchanged

#### Scenario: Mutations refuse before new writes or prunes

- **WHEN** a selection, harness, initialization, lock, synchronization, update, or prune operation encounters unsupported consumer input and existing config/locks/owned output would otherwise change
- **THEN** it MUST refuse before new staging, publication, acquisition, or pruning
- **AND** existing config/lock/harness bytes and metadata MUST remain unchanged, with no attempt-owned staging residue

#### Scenario: Refused layers prevent missing root creation

- **WHEN** an edit targets an absent writable root but another required consumer root or fragment has an unsupported version
- **THEN** the edit MUST return the offending file's format diagnostic
- **AND** it MUST NOT create the target root or acquisition lock, fetch the requested resource, or materialize output

### Requirement: Pending recovery retains admitted transaction semantics

Format validation MUST NOT disable established safe recovery of a pre-existing dependency transaction. Commands in read-only dependency modes MUST refuse pending work with `dependency.recovery-required` without replay. Recovery-capable commands MUST retain established safe replay before loading fresh configuration; completing the already-admitted journaled write set is permitted even when exact-v4 validation subsequently refuses the recovered input. They MUST apply exact-v4 validation before starting new work. Blocked recovery MUST retain its fail-closed outcome and recovery evidence. Recovery MUST NOT authorize fresh legacy interpretation, new conversion, or unrelated writes.

#### Scenario: Read-only commands preserve pending transactions

- **WHEN** a read-only dependency mode encounters pending journaled work and an unsupported consumer root
- **THEN** it MUST return `dependency.recovery-required` without replay or new acquisition
- **AND** journal, config, lock, and harness bytes MUST remain unchanged

#### Scenario: Recovery finishes admitted work before format refusal

- **WHEN** a recovery-capable command encounters a valid interrupted transaction and the recovered configuration still contains an unsupported root or fragment
- **THEN** established recovery MUST finish the recorded write set and finalize its evidence before configuration validation
- **AND** the command MUST then report the path-specific format refusal without starting new acquisition, config/lock publication, or harness writes/prunes

#### Scenario: Blocked recovery preserves evidence

- **WHEN** pending dependency recovery is malformed or conflicts with foreign or replaced destination state
- **THEN** it MUST retain the established recovery refusal and evidence without applying unrelated changes
- **AND** unsupported configuration MUST NOT become an excuse to overwrite conflicting state, discard the journal, or begin a new operation

### Requirement: No in-process consumer migration surface

The shipped CLI MUST NOT expose `migrate`, its migration target flags, startup conversion prompts, or a TUI migration modal. Consumer operations MUST NOT offer an automatic conversion or fall back to legacy reads/edits. Refusal guidance MAY direct users to convert writable configurations with an older released migrator before upgrading and to have consumer organization configuration converted externally; the shipped CLI MUST NOT invoke or acquire a converter.

#### Scenario: Migration command and flags are absent

- **WHEN** a user inspects command help/completion or invokes `migrate`, `migrate --to v2`, or `migrate --to v4`
- **THEN** help/completion MUST expose no consumer migration command or flags and invocation MUST fail as an unknown command without conversion or configuration changes

#### Scenario: Consumers do not offer in-process conversion

- **WHEN** unsupported consumer configuration is encountered through interactive CLI startup, non-interactive operation, bare-terminal TUI startup, or explicit TUI startup
- **THEN** the consumer MUST show the path-specific format refusal without a conversion prompt, migration modal, or legacy-success fallback
- **AND** configuration-dependent actions MUST remain blocked without new acquisition, edits, or pruning, subject only to established pending-transaction recovery

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

### Requirement: Consumer configuration requires supported harness target IDs

The running CLI MUST understand every harness target ID named in consumer configuration, including harness selections and per-target materialization settings. Unknown IDs MUST remain configuration errors that gate configuration-dependent actions, not warnings or permission to use only the supported subset. Refusal MUST NOT silently remove or rewrite unknown entries. This validation does not require the corresponding harness application to be installed. Ordinary operational logging and recovery of a pre-existing dependency transaction retain their existing contracts.

#### Scenario: Unknown harness targets gate configuration-dependent actions

- **WHEN** otherwise valid consumer configuration names an unknown harness target ID alongside supported IDs in harness selections or per-target materialization settings
- **THEN** CLI synchronization MUST refuse with a non-zero outcome and the TUI MUST visibly refuse harness editing and synchronization instead of using only the supported subset
- **AND** the refused new operation MUST preserve authored configuration, acquisition locks and existing installed resources without installing pending resources
- **AND** after the user explicitly repairs the configuration and reloads, supported harness editing and synchronization MUST work

### Requirement: Include trees stay within the include limit

Loading one consumer root MUST refuse when it would read more than 256 config files. The count includes the root and every include occurrence on every branch, so a fragment reached through several branches counts once per branch. Refusal MUST report `v4.include-limit` before any acquisition, lock, or harness work. Include precedence, ordering, multiplicity and branch-local cycle detection MUST be unchanged for trees within the limit. The limit has no override.

#### Scenario: Include trees stay within the include limit

- **WHEN** a v4 root's includes form a shared diamond that expands to more than 256 files, or a flat list of 256 includes
- **THEN** the load MUST fail with `v4.include-limit`, and a root with 255 includes MUST still load
