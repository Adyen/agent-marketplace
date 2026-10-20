## ADDED Requirements

### Requirement: TUI changes carry a TUI QA journey

A pull request SHALL add or modify at least one TUI QA journey whenever it
changes a TUI-bearing file. TUI-bearing files are files under `internal/tui/`
and files directly under `cmd/agent-marketplace/` whose name starts with `tui`,
excluding test files, Markdown files, and anything under a `testdata/`
directory. TUI QA journeys are `*.spec.ts` and `*.test.ts` files under
`tools/tui-qa/tests/`. Deleting a journey MUST NOT count as adding or modifying
one. This requirement is independent of the spec delta requirement: a journey
does not replace a spec delta, and a spec delta does not replace a journey.

#### Scenario: TUI change without journey fails

- **WHEN** the pull request range changes a non-test Go file under
  `internal/tui/` and adds or modifies no TUI QA journey
- **THEN** the change gate MUST fail and list the TUI-bearing files

#### Scenario: TUI launch wiring is TUI-bearing

- **WHEN** the pull request range changes `cmd/agent-marketplace/tui.go` and
  adds or modifies no TUI QA journey
- **THEN** the change gate MUST fail and list that file

#### Scenario: TUI change with journey passes

- **WHEN** the pull request range changes a TUI-bearing file and adds or
  modifies a TUI QA journey
- **THEN** the change gate MUST NOT report a missing TUI QA journey

#### Scenario: Deleted journey does not count

- **WHEN** the pull request range changes a TUI-bearing file and its only TUI
  QA journey change deletes a journey
- **THEN** the change gate MUST fail and report the missing TUI QA journey

#### Scenario: Journey helpers do not count

- **WHEN** the pull request range changes a TUI-bearing file and changes only
  non-journey files under `tools/tui-qa/`, such as fixtures, helpers, or its
  README
- **THEN** the change gate MUST fail and report the missing TUI QA journey

#### Scenario: TUI test-only change needs no journey

- **WHEN** the pull request range changes only test files, Markdown files, or
  `testdata/` content under `internal/tui/`
- **THEN** the change gate MUST pass without a TUI QA journey

### Requirement: TUI QA impact escape trailer

A pull request SHALL be allowed to change TUI-bearing files without a TUI QA
journey when at least one commit in its range carries a
`TUI-QA-Impact: none — <reason>` Git trailer with a non-empty reason. An ASCII
`-` separator MUST be accepted in place of `—`. A `Spec-Impact` trailer MUST
NOT excuse a missing TUI QA journey, and a `TUI-QA-Impact` trailer MUST NOT
excuse a missing spec delta.

#### Scenario: TUI QA trailer with reason passes

- **WHEN** a commit in the range has the trailer
  `TUI-QA-Impact: none — comment-only change`
- **THEN** the change gate MUST NOT report a missing TUI QA journey and MUST
  report the reason

#### Scenario: TUI QA trailer without reason fails

- **WHEN** the only TUI-QA-Impact trailer in the range is `TUI-QA-Impact: none`
- **THEN** the change gate MUST fail and state that a reason is required

#### Scenario: Impact trailers are not interchangeable

- **WHEN** the pull request range changes a TUI-bearing file with neither a
  spec delta nor a TUI QA journey, and carries only a
  `Spec-Impact: none — <reason>` trailer
- **THEN** the change gate MUST fail and report the missing TUI QA journey
- **AND** the same range carrying only a `TUI-QA-Impact: none — <reason>`
  trailer MUST fail and report the missing spec delta
