# Agent Instructions

## Project: harness-compilation

A Go library that compiles AI-agent configuration resources written in a single canonical source format (Claude-plugin layout) into vendor-specific harness layouts. Six conversion targets — **Claude Code**, **Factory Droid**, **Cursor**, **Windsurf**, **Gemini CLI**, and **Devin CLI** — each register both a compiler and an ingester, so each is valid as a conversion source and a conversion destination. `target/target.go` is the authoritative registry and `ValidateTargetRegistrations()` enforces that the declared capabilities and the registered implementations agree in both directions.

Whether a target is selected for a given user is consumer policy, not a library restriction.

The primary consumer is [`agent-marketplace`](https://github.com/Adyen/agent-marketplace), which handles distribution, registries, and UX. This library handles only the translation.

### Intent

- **Resource-centric.** The atomic unit is one skill, one command, one subagent, one hook, or one MCP server. A "plugin" is just a bundle of such resources.
- **Pure core, optional I/O.** `Compile` is pure (`Resource` → `[]Artifact` + `[]Diagnostic`); `Materialize` is the only layer that touches disk (atomic staged-then-rename, with dry-run and optional prune).
- **Honest about loss.** When a target cannot represent a resource, emit a structured `Diagnostic` with a stable code and skip the artifact — never silently drop or fake it.
- **Business logic concentrated in mapping tables** (models, tools, env tokens, MCP interpolation), each overridable via `Option`s so consumers don't have to wait on the library for new vendor IDs.

### Goals

- Deterministic, byte-stable translation across the 6 targets (golden-file tested).
- Claude → IR → Claude round-trip must be byte-stable.
- Stable public API: `Compile`, `CompileBundle`, `Materialize`, per-resource `Load*` functions.
- A versioned diagnostic-code catalog (`diag/codes.md`) that consumer UIs can key off.
- Day-1 resource coverage: **Skills, Commands, Subagents, Hooks, MCP servers** (~95% of real plugins). **Rules** (coding conventions) are also supported: Factory `.factory/rules/*.md` (plain markdown), Cursor/Windsurf `.mdc` (frontmatter with globs/alwaysApply, shared via `compile/mdc`), and a no-primitive diagnostic for Claude/Gemini.
- Per-resource compile is canonical; bundle compile is a thin wrapper and merges fragment artifacts (e.g. multiple MCP entries → one `mcp.json`).

### Non-goals

- Distribution, registries, sync, or drift reconciliation (owned by `agent-marketplace`).
- Rendering, TUIs, CLIs beyond an optional local-testing `cmd/hcc`.
- A neutral bundle manifest. (Reverse-direction conversion is no longer a non-goal: every target has an ingester, so any target can be a conversion source. Ingestion is semantic, not byte-exact, and each irreversible case is a declared entry in the loss matrix.)
- Resources outside the Day-1 scope: LSP servers, monitors, channels, `userConfig` prompt flow, plugin `dependencies`.
- Interactive management, live reloading, or anything beyond file-format translation.

### Design reference

Authoritative design: [`docs/plans/2026-04-23-harness-compilation-design.md`](docs/plans/2026-04-23-harness-compilation-design.md). Keep changes consistent with the locked decisions (source format, lossy policy, output API split, mapping-table strategy) documented there.
