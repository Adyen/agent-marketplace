## Context

OpenCode hooks are data-only declarations in `hooks/agent-marketplace/` executed by one fixed plugin adapter on `tool.execute.before`. The adapter canonicalizes eleven native tool IDs to Claude-style names before comparing a declaration matcher and compares every other ID as written. It runs handlers with the OpenCode instance directory as the working directory and sets no project or plugin variables. OpenCode 1.18.34 also ships `apply_patch`, and checks `edit`, `write`, and `apply_patch` calls under the `edit` agent permission.

## Decisions

### Resource-file hooks bypass the aggregate pipeline

A target whose native layout declares a hook resource path and no hook aggregate publishes compiled hook artifacts as ordinary resource files. Materialization decides this from the layout, not the target name, so aggregate ownership, support-directory planning, and the hook review stay unchanged for the other harness targets.

### No support directory, no plugin root

Without an installed support directory the compiler receives no plugin root. The hook catalog declares no repository base, so plugin-root and project-directory references are refused with the resolver's code plus `hook.opencode.unsupported`. Emitting a cache or source path would break portability and violate the native hook contract. Installing per-hook support for resource-file hooks is follow-up work.

### Tool catalog keys are hook tool IDs

The tool catalog keys are the IDs `tool.execute.before` reports, with the adapter's canonical names as aliases. `apply_patch` expresses both file write and file edit, so the catalog's exact-coverage rule refuses a lone `Edit` or `Write` matcher instead of installing a bypass. Declarations are written in the adapter's canonical spelling.

### Agent restrictions use permission keys

Agent permission keys group tools, so restrictions are translated in the OpenCode compiler. A key is emitted only when every tool it governs is inside the requested concept set, and the emitted keys must cover the whole set. Other harness targets keep `TranslateToolList`.

### Models resolve through catalogs

OpenCode models are provider-qualified. The catalog maps the previously supported Anthropic identifiers and pins the `sonnet` and `opus` families. Ingestion records `ModelSource: opencode` instead of reverse-mapping, so native identifiers are preserved for OpenCode and resolved by identity for other targets. Commands use the subagent selection surface.

## Risks

- Hooks that reference support files cannot be converted to OpenCode until per-hook support is installed for resource-file hooks.
- `Edit` or `Write` hooks alone are refused for OpenCode; authors must match both, which also covers `apply_patch`.
- The in-flight `native-hook-integration` change specifies review and support behavior for "every harness target" in a seven-target table; its archive must account for OpenCode's resource-file hooks.
