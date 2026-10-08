## REMOVED Requirements

### Requirement: Hooks materialize into a fixed file per harness target and scope

**Reason**: Claude Code, Cursor, and Factory Droid output moves from plugin-layout `hooks/hooks.json` to each harness's native hook file.

**Migration**: Replaced by "Hooks materialize into each harness target's native hook file"; owned legacy entries move automatically on the next sync (see "Owned hook entries move out of legacy plugin-layout files").

### Requirement: Hook events translate through a fixed table with per-target loss severity

**Reason**: Event translation becomes catalog-driven and every unmappable event is an error on every harness target.

**Migration**: Replaced by "Hook events translate through shipped harness catalogs and fail closed". Hooks that relied on unchecked Claude Code or Cursor events, or on warning-only suppression, now fail sync for that hook; use an event the target documents.

### Requirement: Matchers and timeouts pass through untranslated

**Reason**: Matchers are translated through catalog tool names or refused, and timeouts are converted to the target unit.

**Migration**: Replaced by "Matchers and timeouts are translated or refused". Cross-harness hooks with regex matchers must use exact tool-name alternations or be authored in the target harness layout.

### Requirement: Handler types are supported per harness target

**Reason**: Handler-type support is declared per event in catalogs and every unsupported type is an error.

**Migration**: Replaced by "Handler types are declared per event by harness catalogs". Gemini CLI prompt hooks and arbitrary Claude Code types are now refused.

### Requirement: Plugin-root tokens are rewritten per harness target

**Reason**: Plugin-only variables and cache paths do not resolve in native harness roots.

**Migration**: Replaced by "Plugin-root tokens resolve to the installed hook support directory".

### Requirement: Hook support files are copied beside the hook file and never silently chosen

**Reason**: Support files move to a per-hook directory on every harness target.

**Migration**: Replaced by "Hook support files install into a per-hook directory"; owned files in legacy `hooks/` directories are removed during migration.

### Requirement: Only Codex reports hook trust review

**Reason**: Every harness target now reports hook command changes.

**Migration**: Replaced by "Hook command changes are reported for review on every harness target"; Codex keeps its existing codes.

### Requirement: Devin hook diagnostics are value-free

**Reason**: The value-free rule extends to every harness target.

**Migration**: Replaced by "Hook diagnostics are value-free".

## MODIFIED Requirements

### Requirement: Shared hook files keep handwritten entries and converge

Hook reconciliation SHALL identify owned entries through ownership state on
every harness target and SHALL change only owned entries. Handwritten entries
SHALL keep their bytes and order. Owned entries SHALL be pruned when their
hook is deselected. A second sync with unchanged inputs SHALL be
byte-identical. On every harness target, a handwritten entry that is
semantically identical to a desired entry SHALL satisfy it: no duplicate is
written, and `materialize.hook.satisfied-by-unmanaged` is emitted. Codex and
Devin CLI keep their existing target-specific codes for this case. New
entries SHALL NOT carry an in-file `_managed_by` marker.

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

#### Scenario: Handwritten Claude hooks satisfy identical entries

- **WHEN** `.claude/settings.json` contains a handwritten hook identical to a
  selected hook
- **THEN** no duplicate entry MUST be written
- **AND** `materialize.hook.satisfied-by-unmanaged` MUST be emitted
- **AND** the handwritten hook MUST NOT become owned

#### Scenario: New entries carry no in-file marker

- **WHEN** a hook is synced to any harness target
- **THEN** the written entry MUST NOT contain a `_managed_by` member

## ADDED Requirements

### Requirement: Hooks materialize into each harness target's native hook file

Agent Marketplace SHALL merge each compiled hook into the harness target's
native hook file for the scope, in that harness's native shape:

| Harness target | Repository scope | Machine scope | Shape |
|---|---|---|---|
| Claude Code | `.claude/settings.json` under `hooks` | `~/.claude/settings.json` under `hooks` | event → matcher groups → handlers |
| Factory Droid | `.factory/hooks.json` | `~/.factory/hooks.json` | event → matcher groups → handlers, at top level |
| Cursor | `.cursor/hooks.json` under `hooks`, with `version: 1` | `~/.cursor/hooks.json` under `hooks`, with `version: 1` | event → handlers with optional `matcher` |
| Gemini CLI | `.gemini/settings.json` under `hooks` | `~/.gemini/settings.json` under `hooks` | event → matcher groups → handlers |
| Windsurf | `.devin/hooks.json` under `hooks` | `~/.codeium/windsurf/hooks.json` under `hooks` | event → handlers |
| Devin CLI | `.devin/hooks.v1.json` | `~/.config/devin/config.json` under `hooks` | event → matcher groups → handlers |
| Codex | `.codex/hooks.json` under `hooks` | `$CODEX_HOME/hooks.json` (default `~/.codex`) under `hooks` | event → matcher groups → handlers |

Members of a shared native file that Agent Marketplace does not own SHALL be
preserved byte-for-byte, including comments where the format allows them.

#### Scenario: Every harness target receives hooks in its native file

- **WHEN** hooks from independent registries are synced to each harness target
  at repository and machine scope, in both materialization modes
- **THEN** every selected hook MUST appear in that target's native file for
  that scope, as listed in the table, and in that target's native shape

#### Scenario: Claude Code hooks merge into settings

- **WHEN** a `PreToolUse` hook with matcher `Bash` is synced to Claude Code at
  repository scope, and `.claude/settings.json` already holds `permissions`
  and a handwritten hook
- **THEN** `.claude/settings.json` MUST contain a `hooks.PreToolUse` matcher
  group with matcher `Bash` whose `hooks` array holds the command handler
- **AND** `permissions` and the handwritten hook MUST be unchanged
- **AND** no `.claude/hooks/hooks.json` file MUST be written

#### Scenario: Cursor hooks use the versioned native envelope

- **WHEN** a `UserPromptSubmit` hook is synced to Cursor at repository scope
- **THEN** `.cursor/hooks.json` MUST have `version` `1` and a
  `hooks.beforeSubmitPrompt` handler array containing the command
- **AND** no `.cursor/hooks/hooks.json` file MUST be written

#### Scenario: Factory hooks use the standalone native file

- **WHEN** a hook is synced to Factory Droid at repository scope
- **THEN** `.factory/hooks.json` MUST hold the event map at top level
- **AND** no `.factory/hooks/hooks.json` file MUST be written

#### Scenario: Windsurf repository hooks use the Devin Desktop workspace file

- **WHEN** a hook is synced to Windsurf at repository scope
- **THEN** `.devin/hooks.json` MUST contain the hook under `hooks`
- **AND** no `.windsurf/hooks.json` file MUST be written
- **AND** Devin CLI's `.devin/hooks.v1.json` MUST NOT be read or changed by
  the Windsurf target

### Requirement: Hook events translate through shipped harness catalogs and fail closed

The compiler SHALL translate a hook's event, from the vocabulary of its
source harness, into the target's native event using only the event mappings
in the shipped hook catalogs. When the catalogs give no exact equivalent for
the target, the compiler SHALL suppress that hook's artifact with an
error-severity event diagnostic. This applies to every harness target,
including Claude Code and Cursor. Targets that already have an
event-unmappable code keep it: `hook.gemini.event-unmappable`,
`hook.windsurf.event-unmappable`, `hook.devin.event-unmappable`, and
`hook.codex.unsupported`. Other targets use `hook.event-unmappable`. The
compiler SHALL NOT choose an approximate or differently timed event.

#### Scenario: Unmappable events are errors on every target

- **WHEN** a hook whose event has no catalog equivalent for a target is
  compiled for that target
- **THEN** no artifact MUST be emitted
- **AND** the diagnostic MUST be that target's event-unmappable code with
  error severity
- **AND** sibling hooks in the same sync MUST still be emitted

#### Scenario: Cursor receives native event names

- **WHEN** hooks for `PreToolUse`, `UserPromptSubmit`, and `Stop` are compiled
  for Cursor
- **THEN** their native events MUST be `preToolUse`, `beforeSubmitPrompt`, and
  `stop`

#### Scenario: Claude Code refuses unknown events

- **WHEN** a hook with an event that no catalog maps to Claude Code is
  compiled for Claude Code
- **THEN** no artifact MUST be emitted and the diagnostic MUST be
  `hook.event-unmappable`

#### Scenario: Differently timed events are never equated

- **WHEN** a Devin `PostCompaction` hook is compiled for a target that
  documents only a pre-compaction event
- **THEN** it MUST be suppressed with that target's event-unmappable error

#### Scenario: Windsurf splits PreToolUse by translated tool matcher

- **WHEN** a `PreToolUse` hook with matcher `Bash` is compiled for Windsurf
- **THEN** its event MUST be `pre_run_command`
- **AND** a `PreToolUse` hook with an empty or untranslatable matcher MUST be
  suppressed with the error `hook.windsurf.event-unmappable`

### Requirement: Matchers and timeouts are translated or refused

The compiler SHALL decode a cross-harness tool matcher into the set of tool
concepts it names, using the source harness's shipped tool catalog, including
the aliases it declares. The matcher may be `*`, empty, one exact tool name,
a list of exact tool names, an anchored form of those (`^X$`, `^(X|Y)$`), or
an MCP tool pattern.

The emitted matcher SHALL fire for exactly that concept set on the target:

- it lists every target tool whose concepts all lie within the set;
- the listed tools together SHALL cover the whole set;
- no target tool may cover only part of the set.

For targets that encode the tool in the event name, the event whose tool set
equals the concept set is chosen. Output SHALL use the target's matcher
syntax, and SHALL be anchored (`^X$`, `^(X|Y)$`) for targets whose regex
matchers are unanchored or not exact-list aware. MCP patterns SHALL be
rewritten to the target's MCP naming. A server-scoped MCP pattern cannot be
represented on a target whose MCP naming omits the server.

Any other matcher, or one whose concept set cannot be covered exactly, SHALL
suppress the hook with the error `hook.matcher-untranslatable`. A matcher on
an event that the target defines without matchers SHALL suppress the hook
with the same error, never be dropped.

Timeouts SHALL be converted to the target's documented unit. A
seconds-to-milliseconds conversion multiplies by 1000. A
milliseconds-to-seconds conversion rounds up to the next whole second and
warns with `hook.timeout-rounded`. A timeout above a documented per-event
limit SHALL be lowered to that limit with a warning. Codex keeps
`compile.codex.field-dropped` for this; other targets use
`hook.timeout-capped`. A timeout on a target that cannot represent one SHALL
suppress the hook with an error: Windsurf keeps
`hook.windsurf.timeout-unsupported`, and other targets use
`hook.timeout-unsupported`. A negative timeout SHALL suppress the hook with
an error on every target.

#### Scenario: Tool names are translated per target

- **WHEN** a Claude Code `PreToolUse` hook with matcher `Bash` is compiled for
  Factory Droid, Cursor, Gemini CLI, Devin CLI, and Codex
- **THEN** the emitted matchers MUST be `Execute`, `^Shell$`,
  `^run_shell_command$`, `^exec$`, and `^Bash$`, respectively

#### Scenario: Write and Edit translate to each target's file-change tools

- **WHEN** a Claude Code `PostToolUse` hook with matcher `Write|Edit` is
  compiled for Factory Droid, Cursor, Codex, and Windsurf
- **THEN** the emitted matchers MUST be `^(Create|Edit|ApplyPatch)$`,
  `^Write$`, and `^apply_patch$`, respectively
- **AND** the Windsurf event MUST be `post_write_code`

#### Scenario: Partial tool coverage suppresses the hook

- **WHEN** a Claude Code hook with matcher `Edit` is compiled for Cursor,
  whose `Write` tool also covers file writes, or for Factory Droid, whose
  `ApplyPatch` tool also covers file writes
- **THEN** no artifact MUST be emitted and the diagnostic MUST be the error
  `hook.matcher-untranslatable`

#### Scenario: Renamed tools are accepted through aliases

- **WHEN** a legacy canonical hook with matcher `Task` is compiled for
  Claude Code
- **THEN** the emitted matcher MUST be `Agent`

#### Scenario: Untranslatable matchers suppress the hook

- **WHEN** a hook with matcher `Bash.*`, with a tool name that the target
  catalog does not cover, or with a server-scoped MCP pattern for Cursor is
  compiled for a different harness target
- **THEN** no artifact MUST be emitted and the diagnostic MUST be the error
  `hook.matcher-untranslatable`
- **AND** the hook MUST NOT be emitted without its matcher

#### Scenario: MCP matchers follow the target's MCP naming

- **WHEN** a Claude Code hook with matcher `mcp__github__create_issue` is
  compiled for Gemini CLI
- **THEN** the emitted matcher MUST be `^mcp_github_create_issue$`

#### Scenario: Gemini timeouts are converted to milliseconds

- **WHEN** a hook with a timeout of `30` seconds is compiled for Gemini CLI
- **THEN** the emitted handler timeout MUST be `30000`

#### Scenario: Gemini millisecond timeouts round up on ingestion

- **WHEN** a Gemini hook with timeout `2500` is ingested and compiled for
  Claude Code
- **THEN** the emitted timeout MUST be `3`
- **AND** `hook.timeout-rounded` MUST be emitted

#### Scenario: Windsurf refuses explicit timeouts

- **WHEN** a hook with a timeout is compiled for Windsurf
- **THEN** no artifact MUST be emitted and the diagnostic MUST be the error
  `hook.windsurf.timeout-unsupported`

#### Scenario: Codex caps SessionEnd timeouts

- **WHEN** a `SessionEnd` hook with a timeout above three seconds is compiled
  for Codex
- **THEN** the emitted timeout MUST be `3` and a `compile.codex.field-dropped`
  warning MUST be emitted

#### Scenario: Negative timeouts are refused on every target

- **WHEN** a hook with a negative timeout is compiled for any harness target
- **THEN** no artifact MUST be emitted and the diagnostic MUST be an error

### Requirement: Handler types are declared per event by harness catalogs

The compiler SHALL emit a handler type only where the target's catalog
declares that type for the hook's native event. Any other type SHALL suppress
the hook with an error. An undeclared prompt handler uses
`hook.prompt.unsupported`. Other undeclared types use the target's existing
type code where one exists (`hook.factory.non-command-type-dropped`,
`hook.cursor.unsupported-type`, `hook.gemini.unsupported-type`,
`hook.windsurf.unsupported-type`, `hook.devin.unsupported-type`,
`hook.codex.unsupported`), and `hook.type-unsupported` otherwise. The
compiler SHALL NOT invent a missing payload and SHALL NOT emit both a command
and a prompt for one handler.

#### Scenario: Prompt hooks compile where the catalog declares them

- **WHEN** a prompt hook for an event whose catalog entry allows prompt
  handlers is compiled for Claude Code, Cursor, or Devin CLI
- **THEN** the artifact MUST contain the prompt and MUST NOT contain a
  `command` member

#### Scenario: Prompt hooks are refused where undeclared

- **WHEN** a prompt hook is compiled for Factory Droid, Gemini CLI, Windsurf,
  or Codex
- **THEN** no artifact MUST be emitted and the diagnostic MUST be the error
  `hook.prompt.unsupported`

#### Scenario: Unknown handler types are refused on Claude Code

- **WHEN** a hook with a type that the Claude Code catalog does not declare is
  compiled for Claude Code
- **THEN** no artifact MUST be emitted and the diagnostic MUST be
  `hook.type-unsupported`

### Requirement: Plugin-root tokens resolve to the installed hook support directory

When a hook command contains `${CLAUDE_PLUGIN_ROOT}`, or a plugin-root token
of the source harness, materialization SHALL replace the token with a
reference to that hook's installed support directory:

- At repository scope, the reference SHALL be the target catalog's
  repository base followed by the support directory's repository-relative
  path. The repository base is one of:
  - a documented project-directory variable, such as `"$CLAUDE_PROJECT_DIR"`,
    `"$FACTORY_PROJECT_DIR"`, `"$CURSOR_PROJECT_DIR"`,
    `"$GEMINI_PROJECT_DIR"`, or `"$DEVIN_PROJECT_DIR"`;
  - a documented git-root expression, as for Codex;
  - nothing, where the harness documents that repository hooks run from the
    repository root (Windsurf). The reference is then the bare
    repository-relative path.
- At machine scope, the reference SHALL be the support directory's absolute
  path.

Materialized commands SHALL NOT contain a plugin-only variable
(`${CLAUDE_PLUGIN_ROOT}`, `${DROID_PLUGIN_ROOT}`, `${CURSOR_PLUGIN_ROOT}`,
`${extensionPath}`, `${PLUGIN_ROOT}`) or a path into the Agent Marketplace
cache. If a target
catalog declares no repository base, a repository-scope hook whose command
contains a plugin-root token SHALL be suppressed with the error
`hook.support-path-unrepresentable`. Commands without a plugin-root token
SHALL be emitted unchanged.

#### Scenario: Repository-scope commands use the project-directory variable

- **WHEN** a hook whose command is `${CLAUDE_PLUGIN_ROOT}/scripts/check.sh` is
  synced to Claude Code at repository scope
- **THEN** the command MUST be `"$CLAUDE_PROJECT_DIR"/` followed by the
  repository-relative path of `scripts/check.sh` inside that hook's support
  directory

#### Scenario: Codex and Windsurf repository commands use their documented base

- **WHEN** the same hook is synced at repository scope to Codex and to
  Windsurf
- **THEN** the Codex command MUST start with
  `"$(git rev-parse --show-toplevel)"/`
- **AND** the Windsurf command MUST start with the repository-relative
  support directory path

#### Scenario: Machine-scope commands use absolute support paths

- **WHEN** the same hook is synced to any harness target at machine scope
- **THEN** the command MUST reference the absolute path of `scripts/check.sh`
  inside that hook's machine-scope support directory

#### Scenario: No plugin-only variable or cache path is emitted

- **WHEN** hooks referencing `${CLAUDE_PLUGIN_ROOT}` are synced to every
  harness target in both scopes
- **THEN** no materialized command MUST contain a plugin-only variable or a
  path under the Agent Marketplace cache

#### Scenario: Missing repository base refuses token commands

- **WHEN** a repository-scope hook whose command contains a plugin-root token
  is compiled for a target whose catalog declares no repository base
- **THEN** no artifact MUST be emitted and the diagnostic MUST be the error
  `hook.support-path-unrepresentable`

### Requirement: Hook support files install into a per-hook directory

Materialization SHALL install every regular sibling file of a source hook
file, except the hook definition itself and native aggregate siblings, into
`hook-support/<source>/<hook>/` under the harness target's root for that
scope. `<source>` is a filesystem-safe identifier derived deterministically
from the hook's registry source, and `<hook>` is the hook's installed name.
Relative paths below the source directory SHALL be preserved. The
materialization mode applies to these files. Every harness target, including
Devin CLI, Codex, and Gemini CLI, SHALL receive support files. When two
selected hooks would still write different bytes or permissions to one
support path, both SHALL be refused with `materialize.hook.support-conflict`
before any aggregate changes. Previously installed entries and files SHALL be
kept.

#### Scenario: Same-named scripts from two registries do not conflict

- **WHEN** two registries each provide a hook with a `check.sh` support file
  of different content, and both are synced to one harness target and scope
- **THEN** each script MUST be installed in its own hook's support directory
- **AND** each hook's command MUST reference its own script
- **AND** `materialize.hook.support-conflict` MUST NOT be emitted

#### Scenario: Support files reach Devin, Codex, and Gemini

- **WHEN** a hook with a support script is synced to Devin CLI, Codex, or
  Gemini CLI
- **THEN** the script MUST be installed in that hook's support directory
  under the target's root

#### Scenario: Deselected hooks remove their support directory

- **WHEN** a synced hook with support files is deselected and sync runs again
- **THEN** its support directory MUST be removed
- **AND** files in the target root that Agent Marketplace does not own MUST
  be kept

### Requirement: Hook command changes are reported for review on every harness target

When sync adds, changes, or removes an owned hook entry on any harness
target, it SHALL print a hook review listing to standard output. The listing
SHALL show, for each affected entry, the harness target, scope, file, event,
matcher, and the exact command or prompt before and after the change. Sync
SHALL also emit the warning `materialize.hook.review`, or
`materialize.codex.trust-review` for repository-scope Codex changes.
`sync --check` SHALL print the same listing for pending changes, emit
`materialize.hook.review-pending` (or `materialize.codex.trust-review-pending`
for Codex), and write nothing. A sync that changes no hook bytes SHALL print
no listing and emit no review diagnostic. The listing SHALL NOT be written to
log files or telemetry.

#### Scenario: Changed hook commands are listed with exact values

- **WHEN** a registry update changes a selected hook's command and sync runs
  for Claude Code
- **THEN** standard output MUST list the hook with its old and new commands
- **AND** `materialize.hook.review` MUST be emitted

#### Scenario: Check reports pending hook changes without writing

- **WHEN** `sync --check` runs while a hook change is pending
- **THEN** it MUST print the pending entry and emit
  `materialize.hook.review-pending`
- **AND** no file MUST change

#### Scenario: Unchanged hooks produce no review

- **WHEN** sync runs with no hook byte changes
- **THEN** no hook review listing and no review diagnostic MUST be produced

#### Scenario: Codex keeps its trust-review codes

- **WHEN** sync adds, changes, or removes a repository-scope Codex hook entry
- **THEN** `materialize.codex.trust-review` MUST be emitted and the change
  MUST appear in the review listing

#### Scenario: Review commands never reach logs

- **WHEN** sync prints a hook review listing containing a distinctive command
- **THEN** the command MUST NOT appear in the log file or in emitted telemetry

### Requirement: Hook diagnostics are value-free

Hook diagnostics and log records on every harness target SHALL NOT include a
command string, prompt body, matcher value, timeout value, or support-file
content. Only event names, handler types, resource names, target names, field
names, and file paths are permitted.

#### Scenario: Diagnostics never quote payloads on any target

- **WHEN** hooks with a distinctive command, prompt, and matcher are compiled
  and materialized for every harness target, including cases that produce
  errors and warnings
- **THEN** no diagnostic message or hint, and no log record, MUST contain
  those values

### Requirement: Owned hook entries move out of legacy plugin-layout files

On the first mutating sync after upgrade, Agent Marketplace SHALL remove
every entry marked `_managed_by: agent-marketplace` from each legacy hook
file and write the corresponding selected hooks to the native file, in one
transaction. The legacy files are the `hooks/hooks.json` files of Claude
Code, Cursor, and Factory Droid in each scope, and Windsurf's repository-scope
`.windsurf/hooks.json`. Owned support files in legacy hook support
directories SHALL be removed. Handwritten entries, unmarked files, and other members SHALL be
kept. If the legacy file then contains nothing but an empty hook map, it
SHALL be deleted. If the legacy file cannot be parsed or reconciled safely,
both files SHALL be left unchanged and the hooks for that target and scope
SHALL be refused with `materialize.hook.legacy-malformed`. Read-only commands
SHALL report a pending migration without writing.

#### Scenario: Owned Factory entries move to the native file

- **WHEN** `.factory/hooks/hooks.json` holds one marked entry and one
  handwritten entry, and sync runs
- **THEN** `.factory/hooks.json` MUST contain the selected hook
- **AND** the legacy file MUST keep only the handwritten entry
- **AND** the hook MUST NOT be present in both files

#### Scenario: A legacy file with only owned entries is deleted

- **WHEN** `.claude/hooks/hooks.json` contains only marked entries and sync
  runs
- **THEN** the legacy file MUST be deleted and the hooks MUST be present in
  `.claude/settings.json`

#### Scenario: Owned Windsurf entries move to the Devin Desktop workspace file

- **WHEN** `.windsurf/hooks.json` holds marked entries and a handwritten
  entry, and sync runs for Windsurf at repository scope
- **THEN** `.devin/hooks.json` MUST contain the selected hooks
- **AND** `.windsurf/hooks.json` MUST keep only the handwritten entry

#### Scenario: Malformed legacy files block migration without changes

- **WHEN** `.cursor/hooks/hooks.json` is not valid JSON and sync runs
- **THEN** neither `.cursor/hooks/hooks.json` nor `.cursor/hooks.json` MUST
  change
- **AND** `materialize.hook.legacy-malformed` MUST be emitted

#### Scenario: Check reports a pending migration

- **WHEN** `sync --check` runs while legacy owned entries exist
- **THEN** it MUST report the pending migration and change no file

#### Scenario: Latest release still verifies after migration

- **WHEN** a teammate's candidate client migrates a repository's hooks and
  the latest released client then runs `sync --locked` and `sync --check`
- **THEN** both released-client commands MUST succeed

### Requirement: Hook catalogs are shipped and validated

Hook translation SHALL depend only on the hook and tool catalogs shipped with
the binary. It SHALL NOT use network discovery, an installed harness, or user
configuration. Each shipped hook catalog SHALL pass validation. `make
catalog-check` SHALL fail when any hook catalog:

- has an unknown key or a duplicate key;
- names a canonical event or tool concept missing from the shared vocabulary;
- links two native events to the same canonical event with overlapping tool
  sets;
- declares a handler type on an event that allows none;
- declares a timeout unit other than seconds, milliseconds, or none;
- declares a malformed repository base, or an environment variable name that
  is not a valid shell identifier or is duplicated;
- declares no evidence references.

`make catalog-check` SHALL also fail when a hand-written conversion fixture
disagrees with the resolver output.

#### Scenario: Invalid catalog fails the check

- **WHEN** a hook catalog links two native events to the same canonical event
  with overlapping tool sets
- **THEN** `make catalog-check` MUST fail and name the catalog

#### Scenario: Shipped catalogs pass the check

- **WHEN** `make catalog-check` runs on the shipped catalogs
- **THEN** it MUST pass without network access or installed harnesses

### Requirement: Hook environment-variable references translate or are refused

In a cross-harness hook command, the compiler SHALL rewrite each `$NAME` or
`${NAME}` reference to a variable that the source harness's hook catalog
declares:

- a project-directory reference is rewritten to the target's repository
  base;
- a plugin-root reference is rewritten to the installed support directory;
- any other variable is rewritten to the target's variable for the same
  purpose, where the target sets it for that event.

A reference left unchanged would be empty or wrong on the target. When the
target catalog has no equivalent for a referenced variable, the hook SHALL be
suppressed with the error `hook.env-unrepresentable`. A reference already
accepted by the target under the same name SHALL be kept. Variables that no
catalog declares SHALL be left unchanged. Only the hook command string is
rewritten; support-file contents are never modified.

#### Scenario: Project-directory references follow the target

- **WHEN** a Claude Code hook whose command is
  `"$CLAUDE_PROJECT_DIR"/bin/check` is compiled for Factory Droid, Codex, and
  Windsurf
- **THEN** the commands MUST be `"$FACTORY_PROJECT_DIR"/bin/check`,
  `"$(git rev-parse --show-toplevel)"/bin/check`, and `bin/check`,
  respectively

#### Scenario: Targets that accept the Claude spelling keep it

- **WHEN** the same hook is compiled for Cursor or Gemini CLI
- **THEN** the command MUST keep `"$CLAUDE_PROJECT_DIR"/bin/check`

#### Scenario: Variables without a target equivalent refuse the hook

- **WHEN** a Claude Code `SessionStart` hook whose command writes to
  `$CLAUDE_ENV_FILE` is compiled for Factory Droid
- **THEN** no artifact MUST be emitted and the diagnostic MUST be the error
  `hook.env-unrepresentable`

#### Scenario: Unknown variables are left unchanged

- **WHEN** a hook whose command references `$HOME` is compiled for any
  harness target
- **THEN** the reference MUST be emitted unchanged

### Requirement: Hooks that another selected harness also loads produce a warning

When a sync selects two harness targets in the same scope, and one target's
hook catalog declares that it loads the other target's hook files by default,
sync SHALL emit the warning `hook.duplicate-execution-risk`. The warning
names both targets and the scope. Each target's hook output SHALL stay
unchanged: neither copy is dropped.

#### Scenario: Claude Code and Cursor selected together

- **WHEN** hooks are synced at repository scope with both Claude Code and
  Cursor selected
- **THEN** `hook.duplicate-execution-risk` MUST be emitted naming Claude Code,
  Cursor, and repository scope
- **AND** both `.claude/settings.json` and `.cursor/hooks.json` MUST contain
  the hooks

#### Scenario: Claude Code and Devin CLI selected together

- **WHEN** hooks are synced at machine scope with both Claude Code and Devin
  CLI selected
- **THEN** `hook.duplicate-execution-risk` MUST be emitted naming Claude Code,
  Devin CLI, and machine scope

#### Scenario: A single target produces no duplicate warning

- **WHEN** hooks are synced with only Cursor selected
- **THEN** `hook.duplicate-execution-risk` MUST NOT be emitted

### Requirement: Same-harness hooks keep their native spelling

When a hook's source harness equals the target harness, the compiler SHALL
emit the native event, matcher, timeout value, and native-only fields as
authored, subject to the handler-type, plugin-root, and negative-timeout
rules. It SHALL NOT round-trip them through the canonical vocabulary.

#### Scenario: Factory hooks keep native tool names

- **WHEN** a Factory Droid hook with matcher `Execute` and field
  `commandRegex` is ingested and compiled for Factory Droid
- **THEN** the emitted group MUST keep matcher `Execute` and the
  `commandRegex` value

#### Scenario: Gemini hooks keep millisecond timeouts

- **WHEN** a Gemini hook with timeout `2500` is ingested and compiled for
  Gemini CLI
- **THEN** the emitted timeout MUST be `2500` with no rounding diagnostic

### Requirement: Native hook locations are ingestible

Each harness target's source layout SHALL ingest hooks from that harness's
native hook file, as listed in the output table, in addition to the
locations ingested today. For shared settings files, only the `hooks` member
SHALL be read.

#### Scenario: Claude settings hooks are ingested

- **WHEN** a registry source contains `.claude/settings.json` with a `hooks`
  member and unrelated `permissions`
- **THEN** ingestion MUST produce one hook per handler from `hooks`
- **AND** `permissions` MUST NOT produce a resource

#### Scenario: Cursor native hooks retain native events on ingestion

- **WHEN** a registry source contains `.cursor/hooks.json` with a
  `beforeSubmitPrompt` handler
- **THEN** ingestion MUST produce a hook whose native event is
  `beforeSubmitPrompt`, with Cursor recorded as its source harness

#### Scenario: Devin Desktop workspace hooks are ingested for Windsurf

- **WHEN** a Windsurf registry source contains `.devin/hooks.json` with a
  `pre_run_command` handler
- **THEN** ingestion MUST produce a hook whose native event is
  `pre_run_command`, with Windsurf recorded as its source harness

### Requirement: Existing acquisition locks keep verifying

Hook and tool source provenance SHALL be retained in runtime resources without
introducing a new source-revision schema or generator version. Existing
source-revision versions `v1` through `v4` SHALL keep their historical digest
projections. New hook and tool provenance SHALL NOT change those projections.
Acquisition locks SHALL keep verifying through `sync`, `sync --locked`, and
`sync --check` without an implicit lock refresh. An explicit `update` may
refresh the selected source revision using an already supported generator
version from `v1` through `v4`.

#### Scenario: Pre-change hook lock verifies unchanged

- **WHEN** a repository has an acquisition lock written before this change
  for a registry that provides hooks
- **THEN** `sync --locked` and `sync --check` MUST succeed and keep the lock
  bytes
- **AND** an explicit `update` MAY refresh the selected source revision but
  MUST use an already supported generator version from `v1` through `v4`

### Requirement: Legacy marked Gemini hooks transition in place

When Gemini settings contain flat command hooks written by v3.1.1, Agent Marketplace SHALL transition only entries with exact legacy `_managed_by: agent-marketplace` evidence into currently selected native matcher groups. This applies at repository and machine scope. Legacy evidence authorizes removal of the recognized old entry, not adoption of an unmarked entry. New native entries SHALL contain no ownership marker and SHALL use current captured source proof. Native bytes and ownership state SHALL publish in the existing transaction. Handwritten native groups, comments, relative order, modes and unrelated settings SHALL remain intact. Invalid, ambiguous or unmarked flat shapes SHALL be preserved and refused safely. Incomplete preparation SHALL not remove legacy entries. Read-only commands SHALL report pending changes without writing configuration or state.

#### Scenario: Marked Gemini flat hooks transition at both scopes

- **WHEN** v3.1.1 flat marked command hooks exist in Gemini settings alongside valid handwritten native groups and unrelated settings, and a mutating sync runs
- **THEN** the recognized marked entries MUST be replaced by the selected native matcher groups with no in-file marker
- **AND** handwritten groups and unrelated settings MUST remain intact
- **AND** updated native bytes and source-bound ownership evidence MUST publish together

#### Scenario: Unmarked Gemini flat hooks are refused without adoption

- **WHEN** Gemini settings contain an unmarked, other-owner or malformed flat hook entry
- **THEN** sync MUST refuse that target and scope and preserve its configuration and ownership state
- **AND** no entry MUST be adopted solely because its payload resembles a desired hook

#### Scenario: Gemini legacy preview and failed preparation preserve bytes

- **WHEN** a marked Gemini transition is pending and check mode runs, or preparation is incomplete or publication fails
- **THEN** check mode MUST report pending exact hook changes without configuration or state writes
- **AND** incomplete or unsuccessful publication MUST not report a completed legacy removal
- **AND** configuration and ownership state MUST remain safe under the existing CAS and recovery rules

### Requirement: Superseded hook resource directories remain preserved

Native hook aggregation SHALL preserve target- and scope-declared superseded per-hook resource directories in every materialization mode. Existing cache-backed symlinks and handwritten files inside those directories SHALL NOT be pruned, adopted, or rewritten by generic artifact reconciliation. This preservation SHALL NOT suppress valid native hook publication or pruning of managed stale artifacts outside the preserved directories.

#### Scenario: Legacy Windsurf cache links remain preserved

- **WHEN** a repository or machine scope contains cache-backed per-event Windsurf hooks in its declared preserved legacy directory and a selected hook is synced in symlink or file mode
- **THEN** the existing directory's paths, bytes, modes, modification times and link targets MUST remain unchanged
- **AND** the selected hook MUST appear once in the scope's native hook aggregate without an in-file ownership marker
- **AND** managed stale artifacts outside the preserved directory MUST still be pruned
- **AND** check mode, repeat sync and deselection MUST preserve the legacy resources
