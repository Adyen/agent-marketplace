# Agent Instructions — pkg/harness-compilation

This package is the harness-compilation library embedded in the agent-marketplace root module: it compiles canonical agent resources into, and ingests them from, the native layouts of every supported harness. Agent Marketplace handles distribution, registries, and UX; this library handles only the translation.

## Specs are the authority

The root [AGENTS.md](../../AGENTS.md) rule applies here unchanged: `openspec/specs/` is the only normative description of behavior, every behavior change ships as an OpenSpec change with a spec delta proven by `// spec:` annotated tests, and legacy documents are leads only. Harness translation behavior is specified by the `harness-ingestion`, `harness-compilation`, `diagnostics`, and `harness-<name>` capabilities as they are baselined.

## Build, test, and verification

- `go test ./pkg/harness-compilation/...` from the repository root runs the package tests; `make validate` and `make spec-check` from the root are the pre-handoff gates.
- `go test ./pkg/harness-compilation -run TestGolden` checks byte-exact golden layouts; `UPDATE_GOLDEN=1` with the same command refreshes them after an intended output change.

## Engineering standards

Read the root [CODING_STANDARDS.md](../../CODING_STANDARDS.md) before modifying code.

## Unharvested leads (non-normative)

The text below is carried over verbatim from the previous version of this file. It is not authoritative: a spec or the code wins over it. Baseline changes delete the paragraphs they absorb.

A Go library that compiles AI-agent configuration resources written in a single canonical source format (Claude-plugin layout) into vendor-specific harness layouts. Seven conversion targets — **Claude Code**, **Factory Droid**, **Cursor**, **Windsurf**, **Gemini CLI**, **Devin CLI**, and **Codex** — each register both a compiler and an ingester, so each is valid as a conversion source and a conversion destination. `target/target.go` is the authoritative registry and `ValidateTargetRegistrations()` enforces that the declared capabilities and the registered implementations agree in both directions. `target/native_layout.go` is the topology authority for targets that declare a `NativeLayout`: validated, scope-neutral descriptions of partitions, per-kind resource paths, and shared aggregate files, including scope-variant ones like Devin's project `hooks.v1.json` versus user `config.json#hooks`. `ValidateTargetRegistrations()` validates every declared layout at the same boundary.

Whether a target is selected for a given user is consumer policy, not a library restriction.

The primary consumer is [`agent-marketplace`](https://github.com/Adyen/agent-marketplace), which handles distribution, registries, and UX. This library handles only the translation.

#### Intent

- **Resource-centric.** The atomic unit is one skill, one command, one subagent, one hook, or one MCP server. A "plugin" is just a bundle of such resources.
- **Pure core, optional I/O.** `Compile` is pure (`Resource` → `[]Artifact` + `[]Diagnostic`); `Materialize` is the only layer that touches disk (atomic staged-then-rename, with dry-run and optional prune).
- **Honest about loss.** When a target cannot represent a resource, emit a structured `Diagnostic` with a stable code and skip the artifact — never silently drop or fake it.
- **Business logic concentrated in mapping tables** (models, tools, env tokens, MCP interpolation), each overridable via `Option`s so consumers don't have to wait on the library for new vendor IDs.

#### Goals

- Deterministic, byte-stable translation across the 7 targets (golden-file tested).
- Claude → IR → Claude round-trip must be byte-stable.
- Stable public API: `Compile`, `CompileBundle`, `Materialize`, per-resource `Load*` functions.
- A versioned diagnostic-code catalog (`diag/codes.md`) that consumer UIs can key off.
- Day-1 resource coverage: **Skills, Commands, Subagents, Hooks, MCP servers** (~95% of real plugins). **Rules** (coding conventions) are also supported: Factory `.factory/rules/*.md` (plain markdown), Cursor/Windsurf `.mdc` (frontmatter with globs/alwaysApply, shared via `compile/mdc`), no-primitive diagnostics for Claude/Gemini, and explicit scope diagnostics for Devin/Codex.
- Per-resource compile is canonical; bundle compile is a thin wrapper and merges fragment artifacts (e.g. multiple MCP entries → one `mcp.json`).

#### Non-goals

- Distribution, registries, sync, or drift reconciliation (owned by `agent-marketplace`).
- Rendering, TUIs, CLIs beyond an optional local-testing `cmd/hcc`.
- A neutral bundle manifest. (Reverse-direction conversion is no longer a non-goal: every target has an ingester, so any target can be a conversion source. Ingestion is semantic, not byte-exact, and each irreversible case is a declared entry in the loss matrix.)
- Resources outside the Day-1 scope: LSP servers, monitors, channels, `userConfig` prompt flow, plugin `dependencies`.
- Interactive management, live reloading, or anything beyond file-format translation.

#### Design reference

Authoritative design: [`docs/plans/2026-04-23-harness-compilation-design.md`](docs/plans/2026-04-23-harness-compilation-design.md). Keep changes consistent with the locked decisions (source format, lossy policy, output API split, mapping-table strategy) documented there.
