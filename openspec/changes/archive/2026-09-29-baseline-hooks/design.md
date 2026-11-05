## Context

Hook behavior was built up across the additive-hooks work, the Devin and Codex
native harness work, and the Windsurf native aggregate work. It lives in
`pkg/harness-compilation` (loaders, IR, per-target translators, event and
env-token tables) and in `internal` (support-file routing, the Factory
plugin-root expansion, output paths, ownership reconciliation, Codex trust
review, additive selection). The claims currently sit in
`docs/plans/2026-09-16-additive-hooks.md`, the `AGENTS.md` appendix "Additive
hook selection", `pkg/harness-compilation/diag/codes.md`,
`docs/harness-validation/*.md`, and
`site/docs/reference/harnesses-and-output-paths.md`. These are leads only.
Each requirement was written from current code and is bound to a test that
already exists.

## Goals / Non-Goals

**Goals:**

- One `hook-resources` capability that a later change can modify
  requirement by requirement.
- Describe today's quirks precisely enough that the follow-up change can
  replace them with MODIFIED requirements, not silent rewrites.

**Non-Goals:**

- Changing any behavior, including the quirks listed below.
- Specifying MCP, skill, or subagent behavior that shares the same
  reconciliation machinery.
- Claiming native execution. Harness compatibility gates prove configuration
  recognition at most, never that a hook fired.

## Decisions

- **D1: One cross-cutting capability.** Hooks cross ingestion, selection,
  compilation, and ownership. A `hook-resources` capability keeps the full
  contract in one place, following the precedent of `harness-model-catalogs`.
  The harvest ledger's `harness-<name>` capabilities can later refer to it
  instead of repeating hook rules.
- **D2: Quirks are specified as behavior.** The spec records these as they
  work today:
  - Claude Code, Cursor, and Factory Droid output uses the plugin-layout
    `hooks/hooks.json` inside native roots.
  - Matchers and timeouts pass through untranslated, including to Gemini CLI,
    whose timeouts are in milliseconds.
  - Plugin-root tokens are rewritten to variables that native roots do not
    set.
  - Unmappable-event severity differs per target, and Claude Code and Cursor
    do not validate events at all.
  - Only Codex reports trust review.

  The `native-hook-integration` change lists each of these as MODIFIED.
- **D3: Scenarios bind to existing tests only.** If an annotated test turns
  out not to prove a scenario as written, the scenario is corrected to match
  the code. The code and test are not changed in this change.
- **D4: Output paths are specified as a table.** Output paths are the most
  user-visible part of the contract, and one all-target test
  (`TestAdditiveHooksAllTargetsScopesAndModes`) already covers every target,
  scope, and mode.

## Risks / Trade-offs

- [A scenario overstates what its test proves] → Task 1 requires reading each
  bound test's assertions before annotating it, and narrowing the scenario if
  needed.
- [Specifying quirks makes them look intended] → The proposal and D2 name
  them, and the follow-up change modifies each one explicitly.

## Discarded claims

From `docs/plans/2026-09-16-additive-hooks.md`:

- Benchmark figures, hardware, and repetition counts: historical
  measurements, not behavior.
- "First implementation checkpoint" and "Completed implementation" narratives,
  and the verification-results log: history.
- The regression matrix and implementation sequence: test-planning guidance,
  not observable behavior.
- "No new flags, broad name redesign, general ownership migration, or new
  dependency is planned": scope statement for that work, not a contract.
- "Existing `hooks/` output directories and their portable ownership records
  are preserved" for Windsurf: not carried separately. It is covered by the
  support-directory and ownership requirements, and a dedicated scenario would
  duplicate `TestWindsurfLegacyPortableOwnershipIsPreserved` without adding a
  user-visible rule. It can be added if a later change touches the legacy
  directory.
- "Real-terminal verification" and CI-owned native inspection: gate
  procedure, not behavior.

From the `AGENTS.md` appendix "Additive hook selection":

- `catalog.Entry.SourceHarness` and `catalog.HookSupportFiles` mechanics:
  implementation names. The observable part (which siblings become support
  files) is specified.
- "`HookSupportFiles` remains unchanged for source-revision hashing, so
  supported lock manifests retain their historical superset of adjacent
  files": covered by the legacy-lock requirement. The internal hashing detail
  is not specified.
- "The policy lives in `internal/resolver` ... install-all consumes
  `HasAdditiveSources`": package layout.
- Focused E2E command lines and concurrent-E2E reporting notes: test
  procedure.
- "Fresh output follows [source] order, while shared-file reconciliation
  preserves surviving installed slots and comments; upgrading a legacy lock
  does not forcibly reorder an installed handler": only partly carried. Source
  order is specified for Windsurf ingestion. Slot preservation across a lock
  upgrade has no dedicated annotated test yet, so it is left out.

From `pkg/harness-compilation/compile/mapping/hook_events.go` comments and
`docs/harness-validation/*.md`:

- Dated "Last verified" and "probe pending" notes: evidence history.
- Statements that validation does not fire hooks: properties of the
  compatibility gate, not of Agent Marketplace output.
