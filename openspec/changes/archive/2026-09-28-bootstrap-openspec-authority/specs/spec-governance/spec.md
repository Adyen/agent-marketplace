## Purpose

Keeps OpenSpec specs the single, test-bound authority for Agent Marketplace
behavior by checking scenario-to-test traceability, gating behavior changes on
spec deltas, and preventing completed changes from lingering unarchived.

## ADDED Requirements

### Requirement: Scenario identity

Each scenario SHALL be identified as `<capability-path>/<scenario-slug>`, where
`<capability-path>` is the spec directory relative to `openspec/specs/` (or to a
change's `specs/` directory) and `<scenario-slug>` is the scenario title
lowercased with every run of characters other than `a-z` and `0-9` replaced by a
single `-` and leading or trailing `-` removed. Scenario slugs MUST be unique
within a capability.

#### Scenario: Slug derived from title

- **WHEN** capability `owned-entry-reconciliation` contains
  `#### Scenario: Preserves handwritten hooks (Devin)`
- **THEN** its identity MUST be
  `owned-entry-reconciliation/preserves-handwritten-hooks-devin`

#### Scenario: Duplicate slug rejected

- **WHEN** two scenarios in the same capability produce the same slug
- **THEN** the traceability check MUST fail and name the capability and both
  scenario titles

### Requirement: Scenario-to-test traceability

The repository SHALL bind every known scenario to at least one test through an
annotation line of the form `// spec: <capability-path>/<scenario-slug>` in a
test file. Test files are files named `*_test.go`, `*.spec.ts`, or `*.test.ts`.
Known scenarios are the scenarios in `openspec/specs/`, plus scenarios under
ADDED or MODIFIED requirements in active changes, minus requirements that an
active change marks REMOVED.

#### Scenario: Unbound scenario fails

- **WHEN** a known scenario has no annotation in any test file
- **THEN** the traceability check MUST fail and list the scenario identity and
  its spec file

#### Scenario: Dangling annotation fails

- **WHEN** a test file contains an annotation whose identity is not a known
  scenario
- **THEN** the traceability check MUST fail and list the file, line, and
  identity

#### Scenario: Scenario added in an active change

- **WHEN** an active change adds a scenario under ADDED Requirements and a test
  annotates it before the change is archived
- **THEN** the traceability check MUST accept the annotation

#### Scenario: Scenario removed in an active change

- **WHEN** an active change marks a requirement REMOVED
- **THEN** its scenarios MUST NOT be required to have annotations
- **AND** annotations that still reference them MUST fail as dangling

#### Scenario: Fully bound specs pass

- **WHEN** every known scenario has at least one annotation and every
  annotation names a known scenario
- **THEN** the traceability check MUST exit successfully

### Requirement: Behavior changes carry a spec delta

A pull request SHALL change a spec file under `openspec/specs/` or
`openspec/changes/**/specs/` whenever it changes a behavior-bearing file.
Behavior-bearing files are files under `cmd/`, `internal/`, `pkg/`, `api/`, or
`web/`, excluding test files, Markdown files, and anything under a `testdata/`
or `web/tests/` directory.

#### Scenario: Behavior change without delta fails

- **WHEN** the pull request range changes a behavior-bearing file and no spec
  file
- **THEN** the change gate MUST fail and list the behavior-bearing files

#### Scenario: Behavior change with delta passes

- **WHEN** the pull request range changes a behavior-bearing file and at least
  one spec file
- **THEN** the change gate MUST pass

#### Scenario: Test-only change passes

- **WHEN** the pull request range changes only test files, Markdown files, or
  `testdata/` content
- **THEN** the change gate MUST pass without a spec delta

### Requirement: Spec-impact escape trailer

A pull request SHALL be allowed to change behavior-bearing files without a spec
delta when at least one commit in its range carries a
`Spec-Impact: none — <reason>` Git trailer with a non-empty reason. An ASCII
`-` separator MUST be accepted in place of `—`.

#### Scenario: Trailer with reason passes

- **WHEN** a commit in the range has the trailer
  `Spec-Impact: none — dependency bump only`
- **THEN** the change gate MUST pass and report the reason

#### Scenario: Trailer without reason fails

- **WHEN** the only Spec-Impact trailer in the range is `Spec-Impact: none`
- **THEN** the change gate MUST fail and state that a reason is required

### Requirement: Completed changes are archived

The repository SHALL NOT contain an active change under `openspec/changes/`
(excluding `openspec/changes/archive/`) whose `tasks.md` has at least one task
and no unchecked task.

#### Scenario: Completed unarchived change fails

- **WHEN** an active change has every task in `tasks.md` checked
- **THEN** the archive check MUST fail and name the change

#### Scenario: In-flight change passes

- **WHEN** every active change has at least one unchecked task
- **THEN** the archive check MUST pass

### Requirement: Strict pinned validation

Continuous integration SHALL run `openspec validate --all --strict` on every
pull request using an exact, pinned OpenSpec CLI version, and SHALL fail the
pull request when validation fails.

#### Scenario: Invalid spec fails CI

- **WHEN** a pull request contains a spec or change that fails strict
  validation
- **THEN** the `specs` CI job MUST fail

#### Scenario: CLI version is pinned

- **WHEN** the `specs` CI job installs the OpenSpec CLI
- **THEN** it MUST reference an exact version rather than a range or `latest`
