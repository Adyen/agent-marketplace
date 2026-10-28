# R5 / R53: Copy Decision History With Exact Capacity

## Scope And Boundary

Partial R53/F136 work under #765, measured independently from open
PR #967 on main `85bc15d7`. Request rebuilding preserves the complete
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
than relabeling old inputs. The final comparison uses original v4 code
from `85bc15d7`, with the same fixed module/workspace inputs on both
sides from `95320ec46b58f87b361d34f3fd6d015eed848860`.
The manifest records that dependency snapshot and baseline transform.

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
| Git includes | 1.146 ms | 1.119 ms | nonsignificant, p=0.065 | 13,261 to 13,250 |
| Marketplace exclusions only | 1.116 ms | 1.081 ms | -3.19%, p=0.009 | 5,282 to 5,272 |

Median bytes fall from 2,138,265 to 2,007,682 for Git includes
(-6.11%, about 127.5 KiB), and 866,406 to 735,938 for Marketplace
exclusions (-15.06%, about 127.4 KiB). Whole-parser YAML/pool variation
accounts for small differences from the direct 130,400-byte and
ten-allocation saving. Say about ten fewer public allocations, not an
exact ten for every shape: the Git median saves eleven in this run.
Do not infer an include-only public speed-up or CLI/acquisition latency.

Direct rebuilding at 1,024 includes improves 6.79%, saving ten
allocations and about 127.3 KiB. Exclusion-only rebuilding improves
54.58%, with allocations falling from eleven to one. These are narrower
helper measurements, not independent end-user speed-up claims.

Sixteen groups of 64 decisions improve 6.33%, with median bytes
1,058,385 to 927,985 and allocations 184 to 174. The 1,024-singleton-group
case improves 3.03%, also saving ten allocations and about 127.3 KiB.
Its frozen benchmark name ends in `control`: it controlled the rejected
bulk-only change, but exact preallocation affects it.

## Controls And Rejected Alternative

The empty helper measures 7.768 to 8.0625 ns (+3.79%, p=0.002),
an absolute increase of about 0.30 ns from the additional capacity
check. It still allocates zero bytes and objects. The direct singleton,
both single-item public cases, and the genuinely unaffected 256/1,024
effective-request cases have nonsignificant timing changes. Their
allocation counts are unchanged. Do not hide the empty-helper result
or describe every shape as faster.

The first candidate moved only the append outside the interpretation
loop. A separate 192-observation matrix shows why it is rejected:
with sixteen groups, runtime chunk-growth sizing allocates 10.75%
more bytes and takes 6.18% longer. Exact capacity removes that
regression without another pass or hidden reusable state.

All three matrices remain distinct: final fixed-dependency files are in
the evidence root, initial exact-capacity files in `pre-security/`,
and rejected files in `rejected-bulk/`. The initial exact-capacity run
has a 3.58% exclusion-only public improvement and 0.27 ns empty-helper
increase; those are historical results, not the final-head claim.
No stage replaces, filters, or pools another. Total retained
observations: 576. The baseline allocation
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
all three stages. Definitions stay in the package's decision-history
benchmark file. Each manifest pins the original base, native toolchain,
arguments, sixteen names, source/module fingerprints, binary hashes,
and ordered trial hashes.

Reconstruction verifies the literal Git baseline and both exact
production transforms, unchanged test/benchmark definitions, all
576 finite observations and units, six alternating pairs per stage,
source/binary/trial hashes, aggregate stdout bytes, and recomputed
byte-exact benchstat. Original module fingerprints remain frozen at
their recorded base; final fingerprints include both workspace modules
and `go.work`/`go.work.sum` from the explicit security snapshot.
All twelve published files match their retained originals byte for
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
