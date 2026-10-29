# R5: Shared V4 Locator Parsing

## Scope

Continue partial R53/F136 and performance tracker #765 by eliminating the
temporary slash-split slice in `parseV4Locator`. Do not change the locator
language, refusal precedence, source binding, or runtime requirements.

The shared function feeds include parsing, exact exclusion validation,
source-kind-bound exclusion validation, ordinary layer-editor selection
operations, Marketplace pin requests, and Marketplace exclusions. Fix the
shared function once rather than introduce a competing parser or bypass
validation in a caller.

## Existing Contract

Check clean input first. Require exactly `kind/name` or
`registry/kind/name`. Validate kind and name, then the Marketplace name
segment, then a qualified registry slug, and finally a nonempty source.
Preserve the error text callers wrap into typed diagnostics, and return
three empty strings on every error.

Source-kind admission remains a separate, unchanged boundary. Accepting
both syntactic shapes in this helper must not permit qualified Git/directory
requests or unqualified Marketplace requests through their public readers.
Do not normalize source strings or add new private-input restrictions.

## Implementation

Use two standard-library `strings.Cut` calls and a remaining-slash check.
Use whether the second cut succeeded where the previous implementation
tested for three components. Keep all validators and their order in place.

No new helper, dependency, cache, state, limit, policy, logging path, lock
format, acquisition behavior, publication rule, or native-output change.

## Verification

Freeze the former complete split-based parser in tests, sharing only the
unchanged validators. Compare all returned fields and error results for
arbitrary bytes and source strings. Exercise public include/exclusion
diagnostics, all six kinds across three source kinds, and real editor
refusal/no-write behavior with a positive selection control.

Use semantic mutations to prove arity, registry validation, blank error
outputs, and refusal precedence are tested. Distinguish mutations caught
through public callers from those caught only by direct replay. Run both
production variants through preservation tests and native byte fuzzing,
then focused races and the consumer gates.

Measure direct parsing, whole public YAML parsing, and unchanged preparsed
adapter controls with six alternating pairs. Reuse the existing exact
exclusion/retained-command document rather than duplicate replay logic.
Fixtures contain 1, 256, or 1,024 skills, matching exact exclusions, and
one retained command. Keep setup and checks outside benchmark timing.

Relative-directory parsing includes its real path-safety metadata checks
(`Lstat`, `EvalSymlinks`, and working-directory resolution). Label that
scope accurately; it is not disk-free CPU work. No fixture acquires a
registry, reads configuration/resource payloads, or makes network requests.

Keep the initial pre-#953 matrix separate from the integrated post-#953
matrix. Preserve the original benchmark definition when correcting its
initial scope comment, retain measured test-source snapshots for untimed
lint-only fixture changes, and never relabel samples or compound #953's
saving with this parser's saving.

Measure Gemini's proposed `qualified` short-circuit separately against
the published Cut parser, not against the original Split parser. Retain
that hypothesis probe even if rejected; do not exchange a tiny
unqualified-input gain for slower qualified/refusal fixtures without
acknowledging the trade-off or imply public latency improvements from
direct-helper measurements.

Before publication and handoff, use host adversarial
`open-code-review-delegate` on every changed file, including excluded
tests, both raw datasets, provenance, and private harnesses. Run committed
specaudit with `Spec-Impact: none` because behavior is unchanged.

R53 remains partial; admission budgets and other R5 policy decisions remain
outside this slice.
