## Context

The model/reasoning work predates spec authority. Its claims live in
`docs/plans/2026-09-21-harness-model-catalogs-design.md` (design reference),
`pkg/harness-compilation/README.md` (catalog maintenance recipe),
`pkg/harness-compilation/CHANGELOG.md` (release notes),
`pkg/harness-compilation/diag/codes.md` (diagnostic catalog), and
`pkg/harness-compilation/field-matrix.md` (field coverage). These are leads:
each requirement below was written from current code and is bound to a passing
test, not copied from prose.

## Decisions

- **D1 — One capability.** Model and reasoning are resolved jointly, so a
  single `harness-model-catalogs` capability covers resolution, diagnostics,
  provenance, and compatibility rather than splitting by harness target.
- **D2 — User-observable scope only.** The spec covers emitted artifact
  fields, diagnostic codes/severities, ingestion output, archive payloads,
  source-revision digests, and materialization outcomes. It does not specify
  the `modelpolicy` package layout, catalog file schema details beyond what a
  failing catalog produces (`make model-catalog-check` failure), or function
  names.
- **D3 — Resource surfaces.** Requirements are keyed by skill and subagent
  surfaces, matching the shipped catalogs' `surfaces:` declarations; no claim
  is made for commands or other kinds.
- **D4 — Devin variant mappings in scope.** Enabled Devin catalog mappings
  are live data and are specified as implemented. No scenario claims live
  profile execution; acceptance-evidence provenance stays in the design
  reference.

## Discarded claims

- Benchmark figures and dates (commit-message benchstat, design-doc timing) —
  historical measurements, not behavior.
- "Gate D1 superseded by the approved evidence policy" and the
  `devin 3000.10.31` capture narrative — process history, not observable
  behavior; the enabled mappings themselves are specified.
- The promise that omission "is not described as preserving inheritance" —
  phrasing guidance for diagnostics prose, not a testable contract.
- Update-recipe steps and evidence-class taxonomy in the design reference —
  maintainer workflow, not user-observable behavior.
- Future-work notes (typed skill effort, command model fields, new resource
  primitives) — not implemented, so not specified.
- The claim that the model-catalog gate needs "no vendor executables or
  credentials" — an environment property of the test gate, not emitted
  behavior.
