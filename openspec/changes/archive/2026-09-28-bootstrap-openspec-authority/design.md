## Context

See proposal.md — Why. Current state that shapes the approach:

- `openspec/` exists with the `spec-driven` schema, an empty-template
  `config.yaml`, three main specs, and four completed but unarchived
  `agent-marketplace-pf99.*` changes that carry no spec deltas.
- `/openspec/changes/` is in `.gitignore` (commit `587bdbfa`), so deltas for
  in-flight work are invisible to review; only the pf99 changes are tracked
  because they predate the ignore rule.
- CI is a single `.github/workflows/ci.yml` whose jobs are selected by
  `tools/ci-plan` from the changed-path list. Repository tooling lives in small
  stdlib Go programs under `tools/` that the root `go.work` already covers.
- End-to-end TUI journeys are TypeScript (`tools/tui-qa/tests/*.spec.ts`) and
  web unit tests are `web/tests/unit/*.test.ts`; everything else is Go.
- `CODING_STANDARDS.md`, `README.md`, `site/docs/`, `skills/`, and review-bot
  configuration stay as they are.

## Goals / Non-Goals

**Goals:**
- Make `openspec/specs/` enforceably authoritative from the merge of this
  change onward, with an empty-or-partial spec set passing honestly.
- Give every later `baseline-<capability>` change a fixed set of conventions
  and a shared ledger so the backfill converges on deleting all legacy docs.

**Non-Goals:**
- Writing any product capability spec; that is the job of baseline changes.
- Deleting `docs/` or `pkg/harness-compilation` catalog/changelog files; each is
  deleted by the baseline that absorbs it.
- Linking OpenSpec changes to Beads or Task-SME contracts.
- Checking that annotated tests pass; the existing test jobs already fail CI
  on failing tests.

## Decisions

### D1. One tool, three checks: `tools/specaudit`

A stdlib-only Go program, structured like `tools/ci-plan`, with subcommands:

- `specaudit trace` — traceability and slug uniqueness over the whole tree.
- `specaudit archived` — no completed active change.
- `specaudit gate --base <sha> --head <sha>` — behavior-change gate and
  `Spec-Impact` trailer, using `git diff --name-only` and
  `git log --format=%(trailers:key=Spec-Impact,valueonly)`.

It parses only the Markdown headers it needs (`### Requirement:`,
`#### Scenario:`, and the `## ADDED|MODIFIED|REMOVED|RENAMED Requirements`
sections of change deltas) rather than depending on the OpenSpec CLI's JSON,
so the checks stay fast, offline, and independent of CLI output changes.

Alternatives: a shell script around `openspec show --json` (couples the gate to
Node and CLI output formats); a golangci-lint plugin (cannot see TypeScript
tests or Git history).

### D2. Annotation as a comment line, anywhere in a test file

`// spec: <capability-path>/<scenario-slug>` works unchanged in Go and
TypeScript. It is matched per line in test files, not bound to a specific
function, so it survives test renames and table-driven tests. Several
annotations may reference the same scenario and one test may carry several
annotations.

Alternative: a `Test:` line inside the scenario, rejected in Q11 because
renaming a test would force a spec edit.

### D3. Known scenarios include in-flight deltas

`trace` computes known scenarios as main specs plus ADDED/MODIFIED scenarios in
active changes, minus REMOVED requirements. A pull request that adds a scenario
and its annotated test is green before its final archive commit, and the
archive commit leaves the set unchanged.

### D4. Pinned CLI in one place

The Makefile gains `OPENSPEC_VERSION ?= 1.12.0` and a `spec-check` target that
runs `npx --yes @fission-ai/openspec@$(OPENSPEC_VERSION) validate --all --strict --no-interactive`
followed by `go run ./tools/specaudit trace` and `go run ./tools/specaudit archived`.
CI's new `specs` job calls `make spec-check`, then on `pull_request` events runs
`specaudit gate` with full history (`fetch-depth: 0`). The job runs on every
pull request, independent of `tools/ci-plan`, because it is cheap and the gate
must see every diff. 1.12.0 is the newest release and is older than the
seven-day supply-chain minimum; contributors running a different local CLI get
the same result through `make spec-check`.

### D5. Glossary and conventions live in `openspec/config.yaml`

`context:` receives the `CONTEXT.md` glossary (about 12 KB, below the
51,200-byte limit) so every generated artifact uses the domain language.
`rules:` encodes, per artifact:

- `specs`: user-observable behavior only; glossary terms only; every scenario
  needs a `// spec:` test in the same pull request.
- `design`: baseline changes include a **Discarded claims** section.
- `tasks`: include annotating/adding tests; the final task is
  `openspec archive <change> --yes` in the same pull request.

### D6. Harvest ledger is a long-lived change

A `harvest-legacy-docs` change (`skip_specs: true`) is created by this change.
Its `tasks.md` has one unchecked task per legacy source file in the ledger
below. Each baseline pull request checks off the rows it absorbed and deletes
those files; the final pull request removes any residue, repoints remaining
code comments, and archives the ledger change. Because it always has unchecked
tasks until the end, it passes `specaudit archived`, and it lives where the
workflow already lives rather than in a new document.

Alternative: the ledger inside this change's `tasks.md`, rejected because it
freezes into `archive/` when this change merges.

### D7. AGENTS.md: four sections plus a shrinking harvest appendix

Both `AGENTS.md` files are rewritten to: what the project is; the OpenSpec
workflow rule (behavior change → change with delta; specs win); build, test,
and verification commands; engineering standards via a reference to
`CODING_STANDARDS.md`. Their existing behavioral prose moves verbatim under a
final `## Unharvested leads (non-normative)` heading, removed paragraph by
paragraph by baselines. This satisfies harvest-then-delete (Q17) without
leaving current rules readable only in Git history, and makes their
non-authority explicit.

### D8. Backfill conventions every baseline change follows

- Name `baseline-<capability>`; only ADDED requirements; zero behavior diffs.
- Old docs are leads, not sources. A requirement enters only if current code
  implements it and a passing test annotated in the same pull request proves
  it; untested claims get a characterization test or are dropped.
- A doc/code conflict is resolved in favor of code; the claim is listed under
  **Discarded claims** in `design.md`, and any suspected bug becomes its own
  change.
- No dated history, bead IDs, or "was/now" notes are carried over.

### D9. Existing specs

- `embedded-harness-compilation`: every requirement is REMOVED (repository
  migration facts, not behavior).
- `marketplace-protocols`: "Test and benchmark coverage" is REMOVED; each
  remaining scenario is annotated on an existing test (for example
  `pkg/marketplace/api/openapi_test.go`) or gets a characterization test; any
  scenario that cannot be proven is added to this change as a REMOVED delta
  with its reason. The capability is renamed to `marketplace-api` later by its
  baseline, not here.
- `resource-prerequisites`: annotated in place; same fallback.
- The pf99 changes are deleted; their proposals and designs remain in Git
  history.

Discarded claims found while binding `marketplace-protocols` (each scenario is
MODIFIED to current observable behavior and keeps its title, because OpenSpec
refuses to drop scenarios from a MODIFIED requirement):

- *Required paths and schemas are present* required `/api/v1/resources` and
  `/api/v1/artifacts/{kind}/{name}`, which `api/openapi.yaml` no longer
  defines. The scenario now lists the paths and schemas the document and
  `TestOpenAPIYAMLIsValidAndContainsRoutes` actually require.
- *Consumer config fields* claimed the canonical `apiVersion` remains
  `agent-marketplace/v2`; v4 is canonical. The requirement text now states only
  the supported fields.
- *Layered config wiring* described source structure ("Config, LocalConfig, the
  v3 loader… are inspected"). It now states the observable merge rules:
  per-layer provenance and first-set `allowed_marketplaces`.
- *Archive digest resolves the self-reference problem* specified the internal
  placeholder mechanism. It now states the verification outcomes.

Follow-up GitHub issues track rebaselining these capabilities under the new
taxonomy: Adyen/agent-marketplace#393 (`marketplace-api`) and
Adyen/agent-marketplace#394 (`config-layering`).

### D10. Capability taxonomy and harvest ledger

Target capabilities, in backfill order (Q21), plus capabilities needed to
absorb remaining sources:

1. `harness-ingestion`, `harness-compilation`, `diagnostics`
2. `resource-selection` (existing `resource-prerequisites` stays separate)
3. `config-mutation`
4. `acquisition-lock`
5. `config-layering`, `config-migration`
6. `owned-entry-reconciliation`, `harness-devin`, `harness-codex`,
   `harness-claude`, `harness-gemini`, `harness-cursor`, `harness-factory`,
   `harness-windsurf`
7. `marketplace-store`, `marketplace-api`, `marketplace-operations`,
   `marketplace-web`
8. `materialization`, `machine-backup`
9. `tui-*` (split by journey when baselined)
10. `telemetry`, `logging`

Initial ledger (source → target capabilities):

| Source | Targets |
|---|---|
| `CONTEXT.md` | `openspec/config.yaml` context (this change) |
| `AGENTS.md`, `pkg/harness-compilation/AGENTS.md` appendix | per paragraph, any |
| `docs/PRECEDENCE.md` | config-layering, config-migration, resource-selection |
| `docs/adoption-guide.md` | config-layering, materialization |
| `docs/kind-scoped-selectors-plan.md`, `docs/issues/*` | resource-selection, config-mutation |
| `docs/adr/0001-marketplace-indexes-git-*`, `0004-*`, `0006-*` | marketplace-store |
| `docs/adr/0002-*`, `0005-*`, `0007-*` | marketplace-api, resource-selection |
| `docs/adr/0003-codex-*` | harness-codex, owned-entry-reconciliation |
| `docs/adr/0003-separate-nuxt-*` | marketplace-web |
| `docs/adr/0008-*` | acquisition-lock |
| `docs/adr/0001-telemetry-*`, `docs/telemetry.md` | telemetry |
| `docs/logging.md` | logging |
| `docs/marketplace-{operator,security,reverse-proxy,backup-restore}.md` | marketplace-operations |
| `docs/tui-product-contract.md` | tui-* |
| `docs/devin-cli-harness-spec-3000.3.md` | harness-devin |
| `docs/devin-verification-2026-08-04.md` | none (dated verification log; delete) |
| `docs/harness-validation/*` | matching harness-* |
| `docs/plans/*` | per plan: its subject capability (leads only) |
| `pkg/harness-compilation/diag/codes.md` | diagnostics |
| `pkg/harness-compilation/field-matrix.md` | harness-* |
| `pkg/harness-compilation/docs/plans/*`, `CHANGELOG.md` | harness-ingestion, harness-compilation (leads only) |

Baselines may split or rename capabilities; they update the ledger rows they
touch.

## Risks / Trade-offs

- [The gate is path-based and can be satisfied by an irrelevant spec edit] →
  Review remains responsible for relevance; the gate only guarantees the
  question was asked.
- [`Spec-Impact` trailers become a habit] → The reason is printed in CI output
  and visible in `git log`, so overuse is reviewable.
- [A comment-line annotation does not prove the test asserts the scenario] →
  Accepted; review checks the assertion, the tool checks existence and
  staleness.
- [The in-flight pull requests of other agents start failing the gate] →
  Announce in Atrium before merge; a trailer unblocks legitimate refactors.
- [Removing every requirement of `embedded-harness-compilation` may leave an
  empty main spec on archive] → Verify during apply and delete the empty spec
  directory in the same pull request if the CLI leaves one.

## Migration Plan

Single pull request, ordered so CI is green at every commit that touches the
gate: tooling and tests first, then annotations on existing specs, then the CI
job, then `.gitignore`, config, AGENTS rework, ledger change, deletions, and
finally `openspec archive bootstrap-openspec-authority --yes`. Rollback is a
revert of the pull request; no runtime behavior changes.
