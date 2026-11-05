# R5 / R53: Copy Decision History With Exact Capacity

## Scope And Boundary

Partial R53/F136 work under #765, initially measured on `85bc15d7`
and finally refreshed on `b5f73185` after #970 and #967 merge.
Request rebuilding preserves the complete
ordered decision history as well as interpreting it into selected
requests. Previously, it appended each decision separately.

Count decisions during the existing source-name pass, reserve exact
capacity only for a nonempty history, and append each already-ordered
group before its unchanged interpretation loop. No additional pass,
cached state, dependency, policy, limit, syntax, or acquisition change.
R53, R5, and #765 remain partial.

## CI Repairs And Measurement Inputs

The first published head encounters two independent blocking failures:
GO-2026-6860 in `tools/model-catalog-audit`'s call to `html.Parse`, and
Playwright rejecting Chromium installation after `ubuntu-latest`
moves to Ubuntu 26.04. Neither is a history-copy regression or a retry.

Reuse #972's `golang.org/x/net` v0.62.0 security patch, retaining its
authorship, including required x/sys v0.49.0 and x/text v0.43.0.
Actual offline module normalization additionally requires x/sync
v0.24.0 and canonical x/tools v0.51.0 checksums. Root and E2E module
verification and zero-diff tidy pass; no new library or global setting.

Pin both PR and tag browser jobs to supported Ubuntu 24.04, retaining
every existing step and package-policy check. A workflow contract test
fails for both old `ubuntu-latest` jobs and passes for both pins.
No policy bypass, vulnerability suppression, or browser-test skip.

Actual config test dependency graphs include the changed x/sys,
x/text, and x/sync packages. Refresh the complete paired matrix rather
than relabeling old inputs. That security-adjusted comparison uses
original v4 code from `85bc15d7`, with fixed module/workspace inputs
from `95320ec46b58f87b361d34f3fd6d015eed848860`. It is now retained
in `pre-integration/`, not reused as the final-head claim.

Main then merges #970's Claude configuration-directory support and
#967's subpath optimization. Harness and fixture packages in the actual
config test dependency graph change. Refresh once after #970, retain
that complete run in `pre-subpath/`, and refresh again after #967.
The delivered comparison starts with literal v4 code from `b5f73185`,
including #967 on both sides, and dependency snapshot `3c601413`.
All final repository Go/embed dependencies and workspace metadata are
fingerprinted: 454 distinct inputs per variant. The preceding #970
matrix records 452. External modules remain bound by their checksums.

## Delivered Measurements

Native Go 1.27.3, Darwin/arm64, Apple M4 Max, CGO disabled, default
sixteen-thread benchmark suffix. Six alternating one-second pairs over
sixteen shapes retain every one of the 192 observations. Fixtures,
full result assertions, and independent interpretation are outside
`b.Loop`; public measurements include YAML parsing and normalization.
No configuration reads, directory path-metadata I/O, network, acquisition,
or harness writes occur in these timed paths.

At 1,024 decisions in one source:

| Public Parse Shape | Before | After | Timing | Median Allocations |
| --- | ---: | ---: | --- | --- |
| Git includes | 1.177 ms | 1.153 ms | nonsignificant, p=0.093 | 13,261 to 13,251 |
| Marketplace exclusions only | 1.141 ms | 1.125 ms | -1.40%, p=0.015 | 5,282 to 5,272 |

Median bytes fall from 2,138,280 to 2,007,698 for Git includes
(-6.11%, about 127.5 KiB), and 866,406.5 to 735,941 for Marketplace
exclusions (-15.06%, about 127.4 KiB). Whole-parser YAML/pool variation
accounts for small differences from the direct 130,400-byte and
ten-allocation saving. Both final public medians save ten allocations;
the earlier security-adjusted Git median saved eleven. Do not turn
that variation into an exact contract for every shape or execution.
Do not infer an include-only public speed-up or CLI/acquisition latency.

Direct rebuilding at 1,024 includes improves 8.23%, saving ten
allocations and about 127.3 KiB. Exclusion-only rebuilding improves
54.97%, with allocations falling from eleven to one. These are narrower
helper measurements, not independent end-user speed-up claims.

Sixteen groups of 64 decisions improve 7.07%, with median bytes
1,058,385 to 927,984.5 and allocations 184 to 174. The 1,024-singleton-group
case improves 3.79%, also saving ten allocations and about 127.3 KiB.
Its frozen benchmark name ends in `control`: it controlled the rejected
bulk-only change, but exact preallocation affects it.

## Controls And Rejected Alternative

The empty helper measures 7.796 to 8.119 ns (+4.14%, p=0.002),
an absolute increase of 0.323 ns from the additional capacity check.
It still allocates zero bytes and objects. The direct singleton measures
196.45 to 200.9 ns (+2.27%, p=0.013), adding 4.45 ns without changing
bytes or allocations. Both single-item public cases and the genuinely
unaffected 256/1,024 effective-request cases have nonsignificant timing
changes. Their allocation counts are unchanged. Do not hide the two
affected helper costs or describe every shape as faster.

The first candidate moved only the append outside the interpretation
loop. A separate 192-observation matrix shows why it is rejected:
with sixteen groups, runtime chunk-growth sizing allocates 10.75%
more bytes and takes 6.18% longer. Exact capacity removes that
regression without another pass or hidden reusable state.

All five matrices remain distinct: final post-#967 files are in the
evidence root, the #970-only refresh in `pre-subpath/`, the
security-adjusted refresh in `pre-integration/`, initial exact-capacity
files in `pre-security/`, and rejected files in `rejected-bulk/`.
The initial exact-capacity run
has a 3.58% exclusion-only public improvement and 0.27 ns empty-helper
increase; those are historical results, not the final-head claim.
The security-adjusted run has a 3.19% exclusion-only improvement and
0.30 ns empty-helper cost; the #970-only run has a 3.89% improvement
and a nominal 0.79% slowdown in its unaffected 256-request control.
That control slowdown does not repeat in the final matrix. All remain
historical observations, not pooled evidence for the final claim.
No stage replaces, filters, or pools another. Total retained
observations: 960. The baseline allocation
profile of direct exclusion-only rebuilding attributes the dominant
allocated space to the old per-decision append; cumulative profile
bytes are not per-call allocation or peak-memory measurements.

## Preservation

Keep lexical source-group order and every intra-group decision in
authored stream order, including exclusions, repeated locators from
different source bindings or included files, provenance, private
non-selection actions, and nil requests.

New direct tests cover nil receivers, absent/empty/allocated-empty
histories, stale output reset, include-only fallbacks, repeat rebuilds,
full selected values, original request-pointer identity, and decision
slice ownership in both directions. Nil/empty history remains nil.
Decision records are copied values; their request pointers deliberately
retain original identity rather than becoming deep copies.

The existing independent per-locator interpreter and Rapid property
check mixed include/exclude/required/re-include streams, metadata,
deterministic output, repeat rebuilds, and source-group preservation.
Both original and delivered variants pass 1,000 generated cases.
Existing include-tree tests retain cross-file replacement, runtime
adaptation, provenance, and authored-byte assertions.

New independently authored public examples execute both byte parsing
and actual file loading. They assert group order, action/locator,
source binding, alias, required protection, file/line/precedence,
original request identity, and selected output. Single-source public
examples prove that changing root decision records cannot change group
records or vice versa. Read-only consumers preserve authored bytes,
mode, modification time, and directory contents.

Six compiled semantic mutations establish non-vacuity:

| Mutation | Public Parse And File Load | Direct/Independent Model |
| --- | --- | --- |
| Reverse source-group order | fails | fails |
| Reverse each group's copied history | fails | fails |
| Omit exclusion history but retain interpretation | fails | fails |
| Alias the single-source decision slice | fails | fails |
| Clone original request pointers | fails | fails |
| Omit empty-history reset | passes | fails |

The last contract requires a reused empty config object. Fresh public
reads do not expose that precondition; do not claim a public witness.
All failures are semantic assertions, not compilation failures.
The before-overlay suite and 1,000-case property pass as well.

## Evidence And Verification

[`security-r5-decision-history-2026-10-28/`](security-r5-decision-history-2026-10-28/)
contains raw before/after output, byte-exact benchstat, and manifests for
all five stages. Definitions stay in the package's decision-history
benchmark file. Each manifest pins the original base, native toolchain,
arguments, sixteen names, source/module fingerprints, binary hashes,
and ordered trial hashes.

Reconstruction verifies the literal Git baseline and both exact
production transforms, unchanged decision-history test/benchmark definitions, all
960 finite observations and units, six alternating pairs per stage,
source/binary/trial hashes, aggregate stdout bytes, and recomputed
byte-exact benchstat. Original module fingerprints remain frozen at
their recorded base. The final two matrices additionally bind all local
Go/embed dependencies, including the changed harness and fixture
packages, to their explicit integration snapshots. Both workspace
modules and `go.work`/`go.work.sum` stay bound on both sides.
All twenty published files match their retained originals byte for
byte. No credit is compounded with earlier R5 slices.

## Initial Validation

Focused races pass with the new tests and 1,000-case property.
Offline module hygiene passes without module changes. Fresh selected
config/pipeline/resolver/TUI validation, whole-root vet, zero-issue lint,
and build pass. Strict specs pass: 68 items, 1,294 scenarios, 2,118
annotations, and two active changes. All 85 terminal journeys and
four utilities pass, with zero skips.

Combined E2E passes in 755.121 seconds: 3,508 invocations, all 114
requirements, zero gaps, all three required refusal witnesses executed
and passed, plus shellcheck. CI owns the full root suite; scoped local
packages are not full-suite proof.

Terminal QA initially encounters a machine-global package-manager
configuration error. An isolated process config and the already-cached
exact pnpm 11.20.0 executable complete frozen offline installation and
the full gate. This workaround changes no global settings,
package-manager dependency files, or registry URLs.

The initial host adversarial `open-code-review-delegate` covers thirteen files,
including default-excluded tests/docs/raw data, both JSON manifests'
values, and private measurement/control/reconstruction harnesses.
All 384 observations and source reconstruction verify, with zero
skipped files and no actionable findings. Repeat before each push
and handoff.

## Fixed-Dependency Validation

Fresh selected config/pipeline/resolver/TUI, CI workflow contracts, and
model-catalog-audit tests pass under the repaired dependencies, together
with whole-root vet, zero-issue lint, and build. Strict specs retain
68 passing items, 1,294 scenarios, 2,118 annotations, and two active
changes. Focused races with the 1,000-case property pass in 3.966 seconds;
the original history implementation with the same repaired dependencies
also passes preservation and 1,000 generated cases.

All 85 terminal journeys plus four utilities pass, zero skipped.
Fresh combined E2E passes in 1,039.110 seconds: 3,508 invocations,
all 114 requirements, zero gaps, all three required refusal witnesses
executed and passed, plus shellcheck.

Main subsequently merges #972 and its PR browser-runner pin. Integrate
that approved history, preserving the independently normalized module
metadata and tag browser pin. The resulting integration commit
`61cf8b49` has exactly the same complete Git tree as the measured and
validated `95320ec4`: no source or module-byte change is inferred away.
All 576 observations and source reconstruction still verify unchanged.
CI remains responsible for the full root suite and Linux browser gate.

On `34660392`, both bots, the full root suite, E2E, vulnerability scan,
and Web checks pass. The macOS TUI matrix leg instead reaches its
ten-minute whole-job cap near the final journey, with no failed journey
assertion. Setup and compilation take 2 minutes 50 seconds, including
a 2 minute 4 second build; the suite then runs until cancellation.
Raise the complete matrix budget to a bounded fifteen minutes, with a
workflow regression that fails on the old ten-minute budget. Preserve
every journey, individual test timeout, platform, and aggregate witness
check. This metadata-only repair changes none of the 125 measured
configuration inputs or the recorded 576 observations.

The fifteen-minute job then finishes all 85 macOS journeys successfully
(623,977.77 ms; zero failures, cancellations, or skips), including the
unchanged 3.5-second toggle-acknowledgement assertion. The whole-job cap
still cancels the subsequent Go cache archive, so calibrate the final
budget to twenty minutes, including cold setup, compilation, the full
suite, and post-job cache cleanup. Keep caching enabled and all existing
assertions and aggregate success requirements intact. The workflow
contract fails with fifteen minutes and passes with twenty.

An earlier attempt completed 84 of 85 journeys, missing that existing
acknowledgement ceiling by 106 ms. Before one targeted CI replay, run
five alternating original/optimized pairs of the isolated, unchanged
journey: all ten pass every original latency and acquisition assertion.
Retain the original failure trace; it shows correct persisted selection
and settled state. The targeted replay then passes all 85 journeys
without any per-test deadline or latency-threshold change. Its remaining
failure is the independently evidenced post-test cache budget, not an
excuse to weaken a behavioral or performance assertion.

## Final Integrated Validation

After integrating #970 and #967, fresh config, pipeline, resolver, TUI,
harness, materializer, test-sandbox, workflow-contract, and model-audit
tests pass, with whole-root vet, zero-issue lint, and build. Targeted
Claude command regressions pass too. Root and E2E offline module
verification and zero-diff tidy pass.

Strict specs pass: 69 items, 1,320 scenarios, 2,151 annotations, two
active changes. Focused races plus the 1,000-case property pass in
3.056 seconds; the literal post-#967 baseline also passes preservation
and 1,000 generated cases with the same integrated dependencies.

All 86 terminal journeys plus four utilities pass with zero skips or
failures. Combined E2E passes in 799.219 seconds: 3,515 invocations,
all 114 requirements, zero gaps, all four required witnesses executed
and passed, plus shellcheck. The added Claude-directory witness from
approved main joins the three existing refusal witnesses; none is
waived. CI still owns full-root and vendor-native verification.

All 960 observations, frozen historical snapshots, final 454-input
fingerprints, exact production transforms, history definitions, binary
and trial hashes, twenty public data files, and byte-exact benchstat
reconstruction verify unchanged before publication.
