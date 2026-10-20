## 1. Tests

- [x] 1.1 Add failing gate tests for every new scenario, annotated with `// spec: spec-governance/<scenario-slug>`, including a Git-range test proving deleted journeys and the `TUI-QA-Impact` trailer are read from the real range; verify they fail on the base

## 2. Implementation

- [x] 2.1 Extend `specaudit gate` with the TUI QA journey rule and `TUI-QA-Impact` trailer while keeping the spec-delta rule's output, and verify `go test ./tools/specaudit` passes

## 3. Guidance and archive

- [x] 3.1 State the rule, what a journey must prove, and how to run it in `AGENTS.md`, `CODING_STANDARDS.md`, `skills/agent-marketplace-development/SKILL.md`, and `tools/tui-qa/README.md`; verify `make spec-check` passes
- [ ] 3.2 Run `openspec archive require-tui-qa-journeys --yes`
