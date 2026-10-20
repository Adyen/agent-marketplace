## Context

`specaudit gate` already reads the pull request range (`base...head` paths and `base..head` commit trailers) and fails behavior changes that lack a spec delta unless a commit carries `Spec-Impact: none — <reason>`. The `Specs` CI job runs it on every pull request. The TUI QA suite (`tools/tui-qa`) runs real-terminal journeys against the built binary, and CI already selects it whenever TUI code changes; what is missing is a requirement that the change also extends the suite.

## Goals / Non-Goals

**Goals:** make "TUI change ships a journey" a failing check rather than advice, with an explicit, reasoned escape for changes that have no user-observable TUI effect; make the rule obvious to coding agents in always-loaded guidance.

**Non-Goals:** judging whether a journey actually exercises the changed code, coverage measurement, changing which CI jobs run, or requiring journeys for non-TUI changes that the TUI consumes indirectly (pipeline, resolver).

## Decisions

- **Extend the existing change gate instead of adding a command or CI step.** Both rules share the same input (changed paths plus trailers) and the same CI step, so contributors see both verdicts in one place. The gate becomes a list of rules that each have a trigger set, evidence, a trailer key, and stable finding kinds; the spec-delta rule keeps its existing output.
- **TUI-bearing files are `internal/tui/` and `cmd/agent-marketplace/tui*.go`, excluding tests, Markdown, and `testdata/`.** That is the TUI package plus its launch wiring. Shared packages are excluded because they are not TUI-specific, and requiring a journey for every resolver change would make the trailer routine and so meaningless.
- **Evidence is an added or modified `*.spec.ts` or `*.test.ts` under `tools/tui-qa/tests/`.** Helpers and fixtures enable journeys but prove nothing alone. Deletions are read from a second `--diff-filter=D` diff, so a deleted journey cannot count as evidence.
- **Separate `TUI-QA-Impact` trailer.** Reusing `Spec-Impact` would let one trailer excuse both obligations, and these are different claims: "no spec-visible behavior change" versus "no user-observable TUI effect".

## Risks / Trade-offs

- [A trivial journey edit or rename satisfies the gate] → the gate proves a journey changed; review and the guidance require that the journey drives the changed flow and asserts its visible result.
- [Pure refactors under `internal/tui/` need a trailer] → this is intended friction: the author must state why no user-visible effect exists.
