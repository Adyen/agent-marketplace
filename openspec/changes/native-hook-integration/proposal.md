## Why

The `hook-resources` baseline shows that hooks often do not work, or work
differently from what the author wrote, once installed:

- **Claude Code, Cursor, and Factory Droid** receive a plugin-layout
  `hooks/hooks.json` inside the native root. Claude Code never reads it.
  Cursor reads `.cursor/hooks.json`, which has a different envelope and
  different event names. Factory treats the file as legacy and archives it
  the next time it saves hooks.
- **Commands** refer to plugin-root variables that native roots do not set.
- **Tool matchers and subagent tool lists** reach harnesses whose tools have
  different names. Factory droids reject unknown tool IDs, and Cursor
  subagents ignore tool lists entirely.
- **Windsurf (Devin Desktop)** now reads workspace hooks from
  `.devin/hooks.json`, and ignores our `.windsurf/hooks.json` once that file
  defines any hook.
- **Cursor and Devin CLI** also load Claude Code hook files by default, so
  selecting two targets can run a hook twice without warning.
- **Gemini CLI** receives timeouts in seconds but reads them as milliseconds.
- **Only Codex** tells the user that installed hook commands changed.

The harness-specific rules behind all of this are spread across hard-coded
tables in `pkg/harness-compilation` and special cases in `internal`, which
makes each fix risky.

## What Changes

- **BREAKING**: hooks for Claude Code, Cursor, and Factory Droid are written to
  each harness's native hook file (`.claude/settings.json#hooks`,
  `.cursor/hooks.json`, `.factory/hooks.json`, and their machine-scope
  equivalents) in the harness's native shape. Windsurf repository-scope hooks
  move to `.devin/hooks.json`. Owned entries in the legacy files move in the
  same transaction. Handwritten entries stay where they are.
- Two families of shipped, validated catalogs, one small YAML file per harness
  each, following the `harness-model-catalogs` pattern:
  - `toolpolicy`: native tool names and aliases mapped onto one shared set of
    tool concepts. They are used for hook matchers, subagent `tools`, and
    skill `allowed-tools`.
  - `hookpolicy`: events and allowed handler types, hook environment
    variables, timeout unit and limits, matcher syntax, how commands locate
    the repository, and which other harnesses' hook files a harness loads.

  A tool rename, new variable, or new event is a one-line YAML edit plus one
  hand-written fixture line. `make catalog-check` validates catalogs and
  fixtures, and generated reference tables keep the docs in step. The
  researched seed data is in `catalogs/` in this change.
- **BREAKING**: subagent tool lists and skill allowed-tools are translated
  through the tool catalogs. They may narrow but never widen. A subagent with
  a tool list compiled for Cursor, which has no tool list, is now suppressed
  instead of emitting an ignored field.
- Loss policy is fail-closed and uniform: an event, matcher, or handler type
  with no exact equivalent in the target suppresses that hook with an error
  diagnostic. A matcher is never widened, and an approximate event is never
  chosen. Sibling hooks still install.
- When source and target harness are the same, the native event, matcher,
  and timeout spelling are kept.
- Tool matchers are translated only when the target covers exactly the same
  tools, and are anchored for harnesses with unanchored regex matchers (Devin
  CLI). Gemini CLI timeouts are converted to milliseconds, and converted back
  on ingestion.
- Hook environment-variable references are translated between harnesses
  (for example `$CLAUDE_PROJECT_DIR` becomes `$FACTORY_PROJECT_DIR`). A
  reference with no target equivalent refuses the hook.
- Selecting two targets where one loads the other's hook files (Claude Code
  with Cursor, or Claude Code with Devin CLI) produces a
  duplicate-execution warning. Both outputs are kept.
- Support files are installed into a stable per-hook directory,
  `hook-support/<source>/<hook>/`. `${CLAUDE_PLUGIN_ROOT}` is rewritten to
  locate that directory. At repository scope it goes through the harness's
  repository base: a project-dir variable, Codex's documented git-root
  expression, or a relative path where hooks run from the repository root
  (Windsurf). At machine scope it is an absolute path. Plugin-only
  variables and paths into the registry cache are no longer emitted.
- Every harness target reports hook command changes: sync prints a review
  listing the exact commands added, changed, or removed, and emits a
  review diagnostic. `sync --check` reports the same changes as pending
  without writing. Codex keeps its existing trust-review codes.
- Hook diagnostics and logs are value-free on every harness target. Exact
  commands appear only in the review listing on standard output.
- Every hook aggregate is reconciled through ownership state. The in-file
  `_managed_by` marker is read only as migration evidence.
- Hooks can be ingested from each harness's native hook location.
- Existing acquisition locks keep verifying. Hook and tool provenance do not
  introduce a new source-revision schema or generator version; existing
  versions keep their historical digest projections.

## Capabilities

### New Capabilities

- `harness-tool-catalogs`: shipped per-harness tool catalogs and allow-list
  translation for subagent `tools` and skill `allowed-tools`, with catalog
  validation and generated reference docs.

### Modified Capabilities

- `hook-resources`: native output locations and shapes; catalog-driven event,
  matcher, handler-type, and timeout translation with fail-closed loss;
  per-hook support directories and repository-base command rewriting;
  hook review on every target; value-free hook diagnostics everywhere;
  ownership-state reconciliation for every target; legacy entry migration;
  native-location ingestion; same-harness preservation; catalog validity;
  environment-variable translation; duplicate-execution warnings;
  source-revision compatibility.

## Impact

- `pkg/harness-compilation`: new `toolpolicy` and `hookpolicy` packages with
  embedded catalogs and resolvers. `compile/mapping/tools.go`,
  `hook_events.go`, and the hook part of `envvars.go` are deleted, apart
  from a frozen legacy alias table. The subagent and skill tool translation
  in the Claude, Factory, Cursor, Gemini, and Devin compilers moves onto
  `toolpolicy`. Native layouts are extended to declare hook
  aggregates and support directories for all seven harness targets. The
  per-target hook translators, loaders, the event table, and the env-token
  table are reworked to use the resolver. Hook IR gains source provenance.
- `internal/materialize`, `internal/harness`: the Factory `${DROID_PLUGIN_ROOT}`
  expansion, the hook-specific support routing, and the hard-coded hook
  aggregate paths are removed in favor of library declarations. Generic
  ownership reconciliation, legacy-entry migration, and the review report are
  added. Nothing under `internal` branches on a harness target name for
  hooks.
- `internal/sourcerevision`: preserve historical digest projections despite
  runtime hook and tool provenance, with frozen compatibility vectors for
  every existing version.
- `cmd/agent-marketplace`: review report output for `sync` and `sync --check`.
- E2E: pre-v4 upgrade and released-client gates gain a hook migration
  scenario. The released-client gate must show that the latest release still
  passes `sync --locked` and `sync --check` after migration.
- Makefile: a `catalog-check` target that also runs PR #276's
  `model-catalog-check`, and a `catalog-docs` target. PR #276's nightly
  catalog audit gains hook and tool documentation sources.
- Docs: a generated `site/docs/reference/hook-and-tool-names.md`,
  `site/docs/reference/harnesses-and-output-paths.md`,
  `docs/harness-validation/*.md`, and `pkg/harness-compilation/diag/codes.md`
  hook rows.
- Depends on `baseline-hooks` being archived first. This change modifies the
  requirements it creates.
