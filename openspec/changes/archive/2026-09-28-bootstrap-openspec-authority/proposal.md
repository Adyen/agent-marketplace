## Why

Normative behavior is currently scattered across `AGENTS.md` (42 KB of rules
prose), `CONTEXT.md`, ADRs, 24 dated plans, harness validation notes,
`pkg/harness-compilation` changelog/catalog files, and three OpenSpec specs that
no test is bound to. Nothing tells a contributor which source wins, nothing
proves a written rule still holds, and in the last 90 days roughly a quarter of
commits were fixes that protect user workflows after the fact. Making
OpenSpec the single, test-bound authority for user-observable behavior turns
those implicit contracts into checked ones.

## What Changes

- OpenSpec specs under `openspec/specs/` become the only normative source for
  user-observable behavior; code, docs, and tests yield to them on conflict.
- `openspec/changes/` is tracked in Git again, and every change is archived as
  the final commit of the pull request that implements it.
- `CONTEXT.md` moves into `openspec/config.yaml` `context:`, and per-artifact
  `rules:` encode the backfill and traceability conventions.
- A new `tools/specaudit` Go program enforces, in CI:
  - every spec scenario is referenced by at least one `// spec:` annotation in
    a test file, and every annotation names an existing scenario;
  - behavior-bearing pull requests carry a spec delta or a
    `Spec-Impact: none — <reason>` commit trailer;
  - no completed change remains unarchived.
- CI runs `openspec validate --all --strict` with the CLI pinned to
  `@fission-ai/openspec@1.12.0`.
- `AGENTS.md` and `pkg/harness-compilation/AGENTS.md` are reduced to project
  orientation, the OpenSpec workflow rule, build/test/verification commands,
  and engineering standards (`CODING_STANDARDS.md` is kept and referenced).
  Their behavioral prose is retained only as a non-normative, shrinking
  harvest section until absorbed.
- A long-lived `harvest-legacy-docs` change is opened to hold the harvest
  ledger mapping every legacy document to its target capability.
- The four completed, unarchived `agent-marketplace-pf99.*` changes are
  deleted.
- The existing specs are brought under traceability: `embedded-harness-compilation`
  is removed (it records a completed repository migration, not user-observable
  behavior), `marketplace-protocols` loses its meta "test and benchmark
  coverage" requirement, and every remaining scenario is bound to a test or
  removed.
- **BREAKING (contributor workflow)**: pull requests that change non-test code
  under `cmd/`, `internal/`, `pkg/`, `api/`, or `web/` fail CI without a spec
  delta or the escape trailer.

## Capabilities

### New Capabilities
- `spec-governance`: the repository's contract for spec authority —
  scenario-to-test traceability, the behavior-change gate and its escape
  trailer, the archive-before-merge rule, and pinned strict validation.

### Modified Capabilities
- `embedded-harness-compilation`: all requirements removed; they describe a
  finished migration and repository layout rather than user-observable
  behavior.
- `marketplace-protocols`: the "Test and benchmark coverage" requirement is
  removed; the "OpenAPI contract", "Consumer config fields", and "Canonical
  artifact format" requirements are corrected to current observable behavior
  where they had drifted from the code (see `design.md` D9).

## Impact

- New: `tools/specaudit/` (Go, stdlib only, same style as `tools/ci-plan`),
  a `spec-check` Makefile target, and a `specs` job in
  `.github/workflows/ci.yml` that runs on every pull request.
- Changed: `openspec/config.yaml`, `.gitignore`, `AGENTS.md`,
  `pkg/harness-compilation/AGENTS.md`, existing test files that gain `// spec:`
  annotations, and tests added to characterize existing scenarios.
- Removed: `CONTEXT.md`, `openspec/changes/agent-marketplace-pf99.*`, and
  `openspec/specs/embedded-harness-compilation/`.
- Not changed: binary behavior, `README.md`, `site/docs/`,
  `CODING_STANDARDS.md`, `skills/`, review-bot configuration. Legacy `docs/`
  and `pkg/harness-compilation/{CHANGELOG.md,docs/,diag/codes.md,field-matrix.md}`
  are deleted by later baseline changes, not by this one.
