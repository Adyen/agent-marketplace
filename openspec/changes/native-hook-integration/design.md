## Context

See `proposal.md` for motivation and the `hook-resources` baseline for
current behavior. Harness-specific knowledge about hooks and tool names
currently sits in six places:

- **Hard-coded Go tables in `compile/mapping`.** `hook_events.go` holds the
  forward event table and three hand-written reverse tables. `envvars.go`
  holds plugin-root tokens. `tools.go` holds the tool table used for subagent
  `tools` and Devin skill `allowed-tools`.
- **Per-target translators.** Each one re-implements type, event, timeout,
  and tool checks with its own severities.
- **`internal/materialize`.** The Factory `${DROID_PLUGIN_ROOT}` rewrite,
  support-file routing through `harness.TargetInfo.ResourceDirs`, and
  plugin-root injection from the cache bundle root.
- **`internal/harness`.** Hook aggregate paths for Factory, Cursor, Windsurf,
  and Gemini, in `MakeTargetInfo` and `nativeAggregateInstalls`.
- **Codex- and Devin-specific ownership and trust code.**
- **Comments in code.** Evidence dates and provenance live in scattered
  comments rather than next to the data they justify.

PR #276 (`harness-model-catalogs`) set the pattern reused here:

- **Catalogs:** one small embedded YAML catalog per harness, strictly decoded
  and carrying its own evidence.
- **Resolver:** a pure resolver that decodes source-native values, encodes
  target values, and reports loss explicitly.
- **Fixtures:** hand-written conversion fixtures that fail when a catalog edit
  is wrong.
- **Offline gate:** `make model-catalog-check`.
- **Drift audit:** a nightly read-only audit that notices when vendor
  documentation changes.
- **README recipe:** a step-by-step "update a shipped catalog" guide.

### Research: native hook contracts (checked 2026-09-29)

Draft catalogs with this data, including evidence refs, are in this change
under `catalogs/`. They are the seed for implementation (task 1.1).

| Harness | Native file, project / user | Hook environment variables (non-plugin) | Plugin-only variables | Repository base | Timeout | Matcher syntax | Handler types |
|---|---|---|---|---|---|---|---|
| Claude Code | `.claude/settings.json#hooks` / `~/.claude/settings.json#hooks` | `CLAUDE_PROJECT_DIR`; `CLAUDE_CODE_REMOTE`; `CLAUDE_ENV_FILE` (SessionStart) | `CLAUDE_PLUGIN_ROOT`, `CLAUDE_PLUGIN_DATA` | `$CLAUDE_PROJECT_DIR` | seconds | plain name or `\|`/`,` list is exact; otherwise unanchored regex | command, http, mcp_tool, prompt, agent (per event) |
| Factory Droid | `.factory/hooks.json` / `~/.factory/hooks.json` (legacy `hooks/hooks.json` still loads and is archived on the next save) | `FACTORY_PROJECT_DIR`; stdin fields also exported under undocumented names | `DROID_PLUGIN_ROOT`, `CLAUDE_PLUGIN_ROOT` | `$FACTORY_PROJECT_DIR` | seconds | plain name exact; otherwise regex | command |
| Cursor | `.cursor/hooks.json` / `~/.cursor/hooks.json`, `version: 1` | `CURSOR_PROJECT_DIR`, `CLAUDE_PROJECT_DIR` (alias), `CURSOR_VERSION`, `CURSOR_USER_EMAIL`, `CURSOR_TRANSCRIPT_PATH`, `CURSOR_CODE_REMOTE` | `CURSOR_PLUGIN_ROOT` | `$CURSOR_PROJECT_DIR` (project hooks run from the project root, user hooks from `~/.cursor`) | seconds | regex | command, prompt |
| Gemini CLI | `.gemini/settings.json#hooks` / `~/.gemini/settings.json#hooks` | `GEMINI_PROJECT_DIR`, `CLAUDE_PROJECT_DIR` (alias), `GEMINI_PLANS_DIR`, `GEMINI_SESSION_ID`, `GEMINI_CWD`; environment is sanitized | `${extensionPath}` | `$GEMINI_PROJECT_DIR` | **milliseconds** | regex for tool events; exact for lifecycle events | command |
| Windsurf (Devin Desktop) | **`.devin/hooks.json`** (legacy `.windsurf/hooks.json` is read only when `.devin/hooks.json` is absent or defines no hooks) / `~/.codeium/windsurf/hooks.json` | none; `ROOT_WORKSPACE_PATH` only for `post_setup_worktree` | none | working directory defaults to the workspace root, so repository-relative paths | not supported | none; the event name encodes the tool | command (+ `powershell`) |
| Devin CLI | `.devin/hooks.v1.json` / `~/.config/devin/config.json#hooks` | `DEVIN_PROJECT_DIR` | none | `$DEVIN_PROJECT_DIR` | seconds | **unanchored** regex (`exec` matches any tool name containing `exec`) | command, prompt |
| Codex | `.codex/hooks.json` / `$CODEX_HOME/hooks.json` | none | `PLUGIN_ROOT`, `PLUGIN_DATA`, `CLAUDE_PLUGIN_ROOT`, `CLAUDE_PLUGIN_DATA` | `"$(git rev-parse --show-toplevel)"`, as Codex's docs recommend | seconds; SessionEnd 1–3 | regex | command, mcp_tool |

Other hook files each harness loads by default:

- **Cursor** loads `.claude/settings.local.json`, `.claude/settings.json`, and
  `~/.claude/settings.json` hooks while the "Include Third-Party Plugins,
  Skills, and Other Configs" setting is on (the default).
- **Devin CLI** loads the same Claude files, plus `~/.claude.json`, while
  `read_config_from.claude` is on (the default).

### Research: tool names (checked 2026-09-29)

| Concept | Claude Code | Factory Droid | Cursor | Gemini CLI | Devin CLI | Codex | Windsurf event |
|---|---|---|---|---|---|---|---|
| shell | `Bash` | `Execute` | `Shell` | `run_shell_command` | `exec` | `Bash` | `pre/post_run_command` |
| file.read | `Read` | `Read` | `Read` | `read_file`, `read_many_files` | `read` | — | `pre/post_read_code` |
| file.write | `Write` | `Create`, `ApplyPatch`* | `Write`* | `write_file` | `write`, `apply_patch`* | `apply_patch`* | `pre/post_write_code`* |
| file.edit | `Edit` (alias `MultiEdit`) | `Edit`, `ApplyPatch`* | `Write`* | `replace` | `edit`, `apply_patch`* | `apply_patch`* (aliases `Edit`, `Write`) | `pre/post_write_code`* |
| file.list | — | `LS` | — | `list_directory` | — | — | — |
| file.delete | — | — | `Delete` | — | — | — | — |
| search.grep | `Grep` | `Grep` | `Grep` | `grep_search` (alias `search_file_content`) | `grep` | — | — |
| search.glob | `Glob` | `Glob` | — | `glob` | `glob` | — | — |
| web.fetch | `WebFetch` | `FetchUrl` | `WebFetch` | `web_fetch` | `webfetch` | — | — |
| web.search | `WebSearch` | `WebSearch` | `WebSearch` | `google_web_search` | — | not hooked | — |
| subagent | `Agent` (alias `Task`) | `Task` | `Task` | — | `run_subagent` | `spawn_agent` (alias `Agent`) | — |
| MCP pattern | `mcp__{server}__{tool}` | `mcp__{server}__{tool}` | `MCP:{tool}` (no server) | `mcp_{server}_{tool}` | `mcp__{server}__{tool}` | `mcp__{server}__{tool}` | `pre/post_mcp_tool_use` (all MCP) |

\* This single native tool covers both file.write and file.edit.

Tool names differ by surface:

- **Factory droid `tools`** must list valid IDs; unknown IDs fail validation.
- **Gemini agent `tools`** use Gemini names, plus the wildcards `*`,
  `mcp_*`, and `mcp_<server>_*`.
- **Devin skill and subagent `allowed-tools`** accept only `read`, `edit`,
  `grep`, `glob`, and `exec`.
- **Cursor subagents** have no tool list, only `readonly`.
- **Codex custom agents** have no tool list.

Several current mapping cells are wrong: Factory `Bash`, Gemini `Read`, and
Cursor `Bash`/`Edit` all emit names the harness does not use.

## Goals / Non-Goals

**Goals:**

- A code maintainer can rename a tool, add an environment variable, or add
  an event by editing one small YAML file and one fixture line. No Go change
  is needed, and a failing fixture proves the edit.
- One place per concern in `pkg/harness-compilation`:
  - native topology in `target/native_layout.go`;
  - tool names in `toolpolicy`;
  - hook semantics in `hookpolicy`.
- Hook matchers, subagent `tools`, and skill `allowed-tools` share one tool
  vocabulary.
- Nothing under `internal/` branches on a harness target name for hooks.

**Non-Goals:**

- Emitting harness plugins or extensions.
- Windows command rewriting. `powershell` and `commandWindows` stay native
  pass-through fields.
- Granting, editing, or approving harness trust state.
- Proving that hooks fire at runtime.
- Moving other Windsurf resource kinds to `.devin/`.
- Rewriting environment-variable references inside support scripts. Only the
  hook command string is rewritten.

## Decisions

### D1: Two catalog families beside `modelpolicy`, and topology stays in `NativeLayout`

```text
pkg/harness-compilation/
  modelpolicy/   (PR #276) catalogs/<harness>.yaml   models, reasoning
  toolpolicy/    concepts.yaml, catalogs/<harness>.yaml   tool names per surface
  hookpolicy/    catalogs/<harness>.yaml, testdata/conversions.yaml
                 events, env vars, timeouts, handler types, matcher syntax,
                 repository base, hook files other harnesses load
  target/native_layout.go   file paths, members, envelopes, shapes, legacy paths
```

The split follows how often and why things change:

- **Tool names** change most often, when vendors rename or add tools. They
  are shared by three surfaces, so they get their own family.
- **Hook semantics** change when a vendor adds events or variables.
- **Paths** are topology. `NativeLayout` already validates path safety,
  partition disjointness, and scope variance for every kind.

Each family keeps one file per harness, so a vendor update touches one file.
`concepts.yaml` is the only shared vocabulary: tool concepts plus canonical
event names.

*Alternative considered:* one catalog per harness holding everything,
including models. Rejected. Model and tool data change on different cadences
and are reviewed by different evidence, and one large file would make every
vendor edit touch unrelated data.

*Alternative considered:* keep Go tables and add comments. Rejected. That is
the current state, and it is why the tables drifted.

### D2: Catalog schemas

Keys are native names spelled exactly as the vendor documents them, so a
maintainer can search the vendor docs for the key.

**toolpolicy** (`catalogs/toolpolicy/*.yaml` in this change):

| Field | Meaning |
|---|---|
| `schema`, `harness`, `evidence{checked, refs, notes}` | As in `modelpolicy`. Evidence is required. |
| `surfaces` | Surfaces this harness has: `hook_matcher`, `subagent_tools`, `skill_allowed_tools`. A missing surface means the field does not exist and is never emitted. |
| `mcp{pattern, all, server_scoped}` | MCP tool naming, with `{server}` and `{tool}` placeholders. |
| `tools.<Native>.concepts` | Concept set the tool covers. Several concepts means one native tool does several things (Cursor `Write`). |
| `tools.<Native>.aliases` | Old or alternate spellings accepted on ingestion. Output always uses the key. A rename moves the key and keeps the old name as an alias. |
| `tools.<Native>.surfaces` | Narrows a tool to some surfaces (Devin `allowed-tools`). |

**hookpolicy** (`catalogs/hookpolicy/*.yaml`):

| Field | Meaning |
|---|---|
| `env.<concept>` | Variables for native hooks, by concept (`project_dir`, `session_id`, `transcript_path`, `cwd`, `remote`, `env_file`, `version`, `user_email`, `plans_dir`, `workspace_root`). The first name is emitted and later names are accepted. The object form `{names, events}` limits a variable to some events. |
| `plugin_env.<concept>` | Variables that exist only for plugin-bundled hooks (`plugin_root`, `plugin_data`). They are recognized in source commands and never emitted. |
| `repository_base` | How a repository-scope command reaches repository files: `{env: <concept>}`, `{shell: <expression>}`, or `{cwd: repository}` (relative paths). Absent means none. |
| `timeout{unit}` | `seconds`, `milliseconds`, or `none`. |
| `matcher{syntax, anchor, exact_lists}` | `exact-or-regex`, `regex`, or `none`. `anchor: true` forces anchored output. `exact_lists: true` means `A\|B` lists match exactly. |
| `loads_hooks_of` | Other harnesses whose hook files this one loads by default. |
| `events.<Native>` | `canonical` (optional; absent means native-only), `matcher` kind (`tool`, `lifecycle`, `command_text`, `none`), `types`, optional `timeout{min,max}`, and for event-split harnesses `tools` (concept set) or `mcp: true`. |
| `trust_review` | Optional target-specific review codes (Codex). |

Validation is run by `make catalog-check` and the package tests. It rejects:

- unknown keys and duplicate keys;
- concepts or canonical events missing from `concepts.yaml`;
- aliases that collide with a key or another alias in the same catalog;
- two native events that link to the same canonical event with overlapping
  split tool sets;
- a type on an event where the harness declares none;
- a malformed repository base;
- env names that are not valid shell identifiers, and duplicate env names
  within one catalog;
- `evidence` with no refs.

### D3: Maintainer workflow

This is the "easy to tweak" contract. It goes into
`pkg/harness-compilation/README.md` next to PR #276's model recipe.

1. **A vendor renames a tool** (for example `Task` → `Agent`): change the key
   in `toolpolicy/catalogs/<harness>.yaml`, add the old name to `aliases`,
   and bump `evidence.checked`.
2. **A vendor adds a tool:** add a key with its concept set. If no concept
   fits, add one to `concepts.yaml`. That is a reviewed vocabulary change:
   check whether other harnesses have an equivalent.
3. **A vendor adds or renames a hook environment variable:** edit
   `hookpolicy/catalogs/<harness>.yaml` `env`. Put the new name first and
   keep the old one while the harness still sets it.
4. **A vendor adds an event:** add it under `events`. Link it with
   `canonical` only when the vendor documents the same timing and blocking
   role. Otherwise leave it native-only.
5. **Add one independently written case** to
   `hookpolicy/testdata/conversions.yaml` (`matchers`, `events`, `env`,
   `timeouts`, or `allow_lists`), with a hand-written expected value.
6. **Run `make catalog-check`.** It validates every catalog, runs the
   conversion fixtures and the public compiler matrix goldens, and checks
   that the generated reference tables in
   `site/docs/reference/hook-and-tool-names.md` are current. Regenerate them
   with `make catalog-docs`.
7. **Drift:** PR #276's nightly audit tool gains hook and tool documentation
   sources with sha256 baselines. A changed page shows up as
   `source.evidence-changed` in the nightly summary, and the same recipe
   applies.

`make catalog-check` supersedes `make model-catalog-check` by also running
it, so there is one command to remember. `WithToolOverride` stays as the
per-invocation escape hatch, like `WithModelOverride`.

### D4: Resolver interface and data flow

```text
ingest (native file + source harness)
  -> ir.Hook{Event, Matcher, Timeout, Type, Command, ..., HookSource}
  -> hookpolicy.Resolve(hook, target, scope, supportBase)
       uses toolpolicy for matcher concepts
       -> native event, matcher, timeout, type, rewritten command
       -> []Loss{code, severity, field}   // value-free
  -> target serializer (shape, envelope, extras)

ir.Subagent.Tools / ir.Skill.AllowedTools + ToolSource
  -> toolpolicy.TranslateAllowList(tools, source, target, surface)
  -> native names + losses -> existing target diagnostics
```

Both resolvers are pure, use indexes built once from the embedded
catalogs, and are the only path used by `Translate`, `Compile`, and
`CompileBundle`.

### D5: Concept-set translation

A source matcher or tool list is decoded into a concept set S using the
source catalog, including aliases. Legacy canonical input with no provenance
uses the Claude catalog, plus a frozen legacy alias table for old IR
spellings such as `Create`. That table is kept only for compatibility, as
`modelpolicy` does with `legacy_models.go`.

- **Hook matchers need exact coverage.** A guard that fires on fewer actions
  can be bypassed, and one that fires on more changes behavior.
  - For each target tool on the `hook_matcher` surface: include it if its
    concepts are a subset of S; refuse the hook if they only partly overlap
    S.
  - The union of included concepts must equal S; otherwise refuse the hook.
  - Event-split targets (Windsurf) pick the event whose `tools` set equals S.
- **Allow-lists may narrow but never widen.** Include target tools whose
  concepts are a subset of S. Tools that partly overlap are excluded. A
  source tool left uncovered produces the target's existing unknown-tool
  diagnostic, with its existing effect: Factory, Gemini, and Cursor suppress
  the resource; Devin warns and omits `allowed-tools`. A target without the
  surface (Cursor, Codex) has no way to restrict tools, so a non-empty
  source list suppresses the resource instead of silently widening.
- **Parsing.** A matcher is translatable when it is `*`, empty, a plain
  name, a `|` list of plain names (also `,` where the source declares
  `exact_lists`), an anchored form of those (`^X$`, `^(X|Y)$`), or an MCP
  pattern. Anything else is refused cross-harness. There is no general regex
  analysis.
- **Output.**
  - A plain name for `exact-or-regex` targets.
  - A `|` list where `exact_lists` is set.
  - Otherwise `^X$` or `^(X|Y)$` for regex targets, and always when
    `anchor: true` is set (Devin, whose matchers are unanchored).
  - MCP patterns are rewritten between `pattern` forms. A server-scoped
    pattern sent to a target with `server_scoped: false` (Cursor) is refused.

Worked examples are the `matchers` and `allow_lists` sections of
`catalogs/testdata/conversions.yaml`.

### D6: Environment-variable translation

The command string is scanned for `$NAME` and `${NAME}` references, where
`NAME` appears in some catalog's `env` or `plugin_env`. Each reference is
resolved to a concept through the source catalog.

- **plugin_root:** rewritten to the hook's support directory (D8).
- **project_dir:** rewritten to the target's repository base. For
  `{cwd: repository}`, the reference and its following `/` are removed and
  the path becomes repository-relative. This handles a quoted `"$VAR"/` form
  too, and a bare `$VAR` becomes `.`. It is left unchanged when the target
  accepts the same spelling (Cursor and Gemini accept `CLAUDE_PROJECT_DIR`).
- **Any other concept:** rewritten to the target's first name for that
  concept, if the target provides it for the hook's event. Otherwise the hook
  is refused with `hook.env-unrepresentable`.
- **plugin_data:** always refused cross-harness, because native hooks have
  no plugin data directory.

Variables unknown to every catalog, such as `$HOME`, are untouched.
Same-harness output keeps the command unchanged apart from the plugin-root
rewrite.

### D7: Provenance in the IR

- `ir.Hook` gains `HookSource`.
- `ir.Subagent` and `ir.Skill` gain `ToolSource`, a harness name. Empty
  means legacy canonical (Claude vocabulary).

New ingestion keeps native values, such as Factory `Execute`, Gemini
milliseconds, Cursor camelCase events, and Devin `exec`, instead of
normalizing them early. Provenance round-trips through `artifact.resourceJSON`
with the same extras-collision and case-alias rules as PR #276's
`ModelSource`.

### D8: Support directories and command rewriting

The support directory is `<root>/hook-support/<source>/<hook>/`, where
`<source>` is a filesystem-safe slug plus a digest of the registry source
identity, and `<hook>` is the installed hook name. The library receives an
opaque base (repository-relative at repository scope, absolute at machine
scope) and rewrites plugin-root references to either:

- `<repository base>/<relative path>`, or
- `<absolute path>`.

`internal/materialize` supplies the base and installs the artifacts the
library lists. The Factory byte replacement and the cache-root
`WithPluginRoot` injection are deleted. `materialize.hook.support-conflict`
remains for residual collisions.

For the bounded independent-client correction, repository-hook support is
always a regular copy of the verified source bytes and permission bits.
Machine-scope hooks and non-hook resources retain their configured mode.
The writer and compatibility reader use the same pure native projection;
its hook-only topology is derived from the writer's existing `NativeLayout`,
not a second path table. The writer retains its definition-digest and
index-only semantic binding checks.

Support-first sequencing is retained. Regular support copies and their
portable manifest publish through one existing recoverable CAS transaction
before the native/state/legacy transaction. Link conversion requires complete
current clean owned-source evidence, the exact authorized generated cache
target, and verified referent bytes and permissions. Missing, corrupt,
drifted or satisfaction-only evidence grants no conversion authority.
Descriptor-anchored staging and rename distinguish the old link, exact new
regular copy and foreign state; checks create no files or state, and mutating
recovery is idempotent. Native and legacy generation guards remain mandatory.

Quoted-reference rendering must preserve shell semantics. Recognized
plugin-root tokens inside authored single-quoted literals are refused with
`hook.env-unrepresentable`; representable double-quoted and unquoted forms
continue to translate. Support script bytes are never rewritten.

### D9: Ownership state for every hook aggregate

Every hook aggregate is reconciled through the durable ownership state
already used by Devin and Codex, with the `hook/v2` total identity. Native
files are shared settings files, where an in-file `_managed_by` member may be
rejected or surfaced. The Devin and Codex adapters become target-agnostic,
driven by `NativeLayout` shape and member.

### D10: Legacy migration in one transaction

For each target and scope with a declared legacy location, the legacy file is
parsed and the entries marked `_managed_by: agent-marketplace` (existing
evidence) are selected. The declared legacy locations are:

- Claude Code, Cursor, and Factory Droid: `hooks/hooks.json`;
- Windsurf, repository scope: `.windsurf/hooks.json`.

In one `dependencytxn` publish, the migration then:

1. removes the selected entries from the legacy file;
2. deletes the file if only an empty hook map remains;
3. prunes owned legacy support files;
4. writes the native file.

A malformed legacy file refuses that target and scope and leaves both files
unchanged. Read-only commands report pending migration.

The Windsurf move matters even without duplication. Devin Desktop ignores
`.windsurf/hooks.json` entirely as soon as `.devin/hooks.json` defines any
hook. `.devin/hooks.json` is a separate file from Devin CLI's
`.devin/hooks.v1.json`. Neither target's cleanup may touch the other's file,
and a test covers this.

Materialized output is mixed-version state, like a committed format, so the
rule "ship read support before switching writes" applies. See the Migration
Plan.

Gemini is a scoped exception inside the same transaction. v3.1.1 wrote flat
`_managed_by` command entries directly into the native `settings.json` hooks
container, before native matcher groups existed, so there is no separate
legacy file to move. Aggregates declare `LegacyMarkedFlatHooks`; only Gemini's
hook aggregate sets it. When the flag is set and ownership proof is valid or
missing, the reconciler removes recognized marked flat entries from the parsed
document in memory before strict native validation and planning, recording
each removal as a review row. The recognized v3.1.1 shape is fixed:
exact-case `_managed_by: agent-marketplace`, `type: "command"`, a non-empty
string `command`, an optional string `matcher`, an optional non-negative
integer `timeout`, and no other keys. Any other flat shape — unmarked,
other-owner, case-aliased, malformed — is left in place for strict native
validation, which refuses the file unchanged. Removal is never adoption: the
old marker is not written into new entries, no legacy proof is forged, and an
identical handwritten native group still wins through the non-adopting
satisfaction path.

### D11: Duplicate-execution warning

If harness B lists harness A in `loads_hooks_of` and both are selected for
the same scope, sync warns with `hook.duplicate-execution-risk`, naming both
targets and the scope. Output for each target stays independent. Today this
applies to Claude Code + Cursor and Claude Code + Devin CLI. The warning is
data-driven, so new third-party loaders need only a catalog edit.

### D12: Review report

The materialization report gains hook entry changes (added, changed, or
removed), each with target, scope, file, event, and matcher, plus before and
after payloads. The CLI prints them in a "Hook changes" section on standard
output for `sync`, and as pending for `sync --check`. Payload values reach
only that renderer. They never go to `slog`, the log file, telemetry, or
diagnostics.

### D13: Diagnostic codes

Following PR #276, existing codes stay for equivalent losses, with severity
raised to error where the loss policy requires it. New shared codes:

- `hook.matcher-untranslatable`
- `hook.env-unrepresentable`
- `hook.timeout-rounded`
- `hook.timeout-capped`
- `hook.timeout-unsupported`
- `hook.type-unsupported`
- `hook.support-path-unrepresentable`
- `hook.duplicate-execution-risk`
- `materialize.hook.review`
- `materialize.hook.review-pending`
- `materialize.hook.satisfied-by-unmanaged`
- `materialize.hook.legacy-malformed`

Every hook code is value-free.

### D14: Deleting the old tables and enforcing the boundary

Once the catalogs replace them, these are deleted:

- `compile/mapping/hook_events.go`;
- the hook part of `envvars.go`;
- `compile/mapping/tools.go`, apart from the frozen legacy alias table
  (D5);
- the per-target tool and event switches.

MCP env interpolation is unchanged. An architecture test in
`internal/materialize` fails on hook-path comparisons against harness
constants and on plugin-root or project-directory literals, using an
allowlist like `TestCommandIsolationGuard`.

### D15: Source-revision compatibility

Hook and tool provenance are runtime data and must not change the historical
IR JSON fallback projections used by source-revision generators v1 through
v4. Keep the existing schemas and generator versions, with frozen vectors
including hook- and tool-bearing fixtures for each layout. Normal source-byte
hashing is unchanged. No lock is refreshed without `update`. An explicit
update uses an already supported generator version; no new version is added.

## Risks / Trade-offs

- [Previously working cross-harness hooks or subagents fail sync, because a
  matcher, event, type, variable, or tool list is refused] → Each refusal
  names its code and a hint. Sibling resources still install. Fail-closed is
  the chosen policy.
- [Cursor subagents with a `tools` list are now suppressed; they previously
  emitted a field Cursor ignores] → Suppression is the honest result: the
  restriction was never enforced. The hint suggests removing `tools` for
  Cursor.
- [Vendor documentation is incomplete. Factory exports stdin fields as
  variables without naming them; Cursor lists tool names as "values
  include"] → Catalogs list only documented names. An undocumented name
  makes a hook untranslatable, never silently mapped. Evidence notes record
  the gap.
- [Factory Droid rewrites `.factory/hooks.json` when a user saves in
  `/hooks`] → The value-based `hook/v2` identity treats reformatting as drift
  and repairs it rather than duplicating. A reformat test covers this.
- [Gemini timeout rounding lengthens a bound by less than one second] → It
  is warned, and same-harness output keeps exact milliseconds.
- [`.devin/` is also Devin CLI's source marker, so a Windsurf registry source
  containing `.devin/hooks.json` could be autodetected as a Devin CLI layout]
  → `.devin/hooks.json` becomes a Windsurf source marker only where no Devin
  CLI marker (`hooks.v1.json`, `skills/`, `agents/`, `config.json`) is
  present. Ambiguous layouts keep the existing explicit source-layout
  selection. An autodetection test covers both directions.
- [Large refactor] → Vertical slices, one target at a time behind the shared
  resolvers (see tasks). Matrix goldens and E2E gates stay green per slice.

## Migration Plan

0. **Prerequisite reader release.** `hook-native-location-tolerance` accepts
   proven native satisfaction instead of recreating legacy output. Its private
   ownership proof alone is insufficient for independent HOME/XDG actors:
   verified `v4.1.3` still fails this change's unchanged mixed-version gate.
   The separate `hook-native-independent-satisfaction` reader in PR #616
   reconstructs exact complete repository-native and regular support
   equivalence without gaining ownership or prune authority. Human merge and
   release of that reader precedes writer merge. Pin and checksum-verify its
   actual release and pass the unchanged independent-teammate gate before
   merging or archiving this writer; do not invent a future version or share
   private state. Reviewed conditional PR publication may proceed now.
1. Ship `toolpolicy` and `hookpolicy`, and switch translators and allow-lists
   per target.
2. Switch Claude Code, Cursor, Factory Droid, and Windsurf repository scope
   to their native files, together with the legacy migration.
3. Extend `upgrade-gate` with a v3.1.1 hook fixture for all seven targets,
   and `released-client-gate` with a hook migration step.

Rollback: a client that cannot prove native satisfaction may re-emit legacy
files, duplicating Factory hooks or affecting Windsurf loading when
`.devin/hooks.json` exists. A merged reader PR or private shared-state
success does not establish independent-client compatibility. Only the
unchanged gate against an actual released reader clears this boundary.

## Open Questions

- Whether Claude Code repository-scope hooks should go to the committed
  `.claude/settings.json` or to `.claude/settings.local.json`. The design
  follows existing repository-scope semantics (committed). A personal-local
  variant would be a later change.
- Promoting Codex `PreCompact`, `PostCompact`, `SubagentStart`,
  `SubagentStop`, `PermissionRequest`, and `SessionEnd` from native-only to
  linked events. This needs a payload and timing review. It is catalog data
  only and changes neither the spec nor the tasks.
