# Windsurf validation

[Shared validation contract](README.md)

## Current status

Windsurf is a supported compiler and materialization target, but native
compatibility CI covers it in configuration-only mode. No verified
credential-free, noninteractive Windsurf CLI inspection surface is enabled.

The matrix runs the Windsurf job in `unavailable` native mode, and each
resource kind carries a capability-gap reason. Configuration assertions still
run, but the result is not native verification. Cascade's documented skills,
workflows, rules, hooks, and MCP management surfaces are hosted in the editor.
An editor launcher or an extension-package listing does not show that those
resource definitions were loaded.

## Configuration coverage

The shared compatibility scenario validates:

- repository and user output roots
- machine-specific workflow directory naming
- skills, commands, hooks, and MCP generated files
- file and symlink materialization modes
- collection expansion and catalog output
- preservation of unrelated aggregate content
- rules through conversion and expected diagnostics
- idempotence and `sync --check`

The suite makes no claim about native Cascade execution, authentication,
model calls, hook firing, or MCP connections.

## Additive native hooks

Hooks are written to `.devin/hooks.json` at repository scope and
`~/.codeium/windsurf/hooks.json` at user scope. The documented
[Cascade hook format](https://docs.devin.ai/desktop/cascade/hooks) is a
`hooks` object that maps event names to handler arrays. Contributions for the
same event append to that event's array, so named resources do not need
separate native files. This is separate from Devin CLI's project-scope
`.devin/hooks.v1.json`.

Shared-file reconciliation preserves handwritten handlers, comments, unrelated
settings, and permissions. New support files go under repository-scope
`hook-support/` or user-scope `~/.codeium/windsurf/hook-support/`, in a
`<source>/<hook>/` directory. Legacy `.windsurf/hooks/` per-event directories
and the user-scope `hooks/` directory stay untouched. A marked entry in legacy
repository `.windsurf/hooks.json` migrates to `.devin/hooks.json`; handwritten
entries remain. Conflicting support-file bytes or modes are refused with
`materialize.hook.support-conflict` before any hook output changes. A skipped
hook keeps its previously managed aggregate entries, and valid sibling hooks
can still materialize.

The documented native handler schema cannot represent an explicit timeout, so
one produces `hook.windsurf.timeout-unsupported`. Conflicting canonical and
native fields produce `hook.windsurf.native-fields-unsupported`. Both are
errors that suppress the unsafe hook artifact.

Existing source revisions v1-v4 preserve their historical digest projections,
identities, and locked revisions. A normal sync does not upgrade or reinterpret
old locks; an explicit update changes inventories. Native ingestion supports
the documented `.devin/hooks.json` path separately.

## Enabling native validation later

Before adding a Windsurf native probe, document and pin a supported headless
command. The probe must include a negative control, stable name and field
assertions, bounded timeouts, credential isolation, and a fixture strategy
that stays local with no network. A desktop or GUI container is not enough
evidence for a CLI compatibility check.
