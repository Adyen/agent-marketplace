# R5 / R53: Wildcard Exclusion Validation

## Scope

Remove temporary split slices from the bounded-wildcard branches of
`validateV4Exclusion` and `validateV4ExclusionForSource`. This is partial
R53/F136 work under #765, not an admission budget or a new syntax policy.

Main `bdb3bac4` already contains #958's independent exact-locator parser
optimization. Both measured variants include it.

Keep the existing `strings` library, validators, exact-locator fallback,
typed diagnostics, ordering, and ownership/publication paths. Do not add
helpers, dependencies, limits, or a new grammar.

## Caller Boundaries

`parseV4Group` first applies general exclusion validation. Later
`normalizeV4Tree` binds the source and applies source-kind validation.

`SetUserMarketplaceExclusion` instead calls the source-kind validator
directly, before locking/reading/writing the user's config. Preserve that
path too, including wildcard names and its different refusal precedence.
Do not silently add the general parser's precheck to the editor.

Preserve private inputs: empty/unknown source kinds, the general validator
accepting `/*`, Marketplace rejecting it, kind/registry ambiguity, empty
segments, extra separators, invalid bytes, and boundedness checks before
shape parsing. Preserve even currently unreachable diagnostic branches.

## Measurement

Freeze test/benchmark definitions after baseline fixture tests and lint.
Compile separate native Go 1.27.3 binaries; only production `v4.go` changes.
Capture an actual before trial before modifying production.

Run six alternating one-second pairs over 33 cases:

- Eighteen direct general/source validator shapes, including exact and
  whole-wildcard controls, long registry names, and refusal cases.
- Six public registry-wildcard parsing cases, required/optional selections
  at 1, 256, and 1,024 distinct Marketplace registry selectors.
- Six existing qualified-wildcard public parsing cases at the same sizes.
- Three unchanged wildcard runtime-adapter controls.

Use `b.Loop`, report allocations, retain all raw observations, source and
binary fingerprints, ordered trial output hashes, and benchstat output.
Do not pool different bases or infer CLI/acquisition latency from parsing.
These Marketplace-only public fixtures include YAML and normalization,
but no acquisition, config reads, or directory path-metadata checks.
Direct validator measurements have no filesystem/network work.

Profile baseline allocation sites. Reject or separately investigate
unfavorable controls rather than selectively dropping observations.
Do not claim limits, global scaling closure, or compounded savings with
earlier R5 slices.

## Preservation

Freeze both former split-based validators as independent parsing oracles.
Shared kind/name validators and the exact fallback remain unchanged.
Compare complete typed error fields, not merely whether an error exists.
Use byte fuzzing for value, source, and source-kind strings.

Exercise real public parsing with valid controls, required protection,
all resource/source kinds, and typed refusal precedence. Exercise the
user Marketplace editor with an actual successful wildcard write, then
refusals preserving bytes, permissions, modification time, and directory
contents. Use test-owned paths and no external network.

Run semantic mutation controls through public consumers where reachable,
and explicitly identify helper-only behavior. Run before/after replay,
fuzzing, focused races, offline module hygiene, selected validation,
strict specs, terminal QA, and the combined E2E/witness gate.

## Review And Publication

Host adversarial `open-code-review-delegate` covers every own file and
private harness, including default-excluded tests/docs/raw data, before
every push and handoff. Verify source reconstruction and all observations.
Follow actionable CodeRabbit/Gemini feedback on the published head.
Ignore major-release commentary, not actual compatibility findings.

Leave the clean PR open with auto-merge disabled and no queue entry.
R53, R5, and #765 remain partial.
