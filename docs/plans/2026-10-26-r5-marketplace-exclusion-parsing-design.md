# R5: Marketplace Exclusion Runtime Guard

## Scope

Continue partial R53/F136 selection adaptation work from the frozen R5
ledger and performance track #765. Remove the remaining locator split
inside `V4Config.toLegacyConfig`'s Marketplace exclusion guard.

The measured base is `867731e54408d4e0b63a55871ad46efcc498155a`,
which already contains #948's independent kind/name-helper optimization.
This slice must not claim that earlier allocation reduction again.

## Current Behavior

Effective Marketplace exclusions reach the runtime adapter after source
binding and layer validation. An exclusion-only source needs no acquisition
lock, but overrides are restricted to the user layer. The adapter then
requires exactly three slash-separated components, rejects literal
`collection` as the middle component and literal `*` as the last component,
and retains the original locator and endpoint in the runtime exclusion.

The runtime guard is not the input parser. `ParseV4` enforces a stronger
grammar; manually constructed normalized inputs have private quirks such
as empty components and unknown kinds. Preserve those private results too.

## Change

Replace the temporary `strings.Split` slice with two `strings.Cut` calls
and a remaining-slash check. Keep the earlier layer refusal in its original
position and the existing error construction unchanged.

Do not introduce a helper, index, cache, dependency, policy, limit, or new
validation. Reusing the validating locator parser would change private
behavior and would not remove its split allocation.

## Preserved Contracts

- Public configuration acceptance, existing private-input behavior, and
  exact refusal code, message, field, line, file, and layer identity.
- Source binding, effective decision replay, first-exclusion order,
  required protection, include/exclude precedence, and re-inclusion.
- Marketplace lock admission, registration-only behavior, acquisition
  identity, runtime endpoint/locator metadata, and read-only adaptation.
- Supported v4 roots, publication boundaries, lock formats, native outputs,
  and all existing size limits.

## Evidence

Run preservation checks on both production variants before interpreting
benchmarks. Freeze the former split predicate in tests, independently of
the production guard and the stronger input parser. Exercise arbitrary
bytes through the public runtime adapter using private normalized fixtures;
exercise valid exact kinds and broad refusals through `ParseV4` as well.

Reuse #948's public Marketplace exclusion benchmark and positive Git
control. Add only refusal benchmarks, with assertions outside timing.
Compare 32 cases in six alternating pairs using frozen binaries and
identical definitions, varying only `internal/config/v4.go`.

Retain all primary timing observations. If a healthy control has a nominal
slowdown, confirm it separately with the same binaries rather than change
or silently discard the original measurements. Never pool those stages or
claim that non-significance proves latency preservation.

Check semantic mutations, byte fuzzing, focused races, allocation profiles,
compiled adapter calls, selected consumer validation, strict specs, the
terminal suite, and witnessed combined E2E. Run host adversarial
`open-code-review-delegate` across tests, documentation, raw evidence, and
private harnesses before publication and handoff.

R53 remains partial. This slice makes no admission-budget or new
compatibility decision.
