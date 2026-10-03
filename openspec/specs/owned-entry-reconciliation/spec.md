# owned-entry-reconciliation Specification

## Purpose
Defines how Agent Marketplace reconciles entries in shared harness aggregate
files against durable ownership state, preserving handwritten entries and
failing closed.

## Requirements

### Requirement: Unproven entries are preserved

An existing entry SHALL be treated as unmanaged unless durable ownership
state proves it is an owned entry; unmanaged entries SHALL be left unchanged.

#### Scenario: Handwritten entries remain unmanaged

- **WHEN** an aggregate entry has no ownership-state record
- **THEN** sync SHALL preserve it unchanged

#### Scenario: Missing ownership state preserves everything

- **WHEN** the ownership state is missing or unreadable
- **THEN** every existing entry SHALL be preserved and none SHALL be pruned

### Requirement: Unmanaged collisions refuse only the generated entry

When a desired generated entry shares the identity of an unmanaged existing
entry, sync SHALL preserve the existing entry and refuse only the generated
entry; valid sibling entries SHALL still materialize.

#### Scenario: Unmanaged collision fails only the generated entry

- **WHEN** a desired MCP key already exists in the file as an unmanaged entry
- **THEN** the existing entry SHALL be preserved and only the generated entry
  SHALL fail

### Requirement: Proven owned entries reconcile against recorded state

A proven owned entry SHALL be kept when its value matches the record,
restored when it has drifted, and pruned only when no longer desired; an
unmanaged entry SHALL never be pruned.

#### Scenario: Drifted owned entry is restored

- **WHEN** a proven owned entry's current value differs from the record
- **THEN** sync SHALL replace it with the configured value

#### Scenario: Stale owned entries are pruned

- **WHEN** a proven owned entry is present but no longer desired
- **THEN** sync SHALL remove it and no unproven entry

### Requirement: Repair candidates partition by value equality

An unmanaged existing entry whose identity is desired SHALL be offered for
adoption when its value equals the desired value, and as a replacement
candidate when it differs; the offers SHALL be disjoint.

#### Scenario: Identical values are offered for adoption

- **WHEN** an unmanaged desired entry's value equals the generated value
- **THEN** repair SHALL offer byte-neutral adoption

#### Scenario: Differing values are offered for replacement

- **WHEN** an unmanaged desired entry's value differs from the generated value
- **THEN** repair SHALL offer it for replacement and never for adoption

### Requirement: Gemini shared scopes reconcile together

When the repository root and user home identify the same directory, including filesystem aliases, Agent Marketplace SHALL reconcile the shared Gemini native settings file once against the combined desired entries. It MUST preserve handwritten content and existing permissions, and identical synchronization MUST NOT rewrite the file. An entry SHALL be pruned only when it is no longer desired in either scope. Conflicting values for one MCP identity and operations that cannot isolate the requested scope MUST refuse before materialization.

#### Scenario: Gemini shared settings retain both scopes

- **WHEN** repository and machine selections share one Gemini settings file
- **THEN** MCP servers and hooks from both scopes MUST remain present alongside handwritten entries
- **AND** repeated synchronization and check-only inspection MUST report no drift without rewriting bytes or modification time
- **AND** removing one scope's selections MUST preserve the other scope and handwritten entries

#### Scenario: Gemini shared identity conflict refuses mutation

- **WHEN** two scopes desire different values for the same MCP identity in one Gemini settings file
- **THEN** materialization MUST report `materialize.destination.overlap`
- **AND** existing settings and ownership state MUST remain unchanged

#### Scenario: Gemini shared scope-restricted mutation refuses

- **WHEN** a scope-restricted operation targets a Gemini root shared by repository and machine scopes
- **THEN** it MUST report `materialize.destination.overlap` before writing or pruning
- **AND** the existing settings MUST remain unchanged

### Requirement: Shared Gemini native work does not introduce ordinary pruning

When shared Gemini native settings reconciliation adds machine-scope work without independently compiled or blocked machine resources, Agent Marketplace SHALL reconcile the shared native settings without running an additional ordinary artifact or skill pruning pass. Repository commands MUST remain selected through synchronization and check-only inspection in both materialization modes, including filesystem aliases of HOME. An empty desired set SHALL retain ordinary stale-output cleanup.

#### Scenario: Shared Gemini native work preserves repository commands

- **WHEN** repository commands are selected at a materialization root that identifies HOME, with or without repository native settings fragments
- **THEN** first and repeated synchronization MUST preserve the commands in both materialization modes
- **AND** check-only inspection MUST report no drift and leave their bytes and modification times unchanged
- **AND** stale repository commands MUST still be pruned when an active repository selection no longer desires them

#### Scenario: Empty shared Gemini desired set retains cleanup

- **WHEN** the last desired repository command and native settings entries are removed from a shared Gemini root in symlink materialization mode
- **THEN** synchronization MUST remove the stale command and owned native settings entries
- **AND** it MUST preserve handwritten settings

### Requirement: Repair uses verified acquisition authority

`doctor --repair` SHALL compute Git-backed desired entries from valid acquisition locks and verified exact snapshots. Missing or invalid Git acquisition evidence SHALL cause nonzero exit without reconciling that evidence or changing shared aggregate entries.

#### Scenario: Missing or invalid Git acquisition evidence refuses repair

- **WHEN** repair is requested for a Git-backed resource whose acquisition lock is missing or invalid
- **THEN** repair SHALL exit nonzero and leave the acquisition lock and shared aggregate unchanged

#### Scenario: Moving a Git ref does not change repair values

- **WHEN** a registry ref moves after its acquisition lock was created and a conflicting entry is confirmed for replacement
- **THEN** repair SHALL install the exact snapshot's value and leave the acquisition lock byte-identical

### Requirement: Repair refuses incomplete organization selection

When organization resource acquisition fails, repair SHALL report that failure and refuse mutation rather than interpret unavailable resources as no longer desired.

#### Scenario: Organization acquisition failure refuses repair

- **WHEN** the organization Marketplace cannot resolve its configured resources and no organization acquisition lock is deployed
- **THEN** repair SHALL report the acquisition failure, exit nonzero, and leave shared aggregate entries unchanged

### Requirement: Repair publication uses unchanged policy

Repair SHALL validate the configuration and acquisition-lock inputs authorizing its preview after confirmation and at the aggregate publication boundary. A changed input SHALL cause a generation conflict, preserve the aggregate and ownership state, discard uncommitted staging, and stop repair before prompting another target.

#### Scenario: Configuration changes during confirmation

- **WHEN** configuration changes while repair waits for confirmation
- **THEN** repair SHALL refuse the action with nonzero exit and preserve the aggregate

#### Scenario: Organization acquisition lock appears during confirmation

- **WHEN** an organization acquisition lock is deployed while repair waits for confirmation
- **THEN** repair SHALL refuse the action and preserve both the aggregate and the deployed lock

#### Scenario: Policy changes between confirmation validation and mutation

- **WHEN** policy changes after confirmation was validated but before its repair mutation begins
- **THEN** repair SHALL report a generation conflict without changing the aggregate

#### Scenario: Policy changes after repair staging

- **WHEN** policy changes after adoption, replacement, or orphan removal is staged but before publication
- **THEN** repair SHALL refuse publication, preserve aggregate and ownership bytes, and remove that attempt's staged file, journal, and backup

#### Scenario: Repair stops at the first policy conflict

- **WHEN** policy changes before a confirmed action in a multi-target repair
- **THEN** repair SHALL refuse that action and SHALL NOT prompt for subsequent targets

### Requirement: Repair confirmation does not hold mutation locks

Repair SHALL hold no shared-aggregate ownership lock while waiting for a user's confirmation.

#### Scenario: Ownership writers remain unblocked at confirmation prompts

- **WHEN** repair waits for confirmation before or after a completed action
- **THEN** another writer SHALL be able to acquire the aggregate's ownership lock

### Requirement: Durable aggregate reads retain canonical-root opened-file authority

Agent Marketplace durable aggregate reconciliation, repair, safety-backup, compare-and-swap and recovery live-payload readers MUST consume through captured authority below the ownership store's recorded canonical root. Relative path validation and strict ancestors MUST refuse intermediate/final aliases and traversal before payload reads. Opened files MUST remain regular, singly linked and nonblocking at captured size with state/named-object validation. Genuine absence MUST retain errors.Is and legacy os.IsNotExist; unsafe/generation or cleanup failures MUST NOT become absence. Explicit target-root aliases canonicalized at store admission MUST remain supported. Refusal MUST preserve original/foreign bytes and metadata. Publication/state/journal/archive mutation remains separate; no arbitrary new limits, transaction or exclusion of uncooperative writers is introduced.

#### Scenario: Scoped aggregate reader refuses intermediate alias

- **WHEN** a canonical-root aggregate reader encounters an intermediate alias to an external regular payload after a valid native read control
- **THEN** it MUST refuse with typed unsafe-file identity before returning bytes and preserve foreign payload bytes, identity, mode and mtime
