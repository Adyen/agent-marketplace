# hook-resources Specification

## Purpose
Describe how Agent Marketplace ingests hook resources from source layouts,
selects them, compiles them for each harness target, and reconciles them into
shared harness configuration files, including owned entries and diagnostics.

## Requirements

### Requirement: Canonical hook files flatten into one hook per handler

Agent Marketplace SHALL read a canonical `hooks.json` in either the workspace
form, which has top-level event keys, or the wrapped form, which nests the
events under a top-level `hooks` object. Both forms SHALL produce one hook
resource per handler. The resource is named `<event>-<type>`, and a repeated
event and type gets the next free numeric suffix.

#### Scenario: Workspace and wrapped forms flatten identically

- **WHEN** a source `hooks.json` declares the same matcher groups in the
  workspace form and in the wrapped form
- **THEN** both MUST yield the same hook resources, one per handler, each
  carrying its event, matcher, type, command or prompt, and timeout

#### Scenario: Repeated event and type receive distinct names

- **WHEN** a source file declares several handlers with the same event and
  type
- **THEN** the first MUST be named `<event>-<type>` and later ones
  `<event>-<type>-2`, `<event>-<type>-3`, and so on
- **AND** no suffixed name MUST collide with the unsuffixed name of a hook of
  another type

#### Scenario: Duplicate JSON keys reject the hook file

- **WHEN** a source `hooks.json` contains the same object key twice at any
  depth, outside comments
- **THEN** ingestion MUST reject the file with `json.duplicate-key` instead of
  keeping the last value

#### Scenario: Non-command handler type loads with a warning

- **WHEN** a canonical handler declares a type other than `command`
- **THEN** the hook MUST still be ingested
- **AND** ingestion MUST emit the warning `hook.claude.unsupported-type`

### Requirement: Native hook files are reversed into canonical events

Each harness target's source layout SHALL ingest its native hook file and map
native event names back to canonical events through a closed per-target
reverse table. A native event outside that table SHALL keep its original name
and produce a diagnostic, or be skipped where the target documents the
handler as unsupported.

#### Scenario: Windsurf native file keeps every handler

- **WHEN** a Windsurf `hooks.json` declares several handlers for one event
- **THEN** ingestion MUST produce one hook per handler with distinct names, in
  source order

#### Scenario: Gemini BeforeAgent maps to UserPromptSubmit

- **WHEN** a Gemini `settings.json` declares a `BeforeAgent` hook
- **THEN** ingestion MUST produce a hook whose canonical event is
  `UserPromptSubmit`

#### Scenario: Undocumented Devin event keeps its name with a diagnostic

- **WHEN** a Devin hook file declares an event outside Devin's documented
  vocabulary
- **THEN** ingestion MUST keep the original event name and emit
  `ingest.devin.hook-event-unmappable`
- **AND** a Devin `PostCompaction` hook MUST NOT become a canonical
  `PreCompact` hook

#### Scenario: Unsupported Codex handlers are skipped

- **WHEN** a Codex `hooks.json` declares an undocumented event or a
  non-command handler
- **THEN** ingestion MUST skip that handler with `ingest.codex.hook-unsupported`
- **AND** valid sibling handlers MUST still be ingested

#### Scenario: Cursor plugin-root token is normalized

- **WHEN** a Cursor hook command references `${CURSOR_PLUGIN_ROOT}`
- **THEN** ingestion MUST rewrite it to `${CLAUDE_PLUGIN_ROOT}`

### Requirement: Hooks materialize into a fixed file per harness target and scope

Agent Marketplace SHALL merge each compiled hook into the following shared
file:

| Harness target | Repository scope | Machine scope |
|---|---|---|
| Claude Code | `.claude/hooks/hooks.json` | `~/.claude/hooks/hooks.json` |
| Factory Droid | `.factory/hooks/hooks.json` | `~/.factory/hooks/hooks.json` |
| Cursor | `.cursor/hooks/hooks.json` | `~/.cursor/hooks/hooks.json` |
| Gemini CLI | `.gemini/settings.json` under `hooks` | `~/.gemini/settings.json` under `hooks` |
| Windsurf | `.windsurf/hooks.json` under `hooks` | `~/.codeium/windsurf/hooks.json` under `hooks` |
| Devin CLI | `.devin/hooks.v1.json` | `~/.config/devin/config.json` under `hooks` |
| Codex | `.codex/hooks.json` under `hooks` | `$CODEX_HOME/hooks.json` (default `~/.codex`) under `hooks` |

For Claude Code, Factory Droid, and Cursor this is the plugin-bundle hook
location placed inside the native root.

#### Scenario: Every harness target receives hooks in both scopes and modes

- **WHEN** hooks from independent registries are synced to each harness target
  at repository and machine scope, in both materialization modes
- **THEN** every selected hook MUST appear in that target's file for that
  scope, as listed in the table

#### Scenario: Claude Code hooks use the plugin hook file

- **WHEN** a hook is compiled for Claude Code
- **THEN** the artifact MUST be a `hooks/hooks.json` fragment whose entries
  sit under `hooks.<event>`
- **AND** each handler MUST carry its `type`, its `matcher` if present, and its
  payload directly, with no matcher-group wrapper

### Requirement: Hook events translate through a fixed table with per-target loss severity

The compiler SHALL translate canonical hook events through one fixed table.
Windsurf splits `PreToolUse` by tool matcher. When the table gives no
equivalent, the hook artifact is suppressed. The diagnostic is an error for
Factory Droid, Devin CLI, and Codex and a warning for Gemini CLI and Windsurf.
The Claude Code and Cursor compilers do not validate the event and emit it
unchanged.

#### Scenario: Gemini and Windsurf suppress unmappable events with a warning

- **WHEN** a `Stop` hook is compiled for Gemini CLI, or a `Notification` hook
  for Windsurf
- **THEN** no artifact MUST be emitted
- **AND** the diagnostic MUST be the warning `hook.gemini.event-unmappable` or
  `hook.windsurf.event-unmappable`, respectively

#### Scenario: Factory, Devin, and Codex suppress unmappable events with an error

- **WHEN** a hook whose event has no equivalent for Factory Droid, Devin CLI,
  or Codex is compiled for that target
- **THEN** no artifact MUST be emitted
- **AND** the diagnostic MUST be an error: `hook.event-unmappable`,
  `hook.devin.event-unmappable`, or `hook.codex.unsupported`, respectively

#### Scenario: Windsurf splits PreToolUse by tool matcher

- **WHEN** a `PreToolUse` hook with matcher `Bash` is compiled for Windsurf
- **THEN** its event MUST be `pre_run_command`
- **AND** a `PreToolUse` hook with an empty or unknown matcher MUST be treated
  as unmappable

#### Scenario: Claude Code emits unknown events unchanged

- **WHEN** a hook with an event outside the table is compiled for Claude Code
- **THEN** the artifact MUST carry the event name unchanged, with no
  diagnostic

### Requirement: Matchers and timeouts pass through untranslated

The compiler SHALL copy a hook's tool matcher string unchanged to every
harness target except Windsurf `PreToolUse` event selection. It SHALL copy the
timeout integer unchanged wherever the target emits one, with no unit
conversion. Explicit timeouts are refused on Windsurf and capped for Codex
`SessionEnd`. Negative timeouts are refused on Devin CLI and Codex.

#### Scenario: Gemini receives the matcher and timeout verbatim

- **WHEN** a `PreToolUse` hook with matcher `Read|Edit` and timeout `2500` is
  compiled for Gemini CLI
- **THEN** the `BeforeTool` group MUST carry matcher `Read|Edit`
- **AND** the handler MUST carry timeout `2500`

#### Scenario: Windsurf refuses explicit timeouts

- **WHEN** a hook with a timeout is compiled for Windsurf
- **THEN** no artifact MUST be emitted and the diagnostic MUST be the error
  `hook.windsurf.timeout-unsupported`

#### Scenario: Codex caps SessionEnd timeouts

- **WHEN** a `SessionEnd` hook with a timeout above three seconds is compiled
  for Codex
- **THEN** the emitted timeout MUST be `3` and a `compile.codex.field-dropped` warning
  MUST be emitted

#### Scenario: Negative timeouts are refused for Devin and Codex

- **WHEN** a hook with a negative timeout is compiled for Devin CLI or Codex
- **THEN** no artifact MUST be emitted and the diagnostic MUST be an error

### Requirement: Handler types are supported per harness target

Claude Code, Gemini CLI, and Devin CLI SHALL emit prompt hooks without
inventing a command. Factory Droid, Cursor, Windsurf, and Codex SHALL suppress
prompt hooks with a diagnostic. Every compiler except Claude Code SHALL
suppress any other non-command type with a diagnostic.

#### Scenario: Prompt hooks compile where supported

- **WHEN** a prompt hook is compiled for Claude Code or Gemini CLI
- **THEN** the artifact MUST contain the prompt and MUST NOT contain a
  `command` member

#### Scenario: Prompt hooks are suppressed where unsupported

- **WHEN** a prompt hook is compiled for Factory Droid, Cursor, or Windsurf
- **THEN** no artifact MUST be emitted and the only diagnostic MUST be
  `hook.prompt.unsupported`

#### Scenario: Factory drops other non-command types with a warning

- **WHEN** a hook of a type other than `command` or `prompt` is compiled for
  Factory Droid
- **THEN** no artifact MUST be emitted and the diagnostic MUST be the warning
  `hook.factory.non-command-type-dropped`

### Requirement: Plugin-root tokens are rewritten per harness target

The compiler SHALL treat `${CLAUDE_PLUGIN_ROOT}` in a hook command as follows:

- Claude Code keeps the token.
- Factory Droid rewrites it to `${DROID_PLUGIN_ROOT}`. Materialization then
  replaces that with the target's install root: a repository-relative path at
  repository scope, an absolute path at machine scope.
- Cursor rewrites it to `${CURSOR_PLUGIN_ROOT}`.
- Gemini CLI rewrites it to `${extensionPath}`.
- Windsurf, Devin CLI, and Codex replace it with the source bundle's root
  directory in the local cache or registry checkout. If no root is known, the
  token is left in place and a warning is emitted.

#### Scenario: Factory commands point into the install root

- **WHEN** a repository-scope Factory Droid hook command references the plugin
  root
- **THEN** the materialized command MUST reference the repository-relative
  Factory install root instead of any variable

#### Scenario: Cursor and Gemini receive harness variables

- **WHEN** a hook command referencing `${CLAUDE_PLUGIN_ROOT}` is compiled for
  Cursor or Gemini CLI
- **THEN** the command MUST reference `${CURSOR_PLUGIN_ROOT}` or
  `${extensionPath}`, respectively

#### Scenario: Codex, Devin, and Windsurf receive the source bundle root

- **WHEN** a hook command referencing `${CLAUDE_PLUGIN_ROOT}` is materialized
  for Codex, Devin CLI, or Windsurf from a cached source bundle
- **THEN** the token MUST be replaced by the bundle's root directory

#### Scenario: Missing plugin root leaves the token with a warning

- **WHEN** such a command is compiled for Codex, Devin CLI, or Windsurf
  without a known plugin root
- **THEN** the token MUST remain in the command
- **AND** a plugin-root warning MUST be emitted for that target

### Requirement: Hook support files are copied beside the hook file and never silently chosen

Materialization SHALL install every regular sibling file of a source hook
file, except the hook definition itself and native aggregate siblings, into
the harness target's hook support directory: `hooks/` for Claude Code, Factory
Droid, and Cursor, and `.windsurf/hook-support/` for Windsurf. Devin CLI,
Codex, and Gemini CLI receive no hook support files. When selected hooks
would write different bytes or permissions to the same support path for one
target and scope, every affected hook SHALL be refused with
`materialize.hook.support-conflict` before any aggregate changes. Previously
installed entries and files SHALL be kept.

#### Scenario: Support scripts are installed beside the hook file

- **WHEN** a Factory Droid hook source directory contains a script next to
  `hooks.json`
- **THEN** sync MUST install the script in the Factory hook directory beside
  the merged hook file

#### Scenario: Conflicting support files refuse the affected hooks

- **WHEN** two selected hooks provide the same support file name with
  different bytes for one target and scope
- **THEN** both hooks MUST be refused with `materialize.hook.support-conflict`
- **AND** previously installed hook entries and support bytes MUST be kept
- **AND** unrelated valid hooks MUST still be materialized

### Requirement: Shared hook files keep handwritten entries and converge

Hook reconciliation SHALL change only owned entries. Handwritten entries
SHALL keep their bytes and order. Owned entries SHALL be pruned when their
hook is deselected. A second sync with unchanged inputs SHALL be
byte-identical. For Devin CLI and Codex, a handwritten entry that is
semantically identical to a desired entry satisfies it: no duplicate is
written, and a warning is emitted.

#### Scenario: Deselected hook is removed from the merged file

- **WHEN** one of two synced hooks sharing an event is excluded and sync runs
  again
- **THEN** its entry MUST be removed and the other entry MUST remain

#### Scenario: Repeat sync is byte-identical

- **WHEN** sync runs twice with unchanged hook selections
- **THEN** the second run MUST leave every hook file byte-identical

#### Scenario: Handwritten Devin hooks keep order and satisfy identical entries

- **WHEN** a Devin hook file contains handwritten groups, one of them
  identical to a selected hook
- **THEN** the handwritten groups MUST keep their order and bytes
- **AND** no duplicate entry MUST be written, and
  `materialize.devin.hook-satisfied-by-unmanaged` MUST be emitted

### Requirement: Only Codex reports hook trust review

When sync changes repository-scope Codex hook or MCP configuration, it SHALL
emit `materialize.codex.trust-review`. When `sync --check` finds such a change
pending, it SHALL emit `materialize.codex.trust-review-pending` without
writing anything. No other harness target emits a trust-review diagnostic for
hook changes.

#### Scenario: Codex repository hook change reports trust review

- **WHEN** sync adds, changes, or removes a repository-scope Codex hook entry
- **THEN** `materialize.codex.trust-review` MUST be emitted
- **AND** a sync that changes no Codex hook bytes MUST NOT emit it

### Requirement: Hooks from independent registries are additive

Hook selection SHALL keep every selected hook from independent registry
sources, even when registry-local names, events, types, matchers, or commands
coincide. Repeated references to one source SHALL be deduplicated. Competing
refs of one source at one location and scope SHALL keep configuration
precedence. Non-hook resource kinds SHALL keep winner-based collision
selection.

#### Scenario: Same-named hooks from two registries both install

- **WHEN** two registries each provide a hook with the same registry-local
  name and both are selected
- **THEN** both hooks MUST be resolved and materialized

#### Scenario: Install all includes every hook provider

- **WHEN** `install --all` runs with two registries that provide same-named
  hooks
- **THEN** the configuration MUST select both providers' hooks

#### Scenario: Collections keep additive hook providers

- **WHEN** a collection expands to hooks that share names across registries
- **THEN** every provider's hook MUST remain selected after expansion

### Requirement: Hook compile errors suppress only the affected hook

When a hook produces an error diagnostic, that hook's contribution to the
shared file SHALL be suppressed. Sibling hooks SHALL still be emitted. The
blocked hook's previously installed owned entries SHALL be kept rather than
pruned.

#### Scenario: A bad Devin hook does not suppress good ones

- **WHEN** a Devin bundle contains one hook that errors and one valid hook
- **THEN** the valid hook MUST be emitted

#### Scenario: Blocked Windsurf hooks keep their installed entries

- **WHEN** a previously installed Windsurf hook becomes blocked while a
  sibling hook stays valid
- **THEN** the blocked hook's installed entry MUST be kept and the sibling
  MUST be reconciled

### Requirement: Devin hook diagnostics are value-free

Devin CLI hook diagnostics SHALL NOT include a command string, prompt body,
matcher value, or timeout value.

#### Scenario: Devin diagnostics never quote payloads

- **WHEN** a Devin hook compile produces diagnostics for a hook containing a
  distinctive command, prompt, and matcher
- **THEN** no diagnostic message or hint MUST contain those values

### Requirement: Marketplace analysis flags hook capability

Marketplace analysis SHALL attach the finding
`marketplace.find.hook-capability`, with outcome `warn`, to every hook
resource revision.

#### Scenario: Hook resource yields the capability finding

- **WHEN** a hook resource is analyzed
- **THEN** its report MUST contain `marketplace.find.hook-capability`

### Requirement: Legacy Windsurf hook locks keep their inventory

A Git acquisition lock written before source-revision v3 SHALL keep its
original Windsurf hook inventory through `sync`, `sync --locked`, and
`sync --check` until an explicit `update` adopts the current generator.

#### Scenario: Legacy Windsurf hook lock is retained until upgrade

- **WHEN** a repository has an acquisition lock whose Windsurf hook source
  revision predates v3
- **THEN** sync MUST keep the locked revision and lock bytes
- **AND** only an explicit update MUST move the lock to the current
  source-revision version

### Requirement: Project-directory references keep the repository directory when hooks run from it

When a harness target's hook catalog declares that repository hooks run from the repository root, the compiler SHALL translate each project-directory reference in a cross-harness hook command by its shell word:

- A reference that forms the whole shell word, alone or followed by one `/`, SHALL become `.`. This covers the bare `$NAME`, braced `${NAME}`, and double-quoted forms. A double-quoted reference SHALL keep its quotes.
- A reference followed by `/` and a further path component SHALL become that repository-relative path.
- A reference joined to following text without `/` has no repository-relative form. The hook SHALL be suppressed with the error `hook.env-unrepresentable` and no artifact, and the diagnostic SHALL NOT quote the command.

The generated command SHALL never replace the repository directory with an empty shell word or with a path that starts at the filesystem root; a reference followed by more than one `/` SHALL become `./` followed by the rest of the path.

#### Scenario: Whole-word project-directory references keep the repository directory

- **WHEN** Claude Code hooks whose commands are `cd $CLAUDE_PROJECT_DIR && pwd -P`, `cd ${CLAUDE_PROJECT_DIR} && pwd -P`, `cd "$CLAUDE_PROJECT_DIR" && pwd -P`, and `cd "$CLAUDE_PROJECT_DIR"/ && pwd -P` are compiled for Windsurf
- **THEN** the commands MUST be `cd . && pwd -P`, `cd . && pwd -P`, `cd "." && pwd -P`, and `cd . && pwd -P`, respectively
- **AND** a command of `"$CLAUDE_PROJECT_DIR"/bin/check` MUST still be `bin/check`, and `"$CLAUDE_PROJECT_DIR"//bin/check` MUST be `./bin/check`
- **AND** when each installed handler is run from its native working directory at repository and user scope in both materialization modes, it MUST print the repository directory, never HOME

#### Scenario: Joined project-directory references refuse the hook

- **WHEN** a Claude Code hook whose command is `cp notes ${CLAUDE_PROJECT_DIR}-backup` is compiled for Windsurf
- **THEN** no artifact MUST be emitted and the diagnostic MUST be the error `hook.env-unrepresentable` without quoting the command
- **AND** the same hook compiled for Factory Droid MUST still use `FACTORY_PROJECT_DIR`

### Requirement: Removal refuses unowned active native hook residue

Before `remove` publishes configuration or acquisition-lock changes, it SHALL
check native hook aggregate entries that would cease to be selected, including hooks
expanded from a removed collection or source. If a matching active native entry
lacks clean ownership proof, removal SHALL fail with `hook.remove-unowned`,
identify the harness target, resource and native file, explain that the hook
remains active, and suggest reviewing the file or explicitly repairing ownership
before retrying. It SHALL NOT report successful removal. Configuration,
acquisition locks, native hook files and ownership state SHALL remain unchanged.
An absent ownership-state root SHALL remain absent. Diagnostics SHALL NOT quote
hook commands, prompts, matchers or other executable payloads.

Full native-value equivalence MAY identify residue but SHALL NOT grant
ownership, adoption or deletion authority. Handwritten matches and missing or
corrupt ownership state SHALL retain fail-closed preservation. A native entry
still required by another selected hook SHALL NOT block deselection of one
contributor. Absent native entries and cleanly owned stale entries SHALL retain
ordinary removal behavior.

This aggregate preflight SHALL apply only to targets with a documented native
hook aggregate projection. Targets with file-based hook declarations instead
SHALL retain their ordinary file-ownership and removal behavior. The aggregate
preflight SHALL NOT inspect, adopt or rewrite their declaration or adapter files.

#### Scenario: Independent checkout refuses removal of a committed hook

- **WHEN** a teammate with a separate checkout and HOME removes a repository hook committed by another teammate for a target with a documented native hook aggregate projection
- **THEN** removal MUST refuse with `hook.remove-unowned`, identify the active native file and explain the ownership limitation
- **AND** configuration, acquisition locks, native hook files and ownership state MUST remain unchanged, with no successful-removal message

#### Scenario: Handwritten matches and unavailable proof cannot authorize removal

- **WHEN** a deselected hook matches an active handwritten native entry, or its ownership state is missing or corrupt
- **THEN** removal MUST refuse without adopting, deleting or rewriting the entry or publishing configuration or acquisition-lock changes
- **AND** diagnostics MUST NOT contain executable hook payloads

#### Scenario: Remaining contributors and owned removal stay supported

- **WHEN** a native entry remains selected by another hook, is absent, or is cleanly owned by the installation performing removal
- **THEN** removal MUST retain its ordinary selection and owned-entry cleanup behavior
- **AND** unrelated handwritten entries and entries in other scopes MUST remain unchanged

#### Scenario: File-based hook targets keep ordinary removal behavior

- **WHEN** OpenCode file-based hooks are selected or deselected without active native hook aggregates
- **THEN** the aggregate preflight MUST report no active aggregate residue and MUST NOT refuse the file-based hook removal
- **AND** its declaration and adapter files MUST remain unchanged by the preflight, with ordinary file-ownership and removal rules retained
