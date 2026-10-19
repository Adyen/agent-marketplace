## Context

The canonical plugin-root token is `${CLAUDE_PLUGIN_ROOT}`. Each compiler maps it to its own spelling (`${extensionPath}` on Gemini, `${DROID_PLUGIN_ROOT}` on Factory) or substitutes a literal plugin root (Windsurf, Devin, Codex). Native hook projection carries hook commands with their source provenance and resolves project and plugin roots through the shipped catalogs. MCP server values, command prompts and subagent bodies have no such provenance and are canonicalized at ingestion.

## Decisions

1. **Gemini maps only the extension path.** `${extensionPath}` is Gemini's plugin root and stays mapped to `${CLAUDE_PLUGIN_ROOT}`. `${GEMINI_PROJECT_DIR}` and `${CLAUDE_PROJECT_DIR}` are project references and are preserved as written, so Gemini round trips are exact and no target can resolve them inside the plugin.
2. **Factory MCP uses its own plugin-root token in both directions.** Ingestion maps `${DROID_PLUGIN_ROOT}` to the canonical token across command, arguments, environment, URL and headers; compilation maps it back with the shared environment-token table, matching how Factory hooks were already translated.
## Non-goals

A cross-harness crosswalk for project-directory tokens in MCP values; tokens are kept verbatim rather than translated. Cursor plugin-root tokens in MCP values, workspace variables such as `${workspacePath}`, and literal plugin-root resolution for native installs are unchanged. The `hcc` local-testing tool keeps using its source directory as the plugin root: since native hook projection treats a repository-scoped plugin root as a repository-relative support path, making it absolute would break hook output, so that case needs its own option rather than this change.

## Verification

Public `Convert` regressions fail before the change: Gemini project tokens become `${extensionPath}` in Gemini output and `${CLAUDE_PLUGIN_ROOT}` in Claude output, and Devin substitutes the configured literal plugin root for them; Factory output keeps `${CLAUDE_PLUGIN_ROOT}` and Claude output keeps `${DROID_PLUGIN_ROOT}`. Extension-path and plugin-root controls stay mapped.
