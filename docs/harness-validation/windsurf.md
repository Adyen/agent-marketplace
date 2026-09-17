# Windsurf validation

## Current status

Windsurf is a supported compiler and materialization target, but it is currently **config-only** in native compatibility CI. No verified credential-free noninteractive Windsurf CLI inspection surface is enabled.

The matrix records this as `unavailable`, with a capability-gap reason for each resource kind. Configuration assertions still run; the result is not native verification. Cascade's documented skills, workflows, rules, hooks, and MCP management surfaces are editor-hosted. An editor launcher or extension-package listing does not establish that those resource definitions were loaded.

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

Native Cascade execution, authentication, model calls, hook firing, and MCP connections are not claimed.

## Additive native hooks

Hooks are written to `.windsurf/hooks.json` at repository scope and `~/.codeium/windsurf/hooks.json` at user scope. The documented [Cascade hook format](https://docs.devin.ai/desktop/cascade/hooks) is a `hooks` object mapping event names to handler arrays. Same-event contributions append to those arrays; named resources do not require separate native files.

Shared-file reconciliation preserves handwritten handlers, comments, unrelated settings, and permissions. New supporting files use `hook-support/`. Legacy `hooks/` directories, including previously owned file-mode entries, remain untouched rather than being implicitly migrated or pruned. Conflicting support-file bytes or modes fail closed with `materialize.hook.support-conflict` before hook output is changed. Skipped hooks retain prior managed aggregate entries while valid siblings can still materialize.

Explicit timeouts are not representable in the documented native handler schema and produce `hook.windsurf.timeout-unsupported`. Conflicting canonical/native fields produce `hook.windsurf.native-fields-unsupported`; both are errors that suppress the unsafe hook artifact.

Source revisions v1/v2 retain historical ingestion for existing locks. New v3 inventories prefer native `hooks.json`, preserve all handlers, and keep the previous last-handler survivor's selector. Upgrade locks explicitly to acquire the corrected inventory; a normal sync does not reinterpret old locks.

## Enabling native validation later

Before adding a Windsurf native probe, document and pin a supported headless command. The probe must include a negative control, stable name/field assertions, bounded timeouts, credential isolation, and a local/no-network fixture strategy. A desktop or GUI container is not sufficient evidence for a CLI compatibility check.
