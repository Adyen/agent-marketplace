# R5 Allocation-Free Exclusion Kind/Name Parsing

## Scope

This independent R53/F136 slice starts from main `027056e3`, including
#944 and #945. R53/R5 and performance track #765 remain partial. No ledger,
source index, admission limit, deadline, or selection policy changes.

`v4ExclusionKindName` allocates a `strings.Split` slice only to inspect its
length and return at most two components. Its callers are runtime Git/
directory selector adaptation and effective-exclusion processing, including
Marketplace adaptation.

The previous slice's allocation profile identified this remaining split.
New helper benchmarks confirm a 16/32/48/64-byte slice allocation for the
sampled shapes before changing production code. Complete public adapters,
not just this helper, are measured before and after.

## Minimal Implementation

Retain the existing single `strings.TrimSuffix(value, "/*")`. Use
`strings.Cut` to obtain the first slash-separated pair and
`strings.Contains` to reject an additional slash without constructing a
slice.

- No slash: return the trimmed input and `"*"`.
- Exactly one slash: return the two components, including empty ones.
- Additional slash: return two empty strings.

This deliberately preserves the old private parser, not a new locator
grammar. For example, `tools/skill/*` returns `("tools", "skill")`, whereas
`tools/skill/item` returns two empty strings. Existing source validation,
ordered interpretation, and Marketplace refusal rules remain authoritative.
Do not broaden accepted inputs or repair those private quirks in this slice.

## Preservation

Use manual examples plus a frozen split-based oracle for arbitrary-byte Go
fuzzing. Cover empty fields, slash counts, one suffix removal, repeated
suffixes, whitespace, case, NUL, invalid UTF-8, and long names.

Make the existing effective-exclusion replay oracle call the frozen parser
rather than the changed production helper. Otherwise a parser defect could
affect both actual and expected exclusion results.

Exercise exact and wildcard selectors through the public Git runtime
adapter for all six kinds, retaining an unrelated kind as a positive
control. Existing directory/re-inclusion/required/Marketplace witnesses
remain. Add public exclusion-only Marketplace fixtures with an unrelated
retained Git command, checking exact locator order, endpoint identity,
absence of unnecessary acquisition, and unchanged authored configuration.

Use semantic mutation overlays. Consumer-visible name/wildcard mutations
must fail public witnesses. Malformed/private-input mutations must fail
the independent parser oracle; do not invent public acceptance to make
those private-only controls look end-to-end.

## Measurement

Use six alternating before/after trials, one second per case. Compile once
per variant with identical tests/benchmark definitions; the baseline overlay
replaces only the original production file. Check source/binary identities,
all 34 case names, and all 408 rows. Do not pool three-sample exploratory
probes with final evidence.

The matrix covers six helper shapes, public Git/directory adaptation at
1/256/1,024 skills with zero/half/all exclusions, active-selection and
repeated-decision controls, wildcard adaptation, Marketplace lock hydration,
and exclusion-only Marketplace adaptation at 1/256/1,024 exact exclusions.
Setup, parsing, disk/network work, and new result assertions are outside
timing. Reused older active/lock benchmarks retain the same in-loop checks
on both variants. Report time, bytes, and allocations, not a universal
speed-up or an end-to-end I/O claim.

## Gates And Review

Run original/delivered preservation, 1,000 replay cases, bounded fuzzing,
semantic mutation controls, focused races, selected config/consumer
validation, strict specs, full terminal QA, and witnessed combined E2E.
Carry a justified `Spec-Impact: none` trailer.

Run host adversarial `open-code-review-delegate` across every own file,
including excluded tests/docs/raw evidence, before each push and handoff.
Follow actionable CodeRabbit/Gemini feedback and CI. Leave the PR open;
do not enable auto-merge, enqueue, or merge it.
