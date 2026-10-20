## Why

Recent TUI changes merged with only Go model tests, and user flows broke in the real terminal. Go tests build a `Model` and feed it messages; they do not prove that a user pressing keys in a real PTY reaches the changed behavior. The real-terminal TUI QA suite exists, but nothing requires a TUI change to extend it, so contributors and coding agents skip it.

## What Changes

- The change gate (`specaudit gate`, run by the `Specs` CI job on every pull request) also fails when a pull request changes TUI code without adding or modifying a TUI QA journey under `tools/tui-qa/tests/`, and lists the TUI files.
- TUI code means non-test files under `internal/tui/` and the TUI launch wiring in `cmd/agent-marketplace/tui*.go`. Test files, Markdown, and `testdata/` are excluded.
- Deleting a journey, or changing only fixtures and helpers, does not satisfy the gate.
- A change with no user-observable TUI effect may instead carry a `TUI-QA-Impact: none — <reason>` commit trailer. It is independent of `Spec-Impact`: neither trailer excuses the other gate.
- Agent guidance (`AGENTS.md`, `CODING_STANDARDS.md`, the `agent-marketplace-development` skill, and the TUI QA package reference) states the rule, what a journey must prove, and how to run it.

## Capabilities

### New Capabilities

### Modified Capabilities
- `spec-governance`: the change gate requires a TUI QA journey for TUI changes, with its own escape trailer.

## Impact

`tools/specaudit` and its tests, and contributor guidance. No product, CLI, configuration, or TUI behavior changes. Future pull requests that change TUI code must ship a journey or carry the trailer.
