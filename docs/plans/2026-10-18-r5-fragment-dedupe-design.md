# R5 R28: Fragment Entry Deduplication

## Scope

Start independently at main `3c13f497` while R42 PR #742 is queued.
Frozen R28/A89/F65 concerns repeated hook-entry serialization/comparison
in `artifact.MergeFragments`. The frozen ledger/source index are unchanged.
R43's JSONC patching and all new D1-D6 policies remain separate.

## Existing Contract

Opt-in object-array accumulation compares complete `encoding/json`
encodings. Object key order does not matter; nested arrays and signed
zero retain their current encoding distinctions. Existing destination
duplicates stay; new duplicates are skipped, first occurrence wins.
Empty/mixed/scalar arrays retain last-wins behavior. A flag on any fragment
opts the whole group in. Root MCP server conflicts keep their special path.

`Convert`, SDK `Materialize`, CLI aggregate publication and native
handwritten-entry retention all reach this shared merger.

## Options

- Keep pairwise comparisons: minimal code, but repeatedly encodes both
  objects for each new entry.
- Build a fresh exact-encoding set inside every append: reduces repeated
  encoding within a call, but still rescans every growing prefix when
  actual compilers contribute one hook per fragment.
- Retain an exact-encoding set per accumulated array for one JSON fragment
  group. Mirror object keys with a small state tree rather than ambiguous
  joined paths, pointer identity or hashes. Forget a subtree whenever its
  value is replaced.

Measure the third option against the original public merger. No index is
needed for non-deduplicating groups. Never mutate existing array objects,
deduplicate an initially assigned array, or share state across groups or
invocations. Serialization failures must remain unequal, as before.

## Evidence And Delivery

Before production edits, add authored state-transition cases and Rapid
comparisons with an independently pinned original merge oracle. Benchmark
public `MergeFragments` with real compiled Devin hook fragments, excluding
compilation/fixture creation and disk/network. Measure distinct, duplicate,
single-fragment and non-deduplicating controls.

Retain repeated before/after samples and benchstat; add a public allocation
guard that fails on the old scan. Exercise stale-index negative controls,
SDK/catalog/materializer/CLI consumers, focused races, strict specs and
combined E2E. Review every intended file via host OCR delegation before
push. Preserve exact output and refusal contracts with a justified
`Spec-Impact: none` trailer; add no ceiling, truncation or ownership policy.
