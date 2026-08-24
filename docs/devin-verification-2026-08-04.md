# D12 release certification — Devin harness target

**Work package:** D12 (Evidence, integration, E2E, and performance)

**Plan:** [`docs/plans/2026-08-03-devin-first-class-harness.md`](./plans/2026-08-03-devin-first-class-harness.md)

**Evidence snapshot:** [`docs/devin-cli-harness-spec-3000.3.md`](./devin-cli-harness-spec-3000.3.md)

**Date:** 2026-08-04

**Branch:** `devin-support` at `d608c34` — the commit this certification was
**originally written against** on 2026-08-04. It is not the tree every later
statement refers to, and the difference matters, so it is stated here rather than
left for a reader to trip over: the 2026-08-05 addenda were written against
the **working tree on top of `devin-support` @ `2d060e4`**, which is also what the
manual authenticated run captured (`internal/devinprobe/manual.go`'s
`manualEvidence2026Aug05` records `2d060e4`) and what the
[Gate re-run, 2026-08-05](#gate-re-run-2026-08-05-bead-agent-marketplace-5exj31)
was executed against. Read that as `2d060e4` **plus this bead's own uncommitted
changes**, which then land as the commit for `agent-marketplace-5exj.31` itself:
`2d060e4` is the jumping-off point, not a commit that already contains
`internal/devinprobe/manual.go`. `d608c34` and `2d060e4` are both correct
provenance for the things attributed to them; neither supersedes the other. Where
a date is given without a commit, the base is `2d060e4` for anything dated
2026-08-05 and `d608c34` for anything dated 2026-08-04.

**Certification host:** darwin/arm64, Apple M5 Pro. `devin 3000.3.27 (0becb483)`
at `/usr/local/bin/devin`. The appendix's Environment table describes the same
machine; it records the OS build string (`Darwin 25.6.0 arm64`) where this line
records the CPU, and the two are aligned explicitly there.

## How to read this document

This is a certification, not a summary. Every PASS below was established by
reading the proving code and, in most cases, by running the named test on this
tree today. Anything that was not established that way is recorded as PARTIAL,
FAIL, or UNVERIFIABLE-HERE with the missing evidence named and an owning bead.
No verdict in this document rests on a commit message or on a claim made by
another work package's documentation.

~~**Headline: the epic is NOT releasable as-is.**~~ See
[Releasability verdict](#releasability-verdict). Two things blocked it on
2026-08-04, and neither was a paperwork problem. One of the two (the red probe
gate) was cleared on 2026-08-05 by `agent-marketplace-5exj.22`; ~~the
release-gating flat-subagent symlink observation is still missing.~~ **It is no
longer missing** — it was taken on 2026-08-05 by the manual authenticated run
(see the addendum immediately below). The strikethrough is applied to the whole
sentence rather than half of it, because a half-struck sentence reads as a live
claim.

**Headline addendum, 2026-08-05 (bead `agent-marketplace-5exj.31`): both
blockers are now cleared, and the epic IS release-certified for the Devin
target.** The sentence above is kept as history because it was true when
written. Blocker 2 was cleared on 2026-08-05 by `agent-marketplace-5exj.22`.
Blocker 1 — the release-gating flat-subagent symlink observation — was cleared
on 2026-08-05 by the manual authenticated run recorded in
[Appendix: the manual authenticated subagent/model run (2026-08-05)](#appendix-the-manual-authenticated-subagentmodel-run-2026-08-05).
Criterion 15 is now PASS. ~~One PARTIAL remains (criterion 17 shortfall (a),
stale golden trees, owner `agent-marketplace-5exj.19`) and it is **not**
release-gating.~~ **No PARTIAL remains.** Criterion 17's shortfall (a) was
re-checked directly against this tree later on 2026-08-05 and is **closed**: the
exemption list no longer exists in any Go file, every target's golden tree is
compared byte for byte, and `TestGolden_AllTargets` is green for all six
targets. Criterion 17 is now PASS and the tally is 17 PASS, 0 PARTIAL. This is a
clean tally, not omniscience — the honest limits are unchanged and are listed in
[Releasability verdict](#releasability-verdict), and the true state of the E2E
suite is recorded in
[E2E suite state, 2026-08-05](#e2e-suite-state-2026-08-05-bead-agent-marketplace-5exj31).

## Gate results

**Read this table with its date.** It records the gates as run on **2026-08-04
at commit `d608c34`**. That commit does **not** contain the 2026-08-05 work
(`internal/devinprobe/manual.go`, the three new probe-suite tests, the `unrun.go`
fifth entry, or the E2E changes), so it cannot on its own support a
release-certified verdict for the current tree. The re-run that can is
[Gate re-run, 2026-08-05](#gate-re-run-2026-08-05-bead-agent-marketplace-5exj31);
the table below is kept as the 2026-08-04 record.

| Gate | Command | Result |
|---|---|---|
| Build | `go build ./...` | **PASS** (exit 0, no output) |
| Vet | `go vet ./...` | **PASS** (exit 0, no output) |
| Unit/golden tests | `go test ./...` | **PASS** (no failures in any package) |
| Lint | `make ci-lint` | **PASS** (`0 issues.`) |
| Devin runtime probes | `make devin-probe` | ~~**FAIL** — 1 of 8 executed probes failed: `TestProbeRulesAreAbsentFromDevinRoots`~~ → **PASS** as of 2026-08-05; see the addendum below |

~~`make devin-probe` exits non-zero on this tree.~~ See
[The rules probe failure](#the-rules-probe-failure).

**Addendum, 2026-08-05 (bead `agent-marketplace-5exj.22`).** `make devin-probe`
now exits 0 on the same host and the same Devin build (`devin 3000.3.27
(0becb483)`, darwin/arm64): all 8 executable probes pass. The failing probe was
not "fixed" by changing what Devin does — Devin still discovers
`.devin/rules/*.md`. It was replaced by
`TestProbeRulesDiscoveryUnderDevinRoots`, which asserts only what is observable
and now logs

```text
PROBE OBSERVED rules: .devin/rules is not advertised by `rules paths` but a rule
placed there IS discovered; rule.devin.unsupported remains a scope decision, not
a capability limit
```

and fails only if Devin starts ADVERTISING a `.devin` rules path. The
documentation it contradicted has been corrected rather than the observation.

### Gate re-run, 2026-08-05 (bead `agent-marketplace-5exj.31`)

Every gate was re-run on the **current working tree** — `devin-support` @
`2d060e4` plus this bead's own changes, i.e. the tree that actually contains
`internal/devinprobe/manual.go`, the three new probe-suite tests
(`TestRetiredProbesKeepTheirManualEvidence`,
`TestManualObservationsAreDocumented`, `TestManualAndUnrunProbesAreDisjoint`),
the fifth `UnrunProbe`, and the E2E changes, all of which land as the
`agent-marketplace-5exj.31` commit itself — on the same host and the same Devin
build (`devin 3000.3.27 (0becb483)`, darwin/arm64). A certification that declares
an epic release-certified must not cite gates from a commit that predates the
code it is certifying.

| Gate | Command | Result |
|---|---|---|
| Format | `gofmt -l internal pkg cmd` | **PASS** (no output) |
| Build | `go build ./...` | **PASS** (exit 0) |
| Vet | `go vet ./...` | **PASS** (exit 0) |
| Unit/golden tests | `go test ./...` | **PASS** (no failures in any package) |
| Lint | `make ci-lint` | **PASS** (`0 issues.`) |
| Race | `go test -race ./internal/devinprobe/... ./internal/materialize/... ./internal/ownership/... ./pkg/harness-compilation/...` | **PASS** (no failures, no data races) |
| Golden (all six targets) | `go test ./pkg/harness-compilation/ -run TestGolden -v` | **PASS** — `TestGolden_AllTargets` green for `claude`, `factory`, `cursor`, `windsurf`, `gemini`, `devin` |
| Devin runtime probes | `make devin-probe` | **PASS** — all tests green; 5 `UNRUN PROBE` lines, 5 `MANUAL OBSERVATION` lines |
| E2E, Devin cases | `go test -run 'TestE2E/devin'` (in `e2e/`) | **PASS 3/3** |
| E2E, full table | `E2E_PARALLEL=8 go test ./...` (in `e2e/`) | **NOT GREEN** — 115 total, 110 passed, 5 failed, all pre-existing and non-Devin; plus the standalone `TestInstallFromHomeDir` failure. See [E2E suite state, 2026-08-05](#e2e-suite-state-2026-08-05-bead-agent-marketplace-5exj31) |

**The race gate's scope, stated precisely.** `-race` was run on the **touched
packages and their immediate neighbours** — `internal/devinprobe`,
`internal/materialize`, `internal/ownership`, and `pkg/harness-compilation` — and
**not** on `./...`. This is a jointly-honest statement: it covers everything this
bead touched plus the packages whose concurrency the Devin target depends on, and
it is *not* a full-tree race run. Anyone reading this as "the tree is race-clean"
is reading more than it says.

**Benchmarks were NOT re-run, and that is a deliberate, jointly-argued
omission — not a silent one.** AGENTS.md requires a before/after benchmark for
any change that touches a hot path. This bead touches none. The complete set of
non-test Go changes is: comment-only edits to
`pkg/harness-compilation/compile/mapping/models.go` and
`pkg/harness-compilation/compile/mapping/tools.go` (provenance rewording; no
table value, no control flow, and no allocation changed), plus data and
documentation in `internal/devinprobe` (`manual.go`, `unrun.go`, `doc.go`) — and
`internal/devinprobe` is referenced by **no shipping code path**: it is a
local-only probe suite reached solely through `make devin-probe`. The remaining
changes are tests (`internal/devinprobe/probe_test.go`, `e2e/*_test.go`) and
documentation. No user-visible latency path was modified, so the before/after
requirement is not triggered and no benchmark delta exists to report. The
absolute numbers in [Benchmarks](#benchmarks) stand as re-measured earlier on
2026-08-05 and are **not** re-asserted here.

**The one measurement this bead did take, reported as what it is.** The
`sanitizePath` change in `e2e/framework_test.go` was measured end to end on the
full E2E table with `E2E_PARALLEL=8` on this host: **39.58 s / 38.93 s before**
versus **37.42 s / 38.76 s after**. That is **within noise on this host** — the
before and after ranges overlap — and the reason is structural: the `PATH`
directory being shadowed (`/usr/local/bin`) holds only 100 entries, so the
per-binary shadow farm has almost nothing to do. It is therefore recorded as a
**robustness fix that is not slower**. It is explicitly **not** a measured
speedup and must not be cited as a performance win.

### E2E suite state, 2026-08-05 (bead `agent-marketplace-5exj.31`)

The 2026-08-04 gate table deliberately does not list `make e2e`, and this
subsection records why in plain terms: **the E2E suite is not green on this
tree.** The 2026-08-05 re-run above now states the numbers instead of omitting
the gate: **115 cases, 110 passed, 5 failed**, plus the standalone
`TestInstallFromHomeDir`. The suite is green for every `devin/*` case, which is
what this certification's verdict rests on, and it is red for six non-Devin
things. A certification must not let a clean Devin verdict imply a clean suite.

**Two `devin/*` cases were BROKEN and are FIXED by this bead** (in
`e2e/table_test.go` and `e2e/framework_test.go`):

- `devin/project_scope_symlinks_and_aggregate_preservation` declared its registry
  in `GlobalCfg` — machine scope — while asserting repo-scope
  `/workspace/.devin/...` paths. **The product was correct and the test was
  wrong:** machine scope routes to the Devin USER root (`$HOME/.config/devin`)
  through `internal/materialize/materialize.go:499-505`, keyed on
  `Source.IsMachineScope()`, with the root itself declared as
  `UserRoot: ".config/devin"` at `pkg/harness-compilation/target/target.go:57`.
  That was established empirically rather than by reading: repointing the
  assertions at the machine root made them pass. The fix moves the registry to
  `RepoCfg`, leaving `GlobalCfg` with only `harnesses: [devin]`.
- `devin/doctor_reports_target_and_repair_is_preview_only_off_tty`
  (`e2e/table_test.go:3257`) had **two** defects, not one. An earlier revision of
  this section named only the first; that was incomplete and is corrected here.
  - **Defect 1 — a host-dependent assertion.** It asserted
    `devin cli: not installed`, which is only true when no Devin CLI is on
    `PATH`. It therefore failed on any host that HAS Devin installed — i.e. on
    exactly the developers most likely to touch this target, including this
    certification host. The fix makes the sandbox `PATH` deterministic instead of
    weakening the assertion, whose exact string is preserved: `sanitizePath`
    (`e2e/framework_test.go:124`) replaces any `PATH` directory containing a
    shadowed binary (`shadowedBinaries = []string{"devin"}`,
    `e2e/framework_test.go:69`) with a shadow directory that symlinks that
    directory's OTHER entries, so `devin` is absent by construction without
    collaterally removing co-located tools such as `git`.
  - **Defect 2 — a VACUOUS assertion, of the same class as its sibling's bug.**
    Its registry was declared in `GlobalCfg` (machine scope) while its
    aggregate-preservation assertion pointed at `/workspace/.devin/mcp_config.json`
    — a file nothing in the run ever wrote. The assertion could therefore never
    fail, whatever the product did. **This was proven rather than assumed:**
    asserting that the generated entry appeared at the machine root AND was
    absent from the repo path PASSED, which is only possible if the repo-path
    assertion had been inspecting a file that did not exist. The fix moves the
    registry to `RepoCfg` and adds a **positive** `RegularFiles` /
    `FileContains` guard on the repo aggregate that is false under machine scope,
    so regressing the scope now fails loudly instead of silently passing. A
    `_managed_by`-absent assertion and a second preview-only `doctor --repair`
    step (for idempotence) were added alongside. The fixed assertion was itself
    proven able to FAIL, by perturbing the expected content.

A new case `devin/machine_scope_routes_to_user_root`
(`e2e/table_test.go:3213`) was added to lock in the machine-scope routing that
the first bug had been hiding. **All three `devin/*` cases now pass.**

`sanitizePath` was hardened at the same time, and the hardening is now tested
rather than asserted: it raises a loud error instead of silently dropping an
unreadable `PATH` directory, builds the shadow farm **once per binary** instead
of once per test, and runs relative `PATH` entries through `filepath.Abs`. It has
a unit test, `TestSanitizePath` (`e2e/framework_test.go:220`), with six subtests.
The end-to-end wall-clock effect of the change was measured and is **within noise
on this host** — see
[Gate re-run, 2026-08-05](#gate-re-run-2026-08-05-bead-agent-marketplace-5exj31).
It is a robustness fix, not a speedup.

**Five E2E failures remain, plus one framework defect. All are pre-existing,
non-Devin, and out of scope for this bead. They are NOT fixed here:**

- `subagent/unknown_tool_passthrough`,
  `subagent/full_renormalization_end_to_end`,
  `subagent/disallowed_tools_dropped_with_warning`, and
  `subagent/isolation_dropped_with_warning` are all four **Factory**-target
  subagent cases still asserting the pre-hardening "emit the artifact and warn"
  contract. The compiler now deliberately fails closed — ERROR plus artifact
  suppression — per
  `pkg/harness-compilation/compile/factory/translator.go:206-232`,
  `pkg/harness-compilation/diag/codes.md:245-247`,
  `pkg/harness-compilation/field-matrix.md:13`, and the unit test
  `pkg/harness-compilation/compile/factory/translator_test.go:152-165`. These are
  stale TEST expectations, not product defects.
- `mcp/remove_roundtrip` (Factory MCP) also fails.
- `TestInstallFromHomeDir` fails on a framework path-mapping defect rather than a
  product one: `resolvePath` in `e2e/framework_test.go` matches `/root/` only
  with a trailing slash, so a bare `/root` leaks through to the host filesystem.

**None of the six involves the Devin target.** They are tracked separately and
they do not affect this certification's Devin verdict.

## Acceptance criteria

| # | Criterion | Verdict | Proving artifact | Gap and owner |
|---|---|---|---|---|
| 1 | `devin` accepted and validated everywhere targets are exposed, without changing defaults | **PASS** | Registry completeness: `pkg/harness-compilation/target/target_test.go:5` (`TestDefinitionsHaveUniqueCompleteIdentity`), `:29`, `:43`. Startup registration both directions: `cmd/agent-marketplace/main_test.go:67` (`TestStartupTargetRegistrationsAreValid`). Config rejection of unknown targets/`source_harness`: `internal/config/targets_test.go:47`. Defaults unchanged from all three sources that can write a harness list: `internal/config/devin_optin_test.go:25` and `:68`. TUI selection, Devin last: `internal/tui/harness_test.go:56`, `:87`. Status/doctor exposure: `cmd/agent-marketplace/status_test.go:356`, `cmd/agent-marketplace/doctor_test.go:25`. All run green today. | — |
| 2 | All five kinds compile to scope-specific canonical 3000.3+ paths, including user hooks in `config.json#hooks` | **PASS** | Compile-side canonical paths: `pkg/harness-compilation/compile/devin/translator_test.go:25` (`TestCanonicalPathConventions`), `:53` (`TestCompilerEmitsEverySelectableKind`). Hook scope envelope (project `hooks.v1.json` vs user `config.json#hooks`): `pkg/harness-compilation/compile/devin/hook_test.go:453` (`TestHookScopeSelection`). MCP same relative path in both scopes: `mcp_test.go:676` (`TestMcpScopeDoesNotMoveTheOutput`); never the pre-3000.3 layout: `mcp_test.go:710` (`TestMcpNeverEmitsThePre3000_3Layout`). Disk-side: `internal/materialize/devin_test.go:79` (`TestDevinAllFiveKindsMaterializeAtCanonicalPaths`) and `:164` (`TestDevinUserHooksPatchConfigWithoutTouchingUnrelatedSettings`), both run green today. E2E: `e2e/table_test.go:3156`. Runtime corroboration that these are the paths Devin actually reads: `devin skills paths` in `TestProbeCanonicalSkillPaths` reports `<user>/skills` and `.devin/skills`, and skills placed at both were discovered. | **Corrected 2026-08-05 (bead `agent-marketplace-5exj.31`); the previous cell contradicted criterion 15.** ~~Hook and subagent paths are **not** runtime-corroborated … See unrun probes `hook-loading-per-event`, `user-hooks-in-config-json`, `subagent-discovery-*`.~~ **Subagent paths ARE now runtime-corroborated**: the manual authenticated run of 2026-08-05T10:00:20Z loaded flat profiles from `<repo>/.devin/agents/` and `$SB/home/.config/devin/agents/` at both scopes and enumerated them from a live session (see [Appendix: the manual authenticated subagent/model run (2026-08-05)](#appendix-the-manual-authenticated-subagentmodel-run-2026-08-05)); the `subagent-discovery-*` probes no longer exist as unrun probes — they moved to `internal/devinprobe/manual.go` (`ManualObservations`). **HOOK paths remain un-corroborated**, and that half of the original cell still stands: Devin 3000.3.27 exposes no read-only hook surface, so its acceptance of `hooks.v1.json` and of `config.json#hooks` is inferred from the compile/disk contract, which is itself fully proven. The only probes this cell now references are the two that still exist: `hook-loading-per-event` and `user-hooks-in-config-json`. |
| 3 | One explicit canonical source root ingests and semantically round-trips; no ancestor/nested discovery; user roots require explicit source harness | **PASS** | `pkg/harness-compilation/ingest/devin/devin_test.go:104` (`TestLoadBundleAcceptsTheDirectRootOnlyWhenExplicitlySelected`), `:121` (`TestLoadBundleNeverMergesRoots`), `:147` (`TestLoadBundleDoesNotDiscoverNestedOrAncestorRoots`), `:193` (symlinked root), `:32`/`:89` (autodetect recognizes only workspace markers). Semantic round trip: `pkg/harness-compilation/ingest/devin/roundtrip_test.go:331` (`TestDevinSemanticRoundTripMatrix`), `:440` (determinism), `:556` (`TestDevinLossMatrix`). Autodetect precedence: `pkg/harness-compilation/ingest/autodetect_devin_test.go`. Ran the four adversarial-root tests and the matrix green today. | — |
| 4 | Versioned field-by-target matrices specify artifact, diagnostic severity/code, and skip behavior for every modeled field and event | **PASS** | `pkg/harness-compilation/field-matrix.md` carries a Devin column with no pending cells (header at `:3`). It is not prose-only: the matrices are executed by `pkg/harness-compilation/compile/devin/skill_test.go:50` and `:317`, `subagent_test.go:49`, `hook_test.go:45`, `mcp_test.go:46`. Diagnostic catalog: `pkg/harness-compilation/diag/codes.md`, with `mcp_test.go:618` (`TestMcpDiagnosticBranchCoverageIsComplete`) failing if a new `mcp.devin.*` code appears uncovered. Hook matcher event set is pinned against the evidence text at `hook_test.go:484` (`TestHookMatcherEventSetMatchesEvidence`). | — |
| 5 | Rules and every unsupported/lossy field produce documented diagnostics; canonical fields win extra conflicts with warnings | **PARTIAL → PASS (2026-08-05, bead `agent-marketplace-5exj.22`)** | Rule diagnostic: `pkg/harness-compilation/compile/devin/translator_test.go` (`TestCompilerDropsRulesAsAnOutOfScopeWarning`), plus `pkg/harness-compilation/bundle_error_contract_test.go` (`TestDevinRuleIsALossyButCleanCompile`, which runs the same rule through Devin, Claude, and Gemini so the three cannot drift) and `internal/materialize/devin_test.go` (`TestDevinRuleWarnsWithoutFailingTheRun`). Extra-field precedence: `field-matrix.md` (extras row) plus `skill_test.go`/`subagent_test.go:451`/`hook_test.go:331`/`mcp_test.go:430`,`:473` (reserved extras never passed through; conflict warning only fires for genuinely overridden fields). All documented in `diag/codes.md`. | **Closed.** The original gap was that the diagnostic's *justification* was falsified on this host: `.devin/rules/<name>.md` IS discovered by 3000.3.27. No emitted artifact was ever wrong, and rules remain an explicit plan non-goal, so the fix was to restate the rationale rather than to implement rule support. The message, the `codes.md` entry (new section "`rule.devin.unsupported` is a scope decision, not a capability limit"), the new `field-matrix.md` Rule row, the evidence snapshot's Rules section, and the plan's Rule row now all describe a scope decision and cite the probe. Severity moved ERROR → WARNING to match `rule.claude.no-primitive` and `rule.gemini.no-primitive`: the Devin ERROR family marks output that would be actively wrong if emitted, which an out-of-scope drop is not, and a scope decision must not fail an otherwise-complete sync. The probe's recorded observation and the documentation now agree. |
| 6 | Tool and permission handling never passes an unverified restriction through. On Devin only, an unrepresentable `allowed-tools` restriction is diagnosed, the whole key is omitted, and the resource is emitted with the accepted widening to Devin's default tool set; unsupported isolation or `disallowedTools` still suppresses the resource, and untyped `permissions` never passes through. | **PASS** | The amended compiler contract is pinned for skills and commands by `pkg/harness-compilation/compile/devin/skill_test.go:474` (`TestUnrepresentableAllowedToolsOmitTheKey`) and for subagents by `subagent_test.go:399` (`TestUnrepresentableSubagentToolsOmitTheKey`): each unrepresentable restriction produces a WARNING, emits the resource, and omits the entire key rather than emitting a silently narrowed partial list. `subagent_test.go:289` (`TestSubagentFailuresSuppressArtifacts`) separately proves that unsupported isolation and `disallowedTools` still produce errors and suppress the profile. `skill_test.go:270` and `:421` prove that untyped `permissions` extensions are reserved and never passed through. Bundle-level warning-and-emission coverage is at `pkg/harness-compilation/devin_bundle_test.go:315` and `:505`. The closed vocabulary remains pinned at `pkg/harness-compilation/compile/mapping/devin_test.go:65` (`TestDevinToolVocabularyIsClosed`), and diagnostics still carry no restricted values (`skill_test.go:626`, `subagent_test.go:399`). **Runtime-confirmed:** `TestProbeAllowedToolVocabulary` (`internal/devinprobe/probe_test.go:76`) recomputed the acceptance set on 3000.3.27 as exactly `[edit exec glob grep read]`; every other candidate is silently dropped by Devin, so the compiler never passes one through and instead applies the explicitly accepted omission-and-warning contract. | — |
| 7 | Command provenance and user-only trigger behavior round-trip through the stable extension | **PASS** | Forward: `pkg/harness-compilation/compile/devin/skill_test.go:317` (`TestCommandFieldMatrix`). Reverse: `pkg/harness-compilation/ingest/devin/devin_test.go:312` (`TestCommandProvenanceIsRecovered`) — the marker is consumed, not left in IR extras. Collision with a same-named skill: `devin_bundle_test.go:78`. **Runtime-confirmed** by `TestProbeCommandProvenanceExtension` (`probe_test.go:138`): 3000.3.27 accepted `x-agent-marketplace-kind: command`, reported `Triggers: user` for the converted command, and left an ordinary skill at `user, model`. | — |
| 8 | Rich Skill, hook, and MCP/OAuth semantics map across every target that can represent them | **PASS** | `field-matrix.md` is a six-column contract, not a Devin column bolted on. OAuth members are emitted by every target whose native format has them: `compile/claude/translator.go:335`, `compile/cursor/translator.go:370`, `compile/gemini/translator.go:182`, and the shared credential scanner `compile/extras.go:112`,`:119`. Typed IR: `pkg/harness-compilation/ir/ir.go:400-402`, `:426`. Per-target extension precedence: `compile/{claude,cursor,factory,gemini,windsurf}/ext_precedence_test.go`. Whole-tree `go test ./...` green. | — |
| 9 | Handwritten aggregate values, associated comments, key order, and hook order survive; whitespace normalization allowed | **PASS** | JSONC layer: `pkg/harness-compilation/jsonc/jsonc_test.go:17` (bytes round-trip exactly), `:176` (`TestSetMemberPreservesCommentsAndKeyOrder`), `:222` (`TestDeleteMemberKeepsNeighbourComments`), `:289` (`TestSetElementsKeepsHandwrittenOrderAndCommentsBeforeGenerated`). Integration: `internal/ownership/integration_test.go:29` (`TestSyncPreservesCommentsKeyOrderAndOwnership`) and `:147` (`TestHandwrittenHookOrderSurvivesAndGeneratedHooksFollow`) — both run green today. Devin aggregates are fenced from the marker-strip walk: `internal/materialize/devin_test.go:972` (`TestDevinAggregatesAreProtectedFromMarkerStripping`), `:992` (`TestDevinAggregateProtectionPreservesCommentsUnderMarkerWalk`). **Runtime-confirmed for `mcp_config.json` only:** `TestProbeMcpAggregate` (`probe_test.go:185`) fed 3000.3.27 a file with a leading `//` comment, a comment attached to an unmanaged key, and an unknown member; all three servers loaded, disabled state and headers were reported. | Comment acceptance is **not** confirmed for `hooks.v1.json` or `config.json` — see unrun probe `hook-comment-acceptance`. That result does not transfer from the MCP file. |
| 10 | Journaled generations recover every crash boundary without heuristic ownership; ownership cannot be inferred from cache contents or missing state | **PASS** | `internal/ownership/journal_crash_test.go:26` (`TestCrashInjectionAtEveryBoundary`), plus `:211`, `:261`, `:304`, `:339`, `:377`, `:396`. Fail-closed: `state_test.go:17` (`TestMissingStateFailsClosed`), `:60` (`TestCorruptStateFailsClosed`). Cache irrelevance: `state_test.go:181` (`TestCacheDeletionIsHarmless`) and `internal/materialize/devin_test.go:341`. Unforgeability is structural: `reconcile_test.go:308` (`TestOwnershipVerdictTypesCannotBeFabricated`). Ran the crash-injection, fail-closed, and cache tests green today. | — |
| 11 | `doctor --repair` implements TTY/non-TTY behavior, exact adoption, confirmed orphan removal, backups, exit codes | **PASS** | `cmd/agent-marketplace/doctor_test.go:219` (off-TTY preview-only), `:269` (`--yes` adopts without prompting), `:310` (`TestDoctorRepairYesNeverRemovesOrphans`), `:348` (per-action TTY prompts), `:411` (exit 1 on corrupt aggregate), `:443` (nothing to repair), `:477` (`--yes` without `--repair` is an error). Primitive-level: `internal/materialize/devin_repair_test.go:57`, `:111` (adoption changes no bytes), `:163` (orphan removal requires confirmation *and* evidence), `:232` (stale candidate refused). Store refuses an unconfirmed removal outright: `internal/ownership/safety_test.go:256`. Whole `doctor_test.go` suite run green today. E2E: `e2e/table_test.go:3257` (`devin/doctor_reports_target_and_repair_is_preview_only_off_tty`; it moved from `:3208` when this bead added a case above it, and its host-dependent `PATH` assumption is fixed — see [E2E suite state, 2026-08-05](#e2e-suite-state-2026-08-05-bead-agent-marketplace-5exj31)). | — |
| 12 | Invalid desired entries are skipped, valid entries/files commit independently and atomically, and **sync returns non-zero** | **PASS** | Skip-and-commit: `internal/materialize/devin_test.go:457` (`TestDevinMalformedAggregateBlocksOnlyThatFile`), `:516` (`TestDevinPartialFailureCommitsSiblingsAndPostponesPrune`), `:576`, `:246` (unmanaged key collision fails only the generated entry), `devin_bundle_test.go:991` (`TestDevinBundleValidMcpServersSurviveSiblingFailures`). **Non-zero exit (closed by `agent-marketplace-5exj.20`, completed by `agent-marketplace-5exj.23`):** the NINE `hook.devin.*` / `mcp.devin.*` codes that REMOVE an entry are all errors. `.20` escalated eight of them; `.23` closed the last skip path by splitting the shared `mcp.interpolation-unsupported` code — an unrepresentable `command` or `url` removes the entry and now reports the new ERROR `mcp.devin.interpolation-unrepresentable`, while the shared warning keeps its value-level meaning (one omitted argument, `env` value, header, or OAuth member on an entry that is still written). The exit path is proven end to end rather than by severity constant — `cmd/agent-marketplace/sync_devin_exit_test.go` (`TestSyncExitCodeForSkippedDevinAggregateEntries`) runs the real `sync` command against a git registry for both aggregates, both server forms, and every entry-removing reason, asserting a non-zero RunE error naming the code, `Errors 1` in the summary, and the surviving skill symlink and valid aggregate entry; its negative control asserts that the same unrepresentable interpolation in an `env` value still exits ZERO with the entry written. The other five targets are unaffected by construction: `pkg/harness-compilation/devin_bundle_test.go` (`TestMcpInterpolationUnsupportedIsDevinOnly`) proves Devin is the only emitter of the shared code. Prune granularity: `internal/materialize/devin_prune_scope_test.go:98` proves an erroring hook, MCP server, or subagent holds back only its own file/component while everything else prunes. | **Closed.** The five `hook.devin.*` / `mcp.devin.*` codes that still EMIT their entry stay warnings by design (inert dropped sub-field, unexpanded plugin-root token, one omitted value), and that is now the only thing warning severity means on this path. |
| 13 | Mandatory safety backups retain five raw versions per file under XDG state regardless of backup settings | **PASS** | `internal/ownership/safety_test.go:24` (`TestSafetyBackupIsMandatoryEvenWhenConfiguredBackupsAreDisabled`), `:70` (`TestSafetyBackupRetainsExactlyFiveVersionsPerFile`), `:130` (retention is per canonical output file), `:164` (mutation refused if the backup cannot be written), `:207` (malformed bytes preserved verbatim). State root honours XDG: `paths_test.go:11`, `:24`. Ran green today. | — |
| 14 | Literal-credential warnings, and no state/log/diagnostic path exposes env, header, hook payload, or OAuth secret values | **PASS** | Compile-side leak tests: `pkg/harness-compilation/compile/devin/mcp_test.go:499` (`TestMcpDiagnosticsNeverQuoteValues`, with a companion completeness guard at `:618`), `hook_test.go:370` (`TestHookDiagnosticsNeverQuotePayloads`), `subagent_test.go:420`. Ingest side: `pkg/harness-compilation/ingest/devin/devin_test.go:821`. Durable state, journal, and backup metadata: `internal/ownership/state_test.go:222` (`TestNoSensitiveValueEntersStateJournalOrBackupMetadata`), run green today. Literal-credential warning emission: `compile/extras.go:119`. | — |
| 15 | Current-version probes pass for project/user skill-directory **and flat-subagent** symlinks; failure blocks release | ~~**PARTIAL — RELEASE-GATING**~~ → **PASS (2026-08-05, bead `agent-marketplace-5exj.31`)** | Skill directory symlinks are **runtime-proven on 3000.3.27 in both scopes**: `internal/devinprobe/probe_test.go:299` (`TestProbeSymlinkProjectSkillDirectory`) and `:307` (`TestProbeSymlinkUserSkillDirectory`) both logged `PROBE PASS`, with Devin resolving the link into the cache stand-in. Compile-side subagent artifact shape supports a file symlink (`pkg/harness-compilation/compile/devin/subagent_test.go:559`, `TestSubagentArtifactShapeSupportsFileSymlink`) and the full symlink lifecycle is covered on disk (`internal/materialize/devin_test.go:716`, `TestDevinSymlinkLifecycle`) with a failed symlink an error and never a real-file write (`:1025`, `TestDevinSymlinkFailureIsAnErrorNotAFileWrite`). **Flat-subagent file symlinks are now runtime-proven on the same 3000.3.27 build (`0becb483`) in both scopes** by the manual authenticated run of 2026-08-05T10:00:20Z, recorded in full at [Appendix: the manual authenticated subagent/model run (2026-08-05)](#appendix-the-manual-authenticated-subagentmodel-run-2026-08-05): a single `agent-marketplace sync -v` installed `<repo>/.devin/agents/probe-agent-project.md` and `$SB/home/.config/devin/agents/probe-agent-user.md` as FILE symlinks into the Devin compile cache, and a Devin session enumerated exactly `probe-agent-project`, `probe-agent-user`, `subagent_explore`, `subagent_general`. Four controls rule out the cheap explanations: running outside the repo dropped only the project profile (CONTROL A, scope separation); Devin echoed each profile's description string — which exists only inside the symlink target's frontmatter (CONTROL B, the body is read through the link); renaming the PROJECT profile's cache target aside made `probe-agent-project` vanish from enumeration and restoring it brought the profile back (CONTROL C-project, causality); and **the same dangling-link control was run for the USER scope too** — renaming the user profile's cache target aside made `probe-agent-user` vanish from an enumeration taken outside the repo, leaving only `subagent_explore` and `subagent_general`, and restoring it brought all four profiles back (CONTROL C-user). The causality control is therefore **symmetric across both scopes, not project-only**. A further control, CONTROL E, confirmed on the filesystem that the compiled `allowed-tools: [read, grep]` is enforced in effect: the spawned profile was asked to overwrite `target.txt` and to run `echo ran-exec`, reported possessing only `grep`, `read`, `read_subagent`, `run_subagent`, did neither, and `target.txt` was independently verified afterwards to still contain `hello-original`. Both profiles were also spawned and returned tokens that exist only in the symlink targets' bodies. The five probes are recorded as observations in `internal/devinprobe/manual.go` (`ManualObservations`: `symlink-subagent-project`, `symlink-subagent-user`, `subagent-discovery-project`, `subagent-discovery-user`, `model-alias-list`), moved there out of `UnrunProbes`. Bundled-docs corroboration, stated accurately: `/usr/local/share/devin/docs/subagents.mdx:224-259` accepts **TWO** layouts — `agents/<name>.md` (flat file) **or** `agents/<name>/AGENT.md` (directory; `AGENTS.md`, `agent.md`, and `agents.md` are also accepted, with `AGENT.md` taking precedence) — with a frontmatter `name:` overriding the path-derived identifier in **both**. An earlier revision of this row said the layout was "exactly" the flat file; that was false and is corrected here. The compiler emits, and this run exercised, only the FLAT-FILE form; the directory form was not observed, and the frontmatter `name:` override was not exercised (the probe profiles' basenames equalled their `name`). | **Closed.** The original gap was an *inability to observe*, not a failure: 3000.3.27 exposes no read-only subagent enumeration surface (no `devin agents` subcommand, `devin skills list` omits subagent profiles, an unauthenticated ACP `session/new` reports skills and slash commands only), so `symlink-subagent-project` / `symlink-subagent-user` sat in `internal/devinprobe/unrun.go` flagged `ReleaseBlocking: true`. The enumeration surface that settled it is a running session's own `run_subagent` profile list, which exists only from *inside* an authenticated session — which is why this cannot be added to the unauthenticated suite and had to be a manual run. **Residual caveats, recorded rather than papered over:** (i) which model actually served each spawned subagent was NOT observed — Devin does not label a running subagent's model in the CLI, so `model: sonnet` is proven ACCEPTED and proven to name a live alias, not proven to have selected Claude Sonnet 5 for that run; (ii) the **VALUE** of `max-nesting` was NOT verified — only the PRESENCE of the nesting tools `read_subagent`/`run_subagent` was observed, which is consistent with any budget ≥ 1, and no nesting-depth limit was exercised; (iii) a frontmatter `name:` overriding the path-derived identifier was NOT exercised, because both probe profiles' basenames equalled their `name`; (iv) CONTROL E bounds `allowed-tools` to the profile's **own** toolset only — the profile itself noted it could in principle delegate to the write-capable builtin `subagent_general`, and nesting was not restricted, so nothing here bounds what a restricted profile can reach THROUGH nesting; (v) only the flat-file layout was exercised, not the documented `agents/<name>/AGENT.md` directory form; and (vi) Devin's bundled docs mark custom subagents **experimental** ("The format, behavior, and configuration options may change in future releases"), so this observation is version-bound to `3000.3.27 (0becb483)` and must be re-run when the floor moves. Caveats (i)-(iii) are tracked machine-readably as the FIFTH `UnrunProbe`, `subagent-frontmatter-model-and-nesting-honored` (`ReleaseBlocking: false`) — see [Still unsettled](#still-unsettled). This row therefore does **not** claim the subagent frontmatter was fully "honoured". |
| 16 | Doctor and local probes establish the 3000.3+ compatibility contract | **PASS** | Detection is total — every failure mode is a status, never an error: `internal/devincli/version_test.go:20` (`TestDetectClassifiesEveryProbeOutcome`), `:149` (runaway output bounded), `:168` (`TestDetectAgainstRealProcesses`, including the pipe-inheriting grandchild hang), `:217`. Doctor surface: `cmd/agent-marketplace/doctor_test.go:87` (`TestDoctorDevinVersionAndRecommendationLines`), run green today. Probe suite floor assertion: `internal/devinprobe/probe_test.go:18` (`TestProbeVersionMeetsFloor`) — `PROBE PASS version: devin 3000.3.27 at /usr/local/bin/devin satisfies the 3000.3 floor`. Suite isolation is structurally enforced, not conventional: `internal/devinprobe/doc.go` and `sandbox.go` (allowlisted env, `assertIsolated` before every exec, read-only commands only). | — |
| 17 | All required tests, logs, diagnostics, and benchmark evidence are present | ~~**PARTIAL**~~ → **PASS (2026-08-05, bead `agent-marketplace-5exj.31`)** | Benchmarks exist on every touched hot path and all were re-run earlier on 2026-08-05 (see [Benchmarks](#benchmarks)). Those absolute numbers stand and are **not** re-asserted by the later correction pass, which re-ran no benchmark and needed none: its only non-test Go changes are comment-only, so no hot path was touched and AGENTS.md's before/after requirement is not triggered — the reasoning is set out in [Gate re-run, 2026-08-05](#gate-re-run-2026-08-05-bead-agent-marketplace-5exj31). Diagnostics catalogued in `diag/codes.md`. Structured `slog` events on compile, mapping, aggregate, ownership, backup, and doctor paths. **Golden coverage is now complete, verified in this tree rather than taken from a CHANGELOG:** `grep -rn "staleFixtureTargets" --include='*.go' .` returns nothing — the exemption list does not exist in any Go file, surviving only as prose history at `pkg/harness-compilation/CHANGELOG.md:704`. `pkg/harness-compilation/golden_test.go:32-36` now carries the opposite contract in the test's own docstring: "No target is exempt. The shared bundle deliberately contains nothing that only one target can represent, so every compiler-backed target compiles it cleanly and every tree is compared byte for byte", with restriction-suppression behaviour relocated to the per-target subagent tests and the `testdata/matrix` conversion matrix. Every target from `targetmeta.CompilerTargets()` reaches `testutil.AssertGoldenDir(t, outDir, expectedDir)` at `golden_test.go:97`: the `result.HasErrors()` branch (`:56-70`) no longer skips the comparison but additionally asserts the per-resource contract (skipped resources are named, artifacts are still published, no artifact is published for a skipped resource) and then falls through to it, and the only remaining early return (`:91-93`) is gated on `hasFixtures == false`, so it is unreachable for any target that HAS a golden tree. The root cause was removed rather than exempted: the shared fixture `pkg/harness-compilation/testdata/resources/agents/demo-agent.md` no longer carries `disallowedTools` or `isolation` (it is now `name`, `description`, `model: sonnet`, `tools: [Read, Grep]`, `max-nesting: 2`), so the bundle no longer self-poisons. **Run today on this tree:** `go test ./pkg/harness-compilation/ -run TestGolden -v` → `--- PASS: TestGolden_AllTargets (0.04s)` with all six subtests present and passing (`claude`, `factory`, `cursor`, `windsurf`, `gemini`, `devin`), and `go test ./pkg/harness-compilation/...` is fully green. | **All three shortfalls closed.** Three were recorded on 2026-08-04. (a) ~~**Golden byte comparison is disabled for three targets.** `pkg/harness-compilation/golden_test.go:46` exempts Factory, Cursor, and Gemini from byte comparison; `:145` skips with a recorded reason. The exemption is non-vacuous (`:53`, `:142` pin the one expected lost resource) but the trees are stale. **STILL OPEN. Owner: `agent-marketplace-5exj.19`.**~~ **Closed by `agent-marketplace-5exj.19`, and verified closed in-tree on 2026-08-05.** The cited line numbers were already stale when this row last said "STILL OPEN": `golden_test.go:46` is now `t.Fatalf("CompileBundle(%s): %v", target, err)` and `:145` is `name: "hook",` inside `TestGolden_PerResourceKind` — neither is an exemption or a skip. What actually changed is recorded at `pkg/harness-compilation/CHANGELOG.md:280-299` ("Every golden tree is compared again, with no exemptions … The exemption list is deleted, and so are the per-target bundle override and the duplicated Devin fixture bundle … One genuinely stale byte surfaced, exactly as predicted: the Cursor `hooks/hooks.json` fixture was missing the `\"type\"` member"). Note that `CHANGELOG.md:513-519` is the OLDER note describing the exempt state this row used to quote — superseded history, not current state. Bead `agent-marketplace-5exj.19` is CLOSED. (b) ~~**A mapping-table provenance comment is not supportable by any evidence this suite can produce.** `compile/mapping/models.go:37`,`:47`,`:57`,`:123` say "verified on 3000.3.27 (`devin models list`)", but `devin models list` requires authentication — verified today: it exits with `Error: Not logged in.` The underlying capture *is* recorded in `docs/devin-cli-harness-spec-3000.3.md:150-160`, so the claim is probably true; the comment's implication that it is re-verifiable here is not. **Unowned — needs a new bead**~~ (The line numbers inside that struck text — `models.go:37`,`:47`,`:57`,`:123` and the spec's `:150-160` — are the 2026-08-04 tree's numbers, quoted as history and deliberately not renumbered. `models.go` was rewritten on 2026-08-05; the current rows are `:48` (`sonnet`), `:68` (`opus`), `:83` (`haiku`), `:145-167` (Pro-class `gemini-3-preview`), `:170-189` (`gemini-3-flash-preview`), and the spec's original alias table is now at `docs/devin-cli-harness-spec-3000.3.md:203-213` with the 2026-08-05 re-capture at `:217-231`.) **Closed 2026-08-05 by `agent-marketplace-5exj.31`** (which also took ownership; it was unowned when this row was written). `devin models list` was re-captured under an authenticated, isolated HOME on the same host and the same Devin build during the manual run — it succeeded and reported 28 model families with `opus` → Claude Opus 5, `sonnet`/`claude` → Claude Sonnet 5, `haiku` → Claude Haiku 4.5. The provenance comments in `compile/mapping/models.go` are reworded to cite that recorded authenticated capture (see [the 2026-08-05 capture in the evidence snapshot](./devin-cli-harness-spec-3000.3.md#authenticated-model-list-re-capture-2026-08-05)) rather than to imply the unauthenticated suite can re-verify them. (c) ~~The rules-probe evidence defect from criterion 5.~~ **Closed 2026-08-05 by `agent-marketplace-5exj.22`**: the rationale is restated as a scope decision, the severity is aligned with the Claude/Gemini rule codes, and `make devin-probe` now exits 0. ~~Criterion 17 stays **PARTIAL** solely on account of (a), which is not release-gating.~~ With (a), (b), and (c) all closed, criterion 17 is **PASS**. One scoping note so the PASS is not read as more than it is: "all required tests present" is a statement about the golden, unit, probe, and benchmark evidence this criterion enumerates. The E2E suite is a separate surface and it is **not** green — all three `devin/*` cases pass, but six non-Devin failures remain, recorded in full at [E2E suite state, 2026-08-05](#e2e-suite-state-2026-08-05-bead-agent-marketplace-5exj31). |

~~**Tally: 14 PASS, 3 PARTIAL, 0 FAIL, 0 UNVERIFIABLE-HERE**~~ (criterion 12 moved
from PARTIAL to PASS when `agent-marketplace-5exj.20` landed and is now met in
full, without a caveat, since `agent-marketplace-5exj.23` closed its last skip
path; criterion 5 moved from PARTIAL to PASS when `agent-marketplace-5exj.22`
landed on 2026-08-05). The tally itself is unchanged by `.23`: criterion 12 was
already PASS and the caveat was a residual note, not a downgrade. One of the
three remaining PARTIALs (criterion 15) is release-gating by the plan's own
words, and it is unchanged: nothing here affects the unobservable flat-subagent
symlink probes.

~~**Tally as of 2026-08-05 (bead `agent-marketplace-5exj.31`): 16 PASS, 1 PARTIAL,
0 FAIL, 0 UNVERIFIABLE-HERE.**~~ (Struck the same day, later in the pass: it
recorded criterion 17 as PARTIAL on a shortfall that had in fact already been
closed in the tree. Kept as history so the correction is visible.)

**Tally as of 2026-08-05 (bead `agent-marketplace-5exj.31`): 17 PASS, 0 PARTIAL,
0 FAIL, 0 UNVERIFIABLE-HERE.** Two moves got here today, both from PARTIAL to
PASS:

- **Criterion 15 (PARTIAL — RELEASE-GATING → PASS.)** The manual authenticated
  run of 2026-08-05T10:00:20Z observed flat-subagent file symlinks resolving at
  both scopes on the same Devin build (`3000.3.27 (0becb483)`), with three
  controls. **With criterion 15 at PASS the release gate is cleared.**
- **Criterion 17 (PARTIAL → PASS.)** Shortfall (a) — the golden-tree exemption —
  was verified closed against this tree, not accepted from a changelog:
  `staleFixtureTargets` exists in no Go file, `golden_test.go:32-36` states the
  opposite contract, every compiler target reaches the byte comparison at `:97`,
  the poisoning shared subagent fixture is cleaned, and
  `TestGolden_AllTargets` passes for all six targets including `devin`.
  Shortfall (b) was closed by this bead's authenticated `devin models list`
  re-capture and the reworded provenance comments; (c) was closed by
  `agent-marketplace-5exj.22`.

A clean tally is not omniscience, and these limits survive it unchanged. Three
frontmatter questions were NOT settled by the manual run: which model actually
served a spawned subagent, whether the **VALUE** of `max-nesting` bounds nesting
depth (only the presence of the nesting tools was observed), and whether a
frontmatter `name:` overrides the path-derived identifier (the probe profiles'
basenames equalled their `name`). Those three are now tracked machine-readably as
a **fifth** `UnrunProbe`, `subagent-frontmatter-model-and-nesting-honored`
(`ReleaseBlocking: false`), so **there are FIVE unrun probes, not four**:
`hook-loading-per-event`, `hook-comment-acceptance`,
`user-hooks-in-config-json`, `mcp-env-interpolation`, and
`subagent-frontmatter-model-and-nesting-honored`. The first four are untouched by
this bead and remain unrun; none of the five is release-gating. And Devin marks
custom subagents experimental, so criterion 15's subagent half is version-bound to
`3000.3.27 (0becb483)`. Separately, the E2E suite is not green — all three
`devin/*` cases pass, six non-Devin failures remain; see
[E2E suite state, 2026-08-05](#e2e-suite-state-2026-08-05-bead-agent-marketplace-5exj31).

## Inferred behaviors still awaiting a live 3000.3 probe

This section is the complete list of places where the implementation acts on an
assumption that a 3000.3.27 runtime probe has not settled. It merges the
machine-readable list in `internal/devinprobe/unrun.go` with the provenance
comments left in the mapping tables, the spec's own `Inferred` grades, and the
two CHANGELOGs — and it records which of them I settled today.

### Settled during this certification

These were open going into D12 and are now closed by an observation, not an
argument.

| Question | What the code assumed | What 3000.3.27 actually does | Where |
|---|---|---|---|
| Does Devin accept `triggers: []`? | `compile/devin/skill.go` emits `triggers: []` rather than omitting the key when no source trigger maps, because omission would inherit Devin's default of `user` + `model` and thereby *broaden* activation. Recorded as a D12 probe item by D4. | **Accepted, and it means what the compiler needs it to mean.** `devin skills list` renders the skill as `empty-triggers []` and `devin skills show` reports an empty `Triggers:` line, versus `user, model` for a skill with no `triggers` key and `user` for `triggers: [user]`. The fail-closed emission is correct. | Ad-hoc probe run during this certification, isolated `HOME`, three fixture skills. Not yet a committed probe — see gap below. |
| Is the `allowed-tools` vocabulary still `read/edit/grep/glob/exec` on 3000.3.27? | `compile/mapping/tools.go:18` pinned the set against 3000.2.17 and flags 3000.3.27 re-verification as required. | **Confirmed exactly, and confirmed closed.** The probe tested a 22-name candidate superset; exactly `[edit exec glob grep read]` were accepted and all 17 others were *silently dropped* by Devin. Silent dropping is why the compiler must never pass an unmapped name through. **Amended by bead `agent-marketplace-pu6b`:** this cell said "why the compiler must error rather than pass through", conflating two conclusions. The pass-through ban stands; the error does not. An unmappable or scoped entry now omits the whole `allowed-tools` key with a warning and still emits the resource, because Devin cannot represent that restriction at all (no per-command scoping, unknown names dropped) even though CONTROL E shows a representable list IS enforced. See `pkg/harness-compilation/diag/codes.md`. | `internal/devinprobe/probe_test.go:76` (`TestProbeAllowedToolVocabulary`), `PROBE PASS tools` |
| Does Devin accept the `x-agent-marketplace-kind` extension key, and does `triggers: [user]` really suppress model invocation? | Spec graded this **Inferred** at `docs/devin-cli-harness-spec-3000.3.md:102`. | **Both confirmed.** The extension key did not cause rejection, and the converted command reported `Triggers: user` with no model trigger while a sibling plain skill kept `user, model`. | `internal/devinprobe/probe_test.go:138` (`TestProbeCommandProvenanceExtension`) |
| Comment and unknown-field handling in the MCP aggregate | Spec graded this **Inferred** at `docs/devin-cli-harness-spec-3000.3.md:325`. The jsonc patcher leaves handwritten `//` comments and unknown members in place. | **Confirmed for `mcp_config.json`.** A file with a leading comment, a comment bound to an unmanaged key, and an unknown member `someUnknownDevinKey` loaded all three servers; disabled state and headers were reported correctly. | `internal/devinprobe/probe_test.go:185` (`TestProbeMcpAggregate`) |
| `env: {}` / `args: []` acceptance | The compiler emits explicit empty collections where the target can represent them (`field-matrix.md`). | **Accepted at parse time.** A stdio server carrying `"args": [], "env": {}` loaded and was returned by `devin mcp get`. Note the surface is weak: `mcp get` does not echo empty collections back, so this confirms the file parses and the server loads, not that Devin distinguishes `{}` from absent. | Ad-hoc probe run during this certification |
| ~~Does `devin models list` expose a Gemini-Pro-class alias?~~ **Moved to [Settled on 2026-08-05 by the authenticated manual run](#settled-on-2026-08-05-by-the-authenticated-manual-run)**, where the caveat below is discharged by a first-hand authenticated capture. | `pkg/harness-compilation/compile/mapping/models.go:145-167` maps `gemini-3-preview` (Pro-class) to `modelUnmappable` and `:170-189` maps `gemini-3-flash-preview` to the `gemini` alias. | **No Pro-class alias exists.** The authenticated capture recorded in the 2026-08-03/04 revision's model-alias table (`docs/devin-cli-harness-spec-3000.3.md:203-213` in the current revision, kept verbatim) lists `opus`, `sonnet`, `claude`, `haiku`, `swe`, `gemini` (→ Gemini 3.5 **Flash**), `gpt`, `codex`. The mapping is therefore correct: Flash→`gemini`, Pro→unmappable. ~~**Caveat:** I could not re-verify this today; `devin models list` requires authentication (confirmed: `Error: Not logged in.`). The verdict rests on the spec's recorded capture, not on my own observation.~~ Caveat discharged 2026-08-05. | The 2026-08-03/04 revision's `devin models list` capture, `docs/devin-cli-harness-spec-3000.3.md:203-213`; `pkg/harness-compilation/compile/mapping/models.go` |
| Are rules genuinely absent from `.devin` roots? | `compile/devin/translator.go` emitted a stable unsupported-rule ERROR whose stated reason was that Devin has no rules primitive; the probe asserted absence as its justification. | **Falsified.** `devin rules paths` advertises only `.windsurf/rules` and `.cursor/rules`, but a file written to `.devin/rules/probe-rule.md` **is** discovered and listed as `probe-rule [Devin] manual`. Reproduced independently of the suite, and re-observed on 2026-08-05. **Resolved by `agent-marketplace-5exj.22`:** the target still emits no rule, now as a documented SCOPE decision carrying a WARNING; the falsified capability claim is gone from the diagnostic, `codes.md`, `field-matrix.md`, the evidence snapshot, and the plan. | `internal/devinprobe/probe_test.go` (`TestProbeRulesDiscoveryUnderDevinRoots`); see [The rules probe failure](#the-rules-probe-failure) |

### Settled on 2026-08-05 by the authenticated manual run

Bead `agent-marketplace-5exj.31`. These five items were in **Still unsettled**
below (plus the Gemini-Pro-alias row moved down from the table above, whose
caveat these observations discharge). They are closed by observation on the same
host and the same Devin build the rest of this certification used —
`devin 3000.3.27 (0becb483)`, Darwin 25.6.0 arm64 — under an isolated but
authenticated `HOME`, whose limits are stated explicitly in the appendix rather
than implied. The full procedure, exact commands, and all nine observations are
in
[Appendix: the manual authenticated subagent/model run (2026-08-05)](#appendix-the-manual-authenticated-subagentmodel-run-2026-08-05).

| Question | What the code assumed | What 3000.3.27 actually does | Where |
|---|---|---|---|
| `symlink-subagent-project` / `symlink-subagent-user` — does Devin follow a **file** symlink at `agents/<name>.md` at both scopes? | `internal/materialize` writes `agents/<name>.md` as a file symlink into the cache with no real-file fallback (`internal/materialize/devin_test.go:1025`, `TestDevinSymlinkFailureIsAnErrorNotAFileWrite`, makes a failed symlink an error, never a file write). Last positive observation was 3000.2.17, below this target's own floor. | **It follows both.** After one `agent-marketplace sync -v` (`Installed 2, Removed 0, Malformed 0, Errors 0`, zero warnings), `<repo>/.devin/agents/probe-agent-project.md` and `$SB/home/.config/devin/agents/probe-agent-user.md` were both FILE symlinks (`test -L` true) into the Devin compile cache, and a session enumerated **both** custom profiles. **CONTROL C settles causality at BOTH scopes** (it is not project-only): renaming the PROJECT profile's cache target aside so the link dangled (`test -e` false, link itself untouched) made `probe-agent-project` disappear from enumeration, and renaming it back restored it; the same control run for the USER scope made `~/.config/devin/agents/probe-agent-user.md` dangle, after which an enumeration taken from outside the sandbox repo returned ONLY `subagent_explore` and `subagent_general` — `probe-agent-user` had vanished too — and a final enumeration from inside the repo after restoring the target returned all four again. Enumeration is driven by the symlink resolving at each scope, not by the filename or a cache. | Manual authenticated run, 2026-08-05T10:00:20Z; recorded as an observation in `internal/devinprobe/manual.go` (`ManualObservations`) |
| `subagent-discovery-project` / `subagent-discovery-user` — is a flat profile with the emitted frontmatter loaded at all, at both scopes? | That a flat profile at `agents/<name>.md` carrying the emitted `name`/`description`/`model`/`allowed-tools`/`max-nesting` frontmatter is loaded. | **Loaded at both scopes; the frontmatter is accepted, and `allowed-tools` is enforced.** From inside the sandbox repo, Devin listed exactly `probe-agent-project`, `probe-agent-user`, `subagent_explore`, `subagent_general`. **CONTROL A (scope separation):** the same prompt run from a directory OUTSIDE the repo listed only `probe-agent-user` plus the two builtins — the project profile came from `<repo>/.devin/agents/` and the user profile from `~/.config/devin/agents/`, established independently. **CONTROL B (the body is read through the link):** asked for each profile's description, Devin returned "Project-scope Devin subagent symlink probe" and "User-scope Devin subagent symlink probe", strings that exist only inside the symlink targets' frontmatter. **CONTROL C-user (causality, user scope):** dangling the user profile's cache target removed `probe-agent-user` from an enumeration taken outside the repo (only `subagent_explore` and `subagent_general` came back), and restoring the target restored all four — so causality is established for this scope too, not just the project one. Both profiles were then actually spawned and returned `PROBE-PROJECT-OK` and `PROBE-USER-OK`, tokens that exist only in the symlink targets' bodies, so the compiled system prompt reached the spawned subagent. The spawned `probe-agent-project` reported its tools as exactly `grep, read, read_subagent, run_subagent` — the compiled `allowed-tools: [read, grep]` honoured exactly, no `edit`, no `exec`. **CONTROL E then verified that restriction on the filesystem rather than trusting the self-report:** `target.txt` containing `hello-original` was created in the sandbox repo root, the profile was asked to overwrite it with `CHANGED` using any edit/write tool and then to run `echo ran-exec`, it reported possessing no write/edit and no exec tool and did neither, and `target.txt` was independently confirmed on disk afterwards to still contain `hello-original`. **Bundled docs, stated accurately:** `/usr/local/share/devin/docs/subagents.mdx:224-259` accepts **two** layouts, `agents/<name>.md` (flat file) or `agents/<name>/AGENT.md` (directory; `AGENTS.md`, `agent.md`, `agents.md` also accepted, `AGENT.md` taking precedence), with frontmatter `name:` overriding the path-derived identifier in both. The compiler emits, and this run exercised, only the flat-file form. **Not established:** which model actually served either spawned subagent (Devin does not label a running subagent's model in the CLI); that the VALUE of `max-nesting` bounds nesting depth (only the PRESENCE of the nesting tools was observed, consistent with any budget ≥ 1); that frontmatter `name:` overrides the path-derived identifier (both basenames equalled their `name`); and what a restricted profile can reach THROUGH nesting — CONTROL E showed the profile could in principle delegate to the write-capable builtin `subagent_general`, with nesting unrestricted, so `allowed-tools` bounds the profile's OWN toolset only. Those residues are the FIFTH `UnrunProbe`, `subagent-frontmatter-model-and-nesting-honored`. | Manual authenticated run, 2026-08-05T10:00:20Z; `internal/devinprobe/manual.go` (`ManualObservations`) |
| `model-alias-list` — do the aliases the mapping table depends on still exist, and does a Gemini-Pro-class alias exist? (Supersedes the Gemini-Pro row above and its caveat.) | `compile/mapping/models.go:48`,`:68`,`:83` map `sonnet`/`opus`/`haiku` and claimed runtime verification the unauthenticated suite cannot produce; `:145-167` maps Pro-class `gemini-3-preview` to `modelUnmappable` and `:170-189` maps `gemini-3-flash-preview` to the `gemini` alias. | **Re-captured first-hand under the isolated authenticated HOME.** `devin models list` succeeded and reported **28 model families**. Stable aliases present: `opus` → Claude Opus 5; `sonnet` and `claude` → Claude Sonnet 5; `haiku` → Claude Haiku 4.5; `gemini` → **Gemini 3.5 Flash**; `gpt` → GPT-5.6 Terra; `codex` → GPT-5.3-Codex; `swe` → SWE-1.7. A `gemini-3.1-pro` family **exists but carries no alias**. So the `sonnet`/`opus`/`haiku` Devin cells are confirmed still valid, and the Flash→`gemini` / Pro→`modelUnmappable` split is now **positively justified rather than assumed**. **Drift worth recording:** versus the 2026-08-03/04 revision's capture (`docs/devin-cli-harness-spec-3000.3.md:203-213`, kept verbatim; the 2026-08-05 re-capture is at `:217-231`), the alias SET is unchanged, but families behind several aliases have moved on (`swe` now SWE-1.7, `gpt` now GPT-5.6 Terra) and the family count is now 28. The aliases the mapping table depends on are exactly the ones that held. | Manual authenticated run, 2026-08-05T10:00:20Z; new dated capture in `docs/devin-cli-harness-spec-3000.3.md`; `internal/devinprobe/manual.go` (`ManualObservations`) |

### Still unsettled

The rows below are what remains: the hook-semantics questions, the two MCP
questions, and — added later on 2026-08-05 — the subagent-frontmatter residue the
manual authenticated run did **not** settle. **None of them is release-gating.**
The four hook/MCP unrun probes `hook-loading-per-event`,
`hook-comment-acceptance`, `user-hooks-in-config-json`, and
`mcp-env-interpolation` are exactly as they were on 2026-08-04; bead
`agent-marketplace-5exj.31` did not touch them. The
`symlink-subagent-*`, `subagent-discovery-*`, and `model-alias-list` rows that
stood here have moved to
[Settled on 2026-08-05 by the authenticated manual run](#settled-on-2026-08-05-by-the-authenticated-manual-run).

**`internal/devinprobe/unrun.go` now carries FIVE `UnrunProbe` entries, not
four.** The fifth,
`subagent-frontmatter-model-and-nesting-honored`, was added by this bead for a
structural reason rather than a discovered one: retiring a probe while leaving a
non-empty `Caveat` on its `ManualObservation` contradicted that field's own
documented meaning ("Empty means the retired probe's claim is fully covered"). An
uncovered residue therefore has to be a live `UnrunProbe`, not a footnote on a
retired one. It has its own row below.

**This section is the target of a cross-reference in code, so it must actually
carry what that cross-reference promises.** `unrun.go`'s doc comment previously
claimed the remaining gaps were "entirely hook loading, user-scope hook
collection, and MCP env interpolation". That was false, and the comment now says
the list is NOT exhaustive and points here for four further genuinely open items:
**`PermissionRequest` matcher acceptance**, the **default hook `type`** when the
key is absent, **`timeout` units and zero-semantics**, and the **remote MCP
transport vocabulary**. All four are rows in the table below; if one is ever
promoted to an executable probe, it should move onto `UnrunProbes` and out of
here.

| ID | What the code assumes today | What the probe would settle | Why it cannot run here | Owner |
|---|---|---|---|---|
| `hook-loading-per-event` | All eight documented events are mapped (`compile/mapping/hook_events.go:31,41,51,61`, each carrying `// Devin: bundled 3000.3 docs; 3000.3.27 probe pending.`, and the block comment at `:97`). Prompt-hook payload shape is taken from bundled docs. | That command and prompt hooks fire for each event and that the emitted rule object shape is correct. | No `devin hooks` command; a deliberately malformed `hooks.v1.json` produces **no error** from any read-only command, so "no error" is not evidence of loading. | D12 / manual |
| **`PermissionRequest` matcher acceptance** | `compile/devin/hook.go:39-52` restricts matchers to `PreToolUse` and `PostToolUse` only, and **skips** any hook with a matcher on another event. The comment at `:50` names this an explicit 3000.3.27 probe item. This is the fail-closed choice: if Devin ignored a matcher, the hook would fire on every occurrence instead of the author's subset. | Whether `PermissionRequest` also honours a matcher. If it does, the target is currently *over*-rejecting representable hooks. | Same absence of a hook surface. | D12 / manual. Note the current behavior is safe but possibly needlessly narrow — this probe can only *widen* what compiles. |
| **Default hook `type` when absent** | The compiler never omits `type`: an unrecognized or absent type is never guessed, because guessing `command` would execute a payload never declared executable (`compile/devin/hook.go`). | What Devin does with a hook object that has no `type`. Settles whether the ingester should infer a default when reading a handwritten hook. | Same. | D12 / manual |
| **`timeout` units and zero-semantics** | The compiler passes `timeout` through verbatim when non-negative and skips the entry when negative (`compile/devin/hook.go:157-167`), because a negative value "could plausibly be read as unbounded". **Zero is passed through verbatim on an argument, not on an observation:** the comment at `compile/devin/hook.go:163-164` reasons that "an explicit 0 is preserved verbatim: it is the strictest value the author can express, so passing it through can only narrow" — which is sound *only if* `0` does not mean "unbounded" to Devin, and that is precisely what is unsettled. Spec grades units and defaults **Inferred** at `docs/devin-cli-harness-spec-3000.3.md:264`. | Whether `timeout` is seconds or milliseconds, and whether `0` means "immediate", "unbounded", or "use the default". If `0` means unbounded, the compiler is currently able to emit an unbounded hook from a source that asked for an immediate one. | Same. | D12 / manual. **This is the most consequential unsettled item after the symlink gate** — it is a security-relevant execution bound whose semantics are unknown. |
| `hook-comment-acceptance` / `user-hooks-in-config-json` | The jsonc patcher leaves handwritten comments in `hooks.v1.json` and `config.json` and patches only the `hooks` member. | That Devin still loads both files after the patch, and that unrelated `config.json` settings keep working. | Same. Comment acceptance is confirmed for `mcp_config.json` only, and does not transfer. | D12 / manual |
| **Remote MCP transport vocabulary** | `compile/devin/mcp.go:54-57` treats `{http, sse}` as the closed documented set and **skips** an entry with any other transport (`mcp.devin.transport-unsupported`), because dropping the field would silently fall back to Devin's Streamable HTTP default and have the agent speak an unauthorized protocol. **The literal string `sse` is never documented** — `docs/devin-cli-harness-spec-3000.3.md:305` shows only `"transport": "http"`, and `:316` is prose ("Remote servers default to Streamable HTTP and may fall back to SSE"). `sse` is an inference from that prose. | Whether `sse` is a valid literal transport value, and whether the set is exactly two. | **Attempted today and found unobservable.** I loaded a fixture with `transport` values `http`, `sse`, `websocket`, `definitely-not-real`, and absent. **All five servers loaded and `devin mcp get` echoed none of them** — `mcp list`/`get` never print the transport field. Acceptance is therefore indistinguishable from silent ignore on the read-only surface; even a garbage transport "loads". Settling this needs a live connection to a real remote server. | D12 / manual, authenticated, with a real endpoint |
| `mcp-env-interpolation` | `compile/mapping/mcp.go:29-30` and `:43` document `${env:VAR}` interpolation for env values, headers, and `oauthClientSecret`, both marked `3000.3.27 probe pending`. `${file:PATH}` is treated as Windsurf-only and unmappable. | That `${env:VAR}` is expanded at server launch. | Expansion happens at launch, which needs an authenticated session and would execute a third-party process. `devin mcp get` **redacts** env and header values (confirmed today: `Env: PROBE_SETTING=<redacted>`). | D12 / manual |
| **`subagent-frontmatter-model-and-nesting-honored`** — the FIFTH unrun probe, added 2026-08-05 by bead `agent-marketplace-5exj.31`, `ReleaseBlocking: false` | The compiler emits `model:` and `max-nesting:` on every Devin subagent artifact and relies on Devin's bundled docs for the frontmatter `name:` override (`/usr/local/share/devin/docs/subagents.mdx:224-259`). The 2026-08-05 manual authenticated run observed the frontmatter being **accepted and parsed**, and separately proved `allowed-tools` is **enforced** (CONTROL E, verified on disk) — but not the three claims here. | (1) **Which model actually serves a spawned custom subagent** — i.e. that `model:` is honoured at *inference* time, not merely accepted at parse time. (2) That the **VALUE** of `max-nesting` bounds nesting *depth*, rather than merely enabling nesting at all. (3) That a frontmatter `name:` **overrides** the path-derived identifier. | Devin 3000.3.27 **does not label a running subagent's model anywhere in the CLI**, so the serving model cannot be read off any surface. For nesting, the manual run observed only that the nesting tools `read_subagent` and `run_subagent` were **PRESENT**, which is consistent with any budget ≥ 1; no nesting-depth limit was exercised. For `name:`, the run used profiles whose file basename **equalled** their frontmatter `name`, so nothing in the capture distinguishes a frontmatter-derived identifier from a path-derived one. | D12 / manual, authenticated. Full three-part procedure is recorded in `internal/devinprobe/unrun.go`. **Why it exists at all:** retiring a probe while keeping a non-empty `Caveat` on its `ManualObservation` contradicted that field's own documented meaning, so an uncovered residue must be a live `UnrunProbe` rather than a footnote. |

### Gaps in the probe suite itself

Two things I established today are **not** yet committed probes, because D12's
file ownership for this task is limited to documentation and CHANGELOGs:

- The `triggers: []` acceptance result and the `env: {}` / `args: []` result
  were obtained by ad-hoc shell invocations, not by a test in
  `internal/devinprobe/`. They should become probes so they cannot silently
  regress. **Unowned — needs a new bead.**
- `TestProbeMcpAggregate` does not currently assert that a transport value is
  *rejected* when undocumented, and as shown above it could not, because the
  read-only surface never echoes transport. The suite should record that
  limitation the way `unrun.go` records the others, rather than leaving the
  reader to assume `TestProbeMcpAggregate` covered it. **Unowned — needs a new
  bead.**

## The rules probe failure

**RESOLVED 2026-08-05 by bead `agent-marketplace-5exj.22`. The section below is
the original 2026-08-04 finding, kept verbatim because the observation it
records is still true of Devin 3000.3.27; only the conclusion changed. See
[Resolution](#resolution-2026-08-05) at the end of this section.**

`make devin-probe` exits non-zero on this tree. One executed probe fails (the
line numbers in this block and in the paragraph below are from the **2026-08-04**
tree at `d608c34`, where `TestProbeRulesAreAbsentFromDevinRoots` still existed;
they are quoted history and deliberately not renumbered — the probe was replaced
by `TestProbeRulesDiscoveryUnderDevinRoots`, now at
`internal/devinprobe/probe_test.go:272`):

```text
--- FAIL: TestProbeRulesAreAbsentFromDevinRoots (0.61s)
    probe_test.go:266: a rule under .devin/rules was discovered; the target
        must stop emitting an unsupported diagnostic:
        Available Rules
          probe-rule [Devin] manual
```

I reproduced this outside the suite, in a clean isolated `HOME` with a throwaway
git repo, to rule out a sandbox artifact:

- `devin rules paths` advertises only `.windsurf/rules/*.md (always-on)` and
  `.cursor/rules/*.md (conditional)`. It does **not** mention `.devin/rules`.
- With no rule present, `devin rules list` prints `No rules found.`
- After writing `.devin/rules/probe-rule.md`, `devin rules list` prints
  `probe-rule [Devin] manual`.

So Devin 3000.3.27 reads a canonical `.devin/rules` path that its own
documentation surface does not advertise.

**This is not, by itself, a release blocker,** and I want to be precise about
why. Marketplace-selectable rules are an explicit plan non-goal (line 28), and
the target's behavior — refuse to emit a rule, with a stable diagnostic — is
unchanged and still correct as a *product* decision. Nothing the target writes
is wrong.

What is wrong is the **recorded evidence**. The probe's docstring
(`probe_test.go:248-253`) states that the absence of a `.devin` rules path is
"the runtime justification for the compiler's stable unsupported-rule
diagnostic". That justification is false. A future reader would conclude the
diagnostic is grounded in a capability Devin lacks, when in fact Devin has the
capability and the target declines to use it. The probe must be rewritten to
assert what is actually true and intended (the target emits no rule artifact,
regardless of whether Devin could read one), and the unsupported-rule rationale
in `diag/codes.md` and the field matrix must be restated as a scope decision
rather than a capability limit.

Until that is done, `make devin-probe` — which the plan lists as an integrated
quality gate — is red, and a red gate cannot be waved through in a
certification.

### Resolution (2026-08-05)

Bead `agent-marketplace-5exj.22` closed this. Both branches its acceptance
criteria offered were considered, and restating won:

- **Implementing Devin rule emission was rejected.** Rules are
  marketplace-selectable generally; Devin is the deliberate target-specific
  exception. Its compiler intentionally emits the `rule.devin.unsupported`
  warning and no artifact. This decision is not based on a belief that Devin
  cannot read Rules: the runtime evidence below shows that it can. Implementing
  emission would also have meant materializing into `.devin/rules`, a path Devin
  does not advertise — the exact undocumented dependency this target refuses
  elsewhere (`mcp.devin.transport-unsupported`,
  `ingest.devin.tool-unreversible`).
- **What *did* rest on the falsified claim** was the evidence snapshot's Rules
  section, which said "there is no canonical `.devin/rules` project path … the
  Devin target *therefore* emits an explicit unsupported diagnostic". That
  "therefore" is a non-sequitur on 3000.3.27 and has been replaced with the
  observation and the real reason.
- **The severity changed with it.** `rule.devin.unsupported` was an ERROR, which
  under the per-resource contract suppresses the resource and makes `sync` exit
  non-zero. Every other Devin ERROR marks output that would be actively wrong if
  emitted: a broadened privilege, an unenforced restriction, a silent transport
  fallback, a shadowed built-in. An out-of-scope rule is none of those — nothing
  is written, nothing is at risk — and it is the same class of loss that Claude
  and Gemini report as WARNINGS (`rule.claude.no-primitive`,
  `rule.gemini.no-primitive`). It is now a warning too. Devin is in fact the
  *least* lossy of the three, since a user can place the file by hand and Devin
  will read it, so an error there while Claude warns was backwards.

`make devin-probe` was re-run on the same host and the same Devin build on
2026-08-05 and exits 0; `TestProbeRulesDiscoveryUnderDevinRoots` logs
`PROBE OBSERVED rules` and fails only if Devin starts advertising a `.devin`
rules path. Criterion 5 is PASS.

## Benchmarks

### Recorded by each work package

Collected from the commit messages on `origin/main..HEAD`
(`git log --format=%B origin/main..HEAD`). Several packages recorded a delta or
a qualitative statement rather than absolute numbers; those are reproduced as
written rather than invented.

| Package | Commit | Recorded benchmark evidence |
|---|---|---|
| D1 | `6bfb440` | Registry-derived benchmark fixtures added; no numbers recorded |
| D2 | `70bdadc`, `0a5e7c8` | Benchmark coverage added; no numbers recorded |
| D3 | `05eb115` | Mapping lookups remain allocation-free: **5.06 ns geomean** across the new per-table sub-benchmarks |
| D4 | `13b17f9` | New translation path baseline: **1.1 µs / 41 allocs** minimal skill, **10.3 µs / 223 allocs** rich skill, **2.4 µs / 77 allocs** minimal command |
| D5 | `0edc81a` | **1.6 µs / 55 allocs** minimal, **9.0 µs / 167 allocs** rich, **1.1–2.0 µs** on the failure paths. Shared `fields.go` extraction left D4's numbers unchanged and allocation-identical |
| D6 | `ef0a59c` | **0.8–1.2 µs** per hook; **24 µs** to merge and collapse eight fragments |
| D7 | `ac53798` | No numbers recorded (work package was credential-hygiene focused) |
| D9 | `c651827` | No numbers recorded |
| D10 | `b235f72` | Non-Devin allocation counts flat: **11 of 15** materialization benchmarks byte-identical across all samples; **2** gain exactly one alloc from a single `TargetInfo` lookup. Measurement-driven fix: backing up before every parse made an idempotent reconcile cost **34 ms**; moving the backup to the malformed-parse branch cut it to **644 µs** |
| D11 | `d608c34` | New benchmarks added (`BenchmarkRunDoctor`, `BenchmarkBuildHarnessTargetRows`, `BenchmarkDevinRepairPreview`, `BenchmarkDetect`); no numbers recorded |
| `.17` | `b55d2fb` | Bundle compilation not slower: **−2.5% sec/op geomean**, allocations unchanged, **+0.1% B/op** for the skipped-resource slice. A `TestMain` now silences the default log handler so before/after are measured like for like |
| `.13` | `b319602` | No numbers recorded |
| hcc dup-key | `f5b3a70` | Fragment merging classifies and validates in one token pass: **−38.8% sec/op, −26.5% B/op, −68.7% allocs/op** geomean over `BenchmarkMergeFragments` (n=5, benchstat) |

### Re-verified on the current tree

`go test -run '^$' -bench <pattern> -benchtime 2s -count 5 -benchmem`, darwin/arm64,
Apple M5 Pro. Ranges are min–max across the five samples.

**Bundle compile — `./pkg/harness-compilation/`**

| Benchmark | ns/op | B/op | allocs/op |
|---|---|---|---|
| `CompileBundleLarge/100_skills_claude` | 1,022,748 – 1,142,201 | 1,851,645 – 1,851,997 | 22,174 – 22,175 |
| `CompileBundleLarge/50_mixed_factory` | 347,604 – 391,670 | 686,697 – 686,740 | 7,373 |
| `CompileBundleLarge/50_mixed_cursor` | 357,317 – 384,905 | 674,139 – 674,178 | 7,290 |
| `CompileBundleViaHCC/20_skills_factory` | 129,810 – 139,941 | 272,782 – 272,783 | 2,730 |
| `CompileBundleViaHCC/50_skills_claude` | 483,362 – 545,807 | 913,436 – 913,653 | 11,116 – 11,117 |
| `CompileBundleRichFields` | 460,718 – 497,853 | 1,060,770 – 1,060,790 | 9,313 |
| `CompileBundlePartialSkips/50_skills_1_bad_subagent_cursor` | 331,367 – 349,433 | 670,070 – 670,093 | 6,599 |
| `CompileBundlePartialSkips/50_skills_10_bad_subagents_cursor` | 413,018 – 438,503 | 775,786 – 775,802 | 7,687 |

**Materialize — `./internal/materialize/`**

| Benchmark | ns/op | B/op | allocs/op |
|---|---|---|---|
| `DevinAggregateReconcileFirstRun` | 85,333,232 – 97,819,144 | 486,352 – 490,925 | 1,486 – 1,489 |
| `DevinAggregateReconcileIdempotent` | 639,855 – 806,941 | 745,944 – 746,158 | 8,373 – 8,374 |
| `DevinHookIdentity` | 2,903 – 3,077 | 4,320 | 76 |
| `DevinRepairPreview/8_entries` | 226,561 – 267,177 | 206,943 – 207,066 | 2,477 |
| `DevinRepairPreview/64_entries` | 953,143 – 1,052,431 | 1,508,067 – 1,508,121 | 17,805 – 17,806 |

**Doctor / repair — `./cmd/agent-marketplace/`**

| Benchmark | ns/op | B/op | allocs/op |
|---|---|---|---|
| `RunDoctor` (before `.21`) | 36,696,371 – 39,204,201 | 164,148 – 164,623 | 783 – 785 |
| `RunDoctor` (after `.21`) | 20,083,820 – 22,543,478 | 104,532 – 105,360 | 666 – 668 |
| `BuildHarnessTargetRows` | 1,872 – 2,033 | 3,144 | 29 |

### Regression analysis

**No regression against any recorded baseline.**

- **D10's headline number holds.** `DevinAggregateReconcileIdempotent` was
  recorded at **644 µs** after the safety-backup placement fix; it measures
  **640–807 µs** today, with four of five samples in the 639–807 µs band and a
  low of 639,855 ns. The fix has not been eroded.
- **`.17`'s bundle-compile claim holds.** All eight `CompileBundle*`
  sub-benchmarks show allocation counts stable to within one allocation across
  all five samples, which is the property `.17` claimed ("allocations
  unchanged"). There is no absolute ns/op baseline in the commit message to
  compare against, only a geomean delta, so I make no ns/op regression claim
  here — that would be a fabricated comparison.
- **`.21` is confirmed by measurement, not just asserted.** `BenchmarkRunDoctor`
  costs **36.7–39.2 ms** per invocation while allocating only ~164 kB / 784
  allocs. A CPU-bound Go path doing 784 allocations does not take 37 ms; the
  time is process-fork latency. `internal/config/loader.go:39` forks
  `git rev-parse --show-toplevel`, and repeated repo-root lookups are the
  documented cause. The bead's "~36 ms of doctor latency" figure is accurate.
  **`.21` has since been fixed and re-measured.** The git boundary is memoized
  per working directory, so `doctor` forks git once instead of three times
  (`status` once instead of six, `list` once instead of seven). benchstat over
  the same command, n=5: `RunDoctor` **37.23 ms → 20.32 ms (−45.41%,
  p=0.008)**, 160.3 KiB → 102.4 KiB (−36.11%), 784 → 667 allocs (−14.92%). The
  residual ~20 ms is the one remaining fork, which the new
  `BenchmarkFindRepoRootCold` measures directly at **18.5 ms/op** — confirming
  the diagnosis that the whole cost was process spawning. `BenchmarkRunDoctor`
  clears the memo per iteration, so the post-fix number is still one full
  process's worth of work and is not a warm-cache artefact.

**One unbaselined outlier worth flagging, not a regression:**
`DevinAggregateReconcileFirstRun` costs **85–98 ms** while allocating only
~490 kB / ~1,488 allocs. As with doctor, the allocation profile rules out CPU
work; this is the journaled-generation write path — mandatory safety backup,
journal fsync, staged rename, ownership commit, journal cleanup — and fsync
dominates. That is the *correct* cost of the durability guarantee D9 was built
to provide, and it is a first-run-only cost (the idempotent path is 130× faster
at ~0.64 ms). No prior work package recorded a baseline for it, so I cannot call
it a regression or an improvement. I record it here so the next person has one.

## Open beads that gate or shadow release

| Bead | What it is | What it blocks | Release impact |
|---|---|---|---|
| `agent-marketplace-5exj.18` | ~~`internal/materialize`'s write-back path (`stripManagedEntriesFromFile`) decodes and re-marshals a user's harness JSON with `encoding/json`, destroying comments and key order and **silently collapsing duplicate keys**.~~ **CLOSED after this certification was written.** The write-back parses through `pkg/harness-compilation/jsonc` (`internal/materialize/materialize.go:2137` (`stripManagedEntriesFromFile`), parsing at `:2149`). A duplicate key now fails closed with the same `json.duplicate-key` `*diag.CodedError` the read-only loaders emit and nothing is written; comments, key order, and untouched members' exact bytes survive a successful prune. `internal/materialize/materialize_writeback_test.go` proves the destination is byte-identical after a refusal, that no staging residue is left, and that the five marker-merge targets are all still on this path. | Correctness of aggregate write-back for the five non-Devin targets. | **Resolved.** The two-tier contrast is now a difference in *ownership evidence* only (Devin: durable journaled state; the other five: an in-file `_managed_by` marker), not a difference in JSON safety. Devin's fence is unchanged: `internal/materialize/devin.go:6-16` and `internal/materialize/devin_test.go:972` (`TestDevinAggregatesAreProtectedFromMarkerStripping`)/`:992` (`TestDevinAggregateProtectionPreservesCommentsUnderMarkerWalk`) still hold, and `TestMarkerMergeTargetsAreNotAggregateProtected` pins that Devin is the only protected target. |
| `agent-marketplace-5exj.19` | ~~The Factory, Cursor, and Gemini golden trees are exempt from byte comparison and have known stale content.~~ **CLOSED after this certification was written.** The shared bundle's subagent no longer carries restrictions only Claude can represent, so every compiler target compiles it cleanly, the exemption list is deleted, and all six trees are compared byte for byte. Devin's per-target bundle override and `testdata/resources-devin/` are deleted with it. The predicted stale Cursor hook byte was real: `hooks/hooks.json` was missing the `"type": "command"` member `compile/cursor/translator.go:316` emits unconditionally. **Re-verified in-tree on 2026-08-05 (bead `agent-marketplace-5exj.31`), because criterion 17(a) still claimed this bead was open and the two statements could not both be true:** `staleFixtureTargets` appears in no Go file, `golden_test.go:32-36` states the no-exemption contract, every target reaches `AssertGoldenDir` at `:97`, and `TestGolden_AllTargets` passes for `claude`, `factory`, `cursor`, `windsurf`, `gemini`, and `devin`. The contradiction is resolved in favour of the tree: **(a) is closed**, and criterion 17 is PASS. | — | **Resolved and verified.** Criterion 17's "all required tests present" is no longer weakened by an exemption list, and criterion 17 no longer carries a PARTIAL on account of this bead. |
| `agent-marketplace-5exj.20` | ~~Devin hook and MCP entry-skip diagnostics are **warnings**, so a skipped aggregate entry does not make `sync` exit non-zero.~~ **CLOSED after this certification was written.** The eight entry-skip codes are errors; the four that still emit their entry stay warnings. | **Acceptance criterion 12**, second half: "sync returns non-zero". | **Resolved.** The prune caveat was handled first rather than accepted: `internal/materialize`'s prune block is now keyed by (output key, output COMPONENT), and Devin hooks and MCP servers write to shared aggregate FILES rather than component directories, so an erroring hook suppresses pruning of `hooks.v1.json` alone and `.devin/skills/` and `.devin/agents/` still prune. `harnesscompile.Materialize`'s `PruneSuppressed` remains bundle-wide and untouched: it governs the one-shot `convert`/`hcc` path, not `sync`. |
| `agent-marketplace-5exj.21` | ~~Repo-root lookup forks `git rev-parse --show-toplevel` (`internal/config/loader.go:41`) repeatedly, costing ~36 ms of doctor latency.~~ **CLOSED after this certification was written.** The git boundary is memoized per working directory in `internal/config/loader.go`, so it is resolved at most once per process: instrumenting the probe and running each command gives `doctor` 1 fork (was 3), `status` 1 (was 6), `sync --check` 1 (was 5), `list` 1 (was 7), TUI startup 1 (was 3). Only the git boundary is memoized — the `.agents/config.yaml` walk still runs per call, because `install --scope repo` moves that boundary mid-process (`internal/pipeline/install.go:98-151`). Out-of-repo behaviour is unchanged and covered by `TestFindRepoRootOutsideGitRepoIsUnchanged`; the memo is mutex-guarded and race-clean. | Interactive latency of `doctor`, and by extension anything else on that path. | **Resolved.** benchstat n=5: `RunDoctor` **37.23 ms → 20.32 ms (−45.41%, p=0.008)**, −36.11% B/op, −14.92% allocs/op. The residual is the single remaining fork, measured directly by the new `BenchmarkFindRepoRootCold` at 18.5 ms/op. |
| `agent-marketplace-5exj.23` | ~~`mcp.interpolation-unsupported` is a WARNING that nonetheless SKIPS a Devin MCP entry when the unrepresentable value is the server's `command` or `url`, so that sync exits 0.~~ **CLOSED after this certification was written.** The code was carrying two different outcomes; it was split by outcome rather than escalated. The entry-removing case is the new ERROR `mcp.devin.interpolation-unrepresentable`; the shared code keeps warning severity and the value-level meaning. | **Acceptance criterion 12**, the one remaining skip path that exited 0. | **Resolved.** Escalating the shared code was rejected on its merits, not only for cross-target reasons: per-resource suppression means any error diagnostic drops all of a resource's artifacts, so escalation would have deleted an entire MCP server because one `env` value used an unmappable style. A per-target severity mechanism was rejected as new machinery in a catalogue whose value is that a code means one thing. The other five targets are unchanged by construction — Devin is the only emitter of `mcp.interpolation-unsupported`, pinned by `TestMcpInterpolationUnsupportedIsDevinOnly`. |
| `agent-marketplace-5exj.22` | ~~`rule.devin.unsupported` is a stable ERROR justified by the claim that Devin has no rule primitive, which the D12 probe falsified: `.devin/rules/<name>.md` IS discovered on 3000.3.27.~~ **CLOSED after this certification was written.** The rationale is restated as a scope decision in the diagnostic message and hint, `diag/codes.md` (new dedicated section), a new `field-matrix.md` Rule row, the evidence snapshot's Rules section, and the plan's resource-mapping row; each cites the probe. Rule support was deliberately NOT implemented — the plan's non-goal is about the marketplace selection model (`internal/config.AllKinds` has no rule kind for any harness), so it does not rest on the falsified claim, and `.devin/rules` is unadvertised. Severity moved ERROR → WARNING to match `rule.claude.no-primitive`/`rule.gemini.no-primitive`. | **Acceptance criterion 5**, and the red `make devin-probe` gate (Blocker 2). | **Resolved.** `make devin-probe` exits 0 on the certification host with the same Devin build; criterion 5 is PASS; Blocker 2 is cleared. Blocker 1 (criterion 15) was untouched **by this bead** — it was cleared separately, later on 2026-08-05, by the manual authenticated run under `agent-marketplace-5exj.31`. Nothing here holds the release. |

**Every bead in this table is now closed** — the heading is kept because it names
what the table was for on 2026-08-04. As of 2026-08-05,
`agent-marketplace-5exj.31` is the **last open child of the epic
`agent-marketplace-5exj`**, so closing it closes the epic.

## Releasability verdict

~~**The epic is not releasable as-is.**~~ Two things blocked it as of
2026-08-04. I am stating both without hedging because a false pass here is worse
than a recorded gap. **Blocker 2 was cleared on 2026-08-05** by
`agent-marketplace-5exj.22`; ~~Blocker 1 still holds the release.~~

### Verdict addendum, 2026-08-05 (bead `agent-marketplace-5exj.31`)

**The epic is now release-certified for the Devin target.** The struck sentence
above is kept because it was the correct verdict on 2026-08-04 and the reasoning
behind it is the reason this document can now say otherwise: the gate was held
open until it was *observed*, not until it was argued away.

- **Blocker 2** — the red `make devin-probe` gate — was cleared on 2026-08-05 by
  `agent-marketplace-5exj.22`.
- **Blocker 1** — acceptance criterion 15's flat-subagent symlink half — was
  cleared on 2026-08-05 by the manual authenticated run recorded in
  [Appendix: the manual authenticated subagent/model run (2026-08-05)](#appendix-the-manual-authenticated-subagentmodel-run-2026-08-05).
  Devin `3000.3.27 (0becb483)` — the same build the rest of this certification
  used, on the same host — follows a **file** symlink at `agents/<name>.md` at
  both the project and the user scope, loads the compiled frontmatter through the
  link, and will spawn the resulting profile. Four controls rule out the
  filename, a cache, and context-guessing as the cause: scope separation
  (CONTROL A), the description read through the link (CONTROL B), and a
  dangled-then-restored cache target run for **both** scopes (CONTROL C-project
  and CONTROL C-user). A fifth, CONTROL E, confirmed on the filesystem that the
  compiled `allowed-tools` restriction is enforced in effect. Criterion 15 is
  **PASS**. What was *not* shown is set out in the four-item list below.

~~Criteria now stand at **16 PASS, 1 PARTIAL, 0 FAIL, 0 UNVERIFIABLE-HERE**. The
sole remaining PARTIAL is criterion 17, PARTIAL only because of shortfall (a) —
the stale Factory/Cursor/Gemini golden trees — which is **tracked by
`agent-marketplace-5exj.19` and is not release-gating**.~~ Criteria now stand at
**17 PASS, 0 PARTIAL, 0 FAIL, 0 UNVERIFIABLE-HERE**. Criterion 17 moved to PASS
later on 2026-08-05: shortfall (a) was verified closed against this tree — the
exemption list is gone from every Go file, `golden_test.go:32-36` states the
no-exemption contract, every compiler target reaches the byte comparison at
`:97`, the poisoning shared subagent fixture is cleaned, and
`TestGolden_AllTargets` passes for all six targets including `devin`. Shortfall
(b), the unsupportable `devin models list` provenance comment, was closed by this
bead with a first-hand authenticated re-capture; (c) by
`agent-marketplace-5exj.22`.

**There is therefore no PARTIAL criterion and no open blocker for the Devin
target, and `agent-marketplace-5exj.31` is the last open child of the epic.** A
clean tally is not omniscience, and this verdict is deliberately narrower than
the tally. What it covers is the Devin target on this host and this Devin build;
what it does not cover is set out immediately below and must be read as part of
it.

Four things this verdict deliberately does **not** claim, because they were not
observed:

1. **Which model actually served the spawned subagents.** Devin does not label a
   running subagent's model in the CLI. `model: sonnet` is proven to be accepted
   and proven to name a live alias (`sonnet` → Claude Sonnet 5 in the 2026-08-05
   authenticated capture); it is *not* proven that Claude Sonnet 5 served either
   run.
2. **Anything about the four hook/MCP unrun probes.** `hook-loading-per-event`,
   `hook-comment-acceptance`, `user-hooks-in-config-json`, and
   `mcp-env-interpolation` are untouched by this bead and remain unrun. There is
   also a **fifth** unrun probe,
   `subagent-frontmatter-model-and-nesting-honored`, added by this bead for the
   frontmatter residue the manual run did not settle. None of the five is
   release-gating; see [What does not block release](#what-does-not-block-release)
   and [Still unsettled](#still-unsettled).
3. **That the subagent frontmatter was fully "honoured".** It was not shown to
   be. `allowed-tools` **is** enforced (CONTROL E confirmed it on the
   filesystem), but the serving `model:`, the **VALUE** of `max-nesting`, and the
   frontmatter `name:` override were all NOT established — see the fifth unrun
   probe.
4. **That the E2E suite is green.** It is not. All three `devin/*` cases pass —
   two of them were broken and are fixed by this bead — but five non-Devin E2E
   cases (four Factory subagent cases and `mcp/remove_roundtrip`) and one
   framework path-mapping defect (`TestInstallFromHomeDir`) still fail. They are
   pre-existing, tracked separately, involve no Devin code path, and are **not**
   fixed here; see
   [E2E suite state, 2026-08-05](#e2e-suite-state-2026-08-05-bead-agent-marketplace-5exj31).
   They do not affect this verdict, which is about the Devin target.

Finally, Devin's own bundled docs mark custom subagents **experimental** ("The
format, behavior, and configuration options may change in future releases",
`/usr/local/share/devin/docs/subagents.mdx:224-259`). This certification is
therefore version-bound to `3000.3.27 (0becb483)`: when the compatibility floor
moves, criterion 15's subagent half must be re-observed by repeating the appendix
procedure.

### Blocker 1 — Acceptance criterion 15 is unmet for flat-subagent symlinks — CLEARED 2026-08-05

The plan is unambiguous (line 61): *"Skills use directory symlinks and flat
subagents use file symlinks. Both shapes are release requirements: 3000.3 probes
must pass for project and user scopes before the target ships."* Criterion 15
repeats it: *"failure blocks release."*

Half of that gate is satisfied and I verified it myself. **Skill directory
symlinks pass on 3000.3.27 in both scopes**: `TestProbeSymlinkProjectSkillDirectory`
and `TestProbeSymlinkUserSkillDirectory` both logged `PROBE PASS`, with Devin
resolving the link into a cache stand-in and rendering the skill body.

**The flat-subagent half did not run.** Devin 3000.3.27 has no read-only
subagent enumeration surface at all — no `devin agents` subcommand,
`devin skills list` omits subagent profiles, and an unauthenticated ACP
`session/new` reports only skills and slash commands. The two probes are
enumerated as release-gating in `internal/devinprobe/unrun.go` and printed on
every suite run.

A gate that could not be evaluated is not a gate that passed. The last positive
observation of subagent file-symlink following was on **3000.2.17** — a version
below this target's own compatibility floor. And the consequence of being wrong
is not cosmetic: the plan forbids a real-file fallback, so if 3000.3.27 does not
follow the link, every Devin subagent a user syncs is silently invisible to the
agent, with no error anywhere. `internal/materialize` will happily create the
symlink and report success.

**A `devin` binary IS installed on this host** (`devin 3000.3.27 (0becb483)` at
`/usr/local/bin/devin`), so the blocker is *not* "no binary available" — the
skill probes prove the harness works end to end. The blocker is that the
subagent probes require an **authenticated interactive session**, which the
probe suite is correctly forbidden from performing. Clearing it requires a human
to run the manual procedure recorded in `unrun.go`: log in under an isolated
`HOME`, materialize a Devin subagent through `agent-marketplace sync`, confirm
`.devin/agents/<name>.md` is a symlink, start a session, and confirm the profile
is offered. That is one person, once, and it either clears the gate or stops a
silent-failure ship.

*Cleared (2026-08-05, bead `agent-marketplace-5exj.31`):* one person did exactly
that, once. The procedure above was run at 2026-08-05T10:00:20Z on the same host
and the same Devin build, and it cleared the gate rather than stopping the ship.
The missing ingredient was never the binary or the login — it was that isolation
and authentication had to coexist. Devin keeps its credentials at
`$HOME/.local/share/devin/credentials.toml`, **not** under `$HOME/.config/devin`,
so copying that one file into a `mktemp -d` sandbox `HOME` yields an
authenticated session whose config, cache, and agent-marketplace state are all
still fully isolated. Nothing read or wrote the real `~/.config/devin`,
`~/.cache/agent-marketplace`, or `~/.agents`. The enumeration surface that
finally answered the question is a running session's own `run_subagent` profile
list — it exists from *inside* a session, not from a read-only subcommand, which
is exactly why this could not be added to the unauthenticated probe suite and had
to be a manual run. Devin followed the file symlink at both scopes, read the
compiled frontmatter through the link, honoured `allowed-tools: [read, grep]`
exactly (and CONTROL E confirmed that restriction on disk), and spawned both
profiles. The full record — with the four controls, the dangling-link control run
at BOTH scopes, the `allowed-tools` filesystem check, the stated limits of the
isolation, and every command — is in
[Appendix: the manual authenticated subagent/model run (2026-08-05)](#appendix-the-manual-authenticated-subagentmodel-run-2026-08-05).
The feared outcome — a silently invisible subagent with no error anywhere — did
not occur.

### Blocker 2 — An integrated quality gate is red — CLEARED 2026-08-05

*Original finding (2026-08-04):* `make devin-probe` exits non-zero.
`TestProbeRulesAreAbsentFromDevinRoots` fails because Devin 3000.3.27 discovers
`.devin/rules/*.md`, contradicting the recorded justification for the
unsupported-rule diagnostic. I reproduced this independently.

The *behavior* is fine — refusing to emit rules is an explicit plan non-goal, so
nothing the target writes is wrong. The *evidence* is not fine, and the plan
lists "version-gated local Devin 3000.3+ probes" among its integrated quality
gates. Shipping with a red probe suite and a docstring that asserts a falsehood
about the harness would leave the next reader with an evidence base they cannot
trust. This is a small, well-understood fix — rewrite the probe to assert the
scope decision instead of the capability claim, and restate the rationale in
`diag/codes.md` and `field-matrix.md` — but it must be done before release, not
after.

*Cleared:* bead `agent-marketplace-5exj.22` did exactly that. The probe asserts
the observable behaviour, the rationale is restated as a scope decision
everywhere it appeared, the severity was realigned with the Claude and Gemini
rule codes, and `make devin-probe` re-run on the same host and Devin build on
2026-08-05 exits 0 with all 8 executable probes passing. ~~Blocker 1 is
unaffected and still holds the release.~~ **Struck 2026-08-05 (bead
`agent-marketplace-5exj.31`): it was true of `.22`'s scope but is no longer true
of the release.** `.22` did indeed not touch Blocker 1; Blocker 1 was cleared
separately, later the same day, by the manual authenticated run — see
[Blocker 1 — Acceptance criterion 15 is unmet for flat-subagent symlinks — CLEARED 2026-08-05](#blocker-1--acceptance-criterion-15-is-unmet-for-flat-subagent-symlinks--cleared-2026-08-05).
**No blocker holds the release.**

### What does not block release

For completeness, and so nothing here is read as more alarming than it is:

- `.18`, `.19`, and `.21` are real but do not gate this epic; all three are now
  closed. See the bead table.
- `.20` is closed: a Devin sync that skips a hook or MCP entry now exits
  non-zero, and prune suppression was narrowed per component first so the
  escalation costs no unrelated pruning.
- `.23` is closed, and with it the last residual visibility gap. The shared
  `mcp.interpolation-unsupported` code was being used for two outcomes at once,
  which was the actual defect; it was SPLIT rather than escalated. The
  entry-removing case (an unrepresentable `command` or `url`) is the new ERROR
  `mcp.devin.interpolation-unrepresentable`; the shared code stays a warning and
  now only ever means one omitted value on an entry that was still written.
  Escalating it instead would have deleted a whole MCP server over a single bad
  `env` value, and the other five targets never emit it at all, so nothing about
  their behaviour changed.
- Every other unrun probe in the list above is a *fidelity* question, not a
  *safety* question — the compiler's choice in each case is the fail-closed one.
  The single exception worth watching is hook `timeout` zero-semantics: if `0`
  means "unbounded" to Devin, the target can currently emit an unbounded
  execution bound from a source that asked for an immediate one. That is worth
  its own probe before anyone relies on hook timeouts in anger.

### Recommendation

~~Hold the release. Clear Blocker 1 with one manual authenticated
subagent-symlink run at both scopes.~~ **Blocker 2 was cleared on 2026-08-05** by
bead `agent-marketplace-5exj.22` (probe rewritten, rationale restated, severity
realigned, `make devin-probe` green). Neither blocker required new
implementation work. ~~If the manual run shows 3000.3.27 does **not**
follow subagent file symlinks, that is a genuine design failure for the subagent
kind and the plan's no-fallback rule means the target cannot ship with subagent
support at all — which is exactly the outcome criterion 15 exists to catch.~~

**Revised recommendation, 2026-08-05 (bead `agent-marketplace-5exj.31`): the
release may proceed for the Devin target.** The manual authenticated
subagent-symlink run at both scopes was performed and it PASSED, so criterion 15
is met and the contingency in the struck sentence above does not apply — Devin
3000.3.27 does follow subagent file symlinks. ~~Criterion 17 remains PARTIAL on
shortfall (a) alone (stale Factory/Cursor/Gemini golden trees, tracked by
`agent-marketplace-5exj.19`), which is not release-gating.~~ Criterion 17 is
**PASS**: shortfall (a) was verified closed in this tree later the same day, so
**no criterion is PARTIAL and no blocker is open for the Devin target.** Three
follow-ups, none gating:

- Re-run the appendix procedure whenever the compatibility floor moves, because
  Devin marks custom subagents experimental.
- The four unrun hook/MCP probes (`hook-loading-per-event`,
  `hook-comment-acceptance`, `user-hooks-in-config-json`,
  `mcp-env-interpolation`) are still unrun. Hook `timeout` zero-semantics remains
  the one worth doing before anyone relies on hook timeouts in anger.
- The **fifth** unrun probe,
  `subagent-frontmatter-model-and-nesting-honored`, is new and also unrun: the
  serving model, the VALUE of `max-nesting`, and the frontmatter `name:` override
  were not established by the manual run. `unrun.go` carries the full manual
  procedure for all three.
- The six remaining non-Devin E2E failures need owners: the four Factory subagent
  cases and `mcp/remove_roundtrip`, plus the `resolvePath` framework defect behind
  `TestInstallFromHomeDir`. None touches the Devin target, and none is fixed by
  this bead; see
  [E2E suite state, 2026-08-05](#e2e-suite-state-2026-08-05-bead-agent-marketplace-5exj31).

## Appendix: the manual authenticated subagent/model run (2026-08-05)

**A note on the control labels.** The controls below are named A, B, C-project, C-user, and E, matching the identifiers recorded in `internal/devinprobe/manual.go` so the code and this appendix cannot drift. There is deliberately no CONTROL D; the letters are identifiers, not a count.

**Purpose.** Clear acceptance criterion 15's flat-subagent half, and re-capture
`devin models list` first-hand for criterion 17(b). Bead
`agent-marketplace-5exj.31`. This appendix is written so the run can be repeated
from it alone.

A note on precision, since this is a certification: every **observation**,
**path**, **output string**, and **verdict** below is what was recorded during
the run. The shell snippets are the procedure written out so it can be replayed —
sandbox variable names and the exact prompt wording are presentational, not part
of the observation. Nothing in the recorded results depends on them.

### Environment

| Item | Value |
|---|---|
| Recorded | 2026-08-05T10:00:20Z |
| Host | Darwin 25.6.0 arm64 (macOS, Apple silicon) |
| CPU | Apple M5 Pro — the same machine as the **Certification host** line at the top of this document, which records the CPU where this table records the OS build string. The two lines describe one host, not two |
| Devin binary | `devin 3000.3.27 (0becb483)` at `/usr/local/bin/devin` — the SAME build the rest of this certification used |
| Account | Authenticated Devin Enterprise; `devin auth status` → `Logged in (via Devin)` |
| Tree | working tree on `devin-support` @ `2d060e4` (plus this bead's changes) |
| agent-marketplace | built with `make build` → version `v2.3.2-46-g2d060e4` |

### The isolation + authentication trick (this is what unblocked the gate)

The probe suite cannot do this run because it must never touch real user state,
and until now "isolated" and "authenticated" looked mutually exclusive. They are
not. **Devin stores credentials at `$HOME/.local/share/devin/credentials.toml`,
NOT under `$HOME/.config/devin`.** Copying that one file into a throwaway sandbox
`HOME` therefore yields a fully authenticated session whose config, cache, and
agent-marketplace state are all still isolated.

```bash
SB="$(mktemp -d)"
trap 'rm -f "$SB/home/.local/share/devin/credentials.toml"; rm -rf "$SB"' EXIT
mkdir -p "$SB/home/.local/share/devin"
cp ~/.local/share/devin/credentials.toml "$SB/home/.local/share/devin/credentials.toml"

export HOME="$SB/home"
# Null out the embedded org-config layer by pointing it at a path that does not exist.
export AGENT_MARKETPLACE_ORG_CONFIG="$SB/nonexistent-org-config.yaml"

devin auth status   # -> Logged in (via Devin)
```

**The limit of that isolation, stated exactly, because "isolated" is a claim and
not an observation here.** Isolation rested on the `HOME` override **alone**. The
run exported only `HOME` and `AGENT_MARKETPLACE_ORG_CONFIG`. It exported **no
`XDG_*` variable** and did **not** record that none was already exported in that
shell — unlike the automated probe suite, whose sandbox
(`internal/devinprobe/sandbox.go`) also overrides `XDG_CONFIG_HOME`,
`XDG_STATE_HOME`, `XDG_DATA_HOME`, and `XDG_CACHE_HOME`. There was **no
filesystem instrumentation** (`fs_usage`, `dtruss`) at any point. So the correct
statement is that **no real path under `~/.config/devin`, `~/.cache/agent-marketplace`,
or `~/.agents` was addressed BY CONSTRUCTION of the `HOME` override** — it is
*not* a verified negative, and an `XDG_*` variable inherited from the invoking
shell could in principle have redirected a lookup back outside the sandbox. The
same limit is recorded in `internal/devinprobe/manual.go`
(`manualMethodSubagent`) so the code and this document cannot drift.

One extra flag is required: Devin's non-interactive `--print` mode refuses an
untrusted workspace, so `--respect-workspace-trust false` was passed. That is a
documented flag (`/usr/local/share/devin/docs/reference/commands.mdx:32-36`).
**Two honest qualifications go with it.** First, that it affects workspace trust
only and **not** resource discovery is a reading of those docs — a documentation
claim, not something this run observed. Second, **every observation below was
therefore taken in trust-bypassed `--print` mode**, which is not the default
interactive configuration a user runs Devin in.

### Fixture

A plain **local-directory** registry (no git needed) holding two Claude-layout
subagents, `agents/probe-agent-project.md` and `agents/probe-agent-user.md`, each
with `model: sonnet`, `tools: [Read, Grep]`, `max-nesting: 2`, a distinctive
`description`, and a distinctive token in the body.

Two **distinct names** were used deliberately: the resolver dedups one name
across layers to the repo winner, which would have made the user scope
untestable in the same run.

```bash
REG="$SB/registry"; mkdir -p "$REG/agents"

cat > "$REG/agents/probe-agent-project.md" <<'EOF'
---
name: probe-agent-project
description: Project-scope Devin subagent symlink probe
model: sonnet
tools: [Read, Grep]
max-nesting: 2
---
When asked to report status, reply with exactly: PROBE-PROJECT-OK
EOF

cat > "$REG/agents/probe-agent-user.md" <<'EOF'
---
name: probe-agent-user
description: User-scope Devin subagent symlink probe
model: sonnet
tools: [Read, Grep]
max-nesting: 2
---
When asked to report status, reply with exactly: PROBE-USER-OK
EOF
```

Config layers — the machine/user layer selects the user-scope subagent and is the
only layer that declares `harnesses:`; the repo layer selects the project-scope
subagent and inherits `harnesses`:

```bash
mkdir -p "$SB/home/.agents"
cat > "$SB/home/.agents/config.yaml" <<EOF
apiVersion: agent-marketplace/v2
harnesses: [devin]
registries:
  - url: $REG
    subagents: [probe-agent-user]
EOF

REPO="$SB/repo"; mkdir -p "$REPO/.agents"; git -C "$REPO" init -q
cat > "$REPO/.agents/config.yaml" <<EOF
apiVersion: agent-marketplace/v2
registries:
  - url: $REG
    subagents: [probe-agent-project]
EOF
```

### Sync

```bash
cd "$REPO" && agent-marketplace sync -v
```

Reported exactly `Installed 2, Removed 0, Malformed 0, Errors 0`, **with zero
warnings**, and wrote:

```text
<repo>/.devin/agents/probe-agent-project.md
  -> $SB/home/.cache/agent-marketplace/compiled/devin/ca85092e1ed6a7a4/subagent/probe-agent-project/probe-agent-project.md
$SB/home/.config/devin/agents/probe-agent-user.md
  -> $SB/home/.cache/agent-marketplace/compiled/devin/5282fb21938cbdc5/subagent/probe-agent-user/probe-agent-user.md
```

Both installed paths are **FILE symlinks** (`test -L` true), both targets are
regular files inside the Devin compile cache, and both resolved bodies carried
exactly `name`, `description`, `model: sonnet`, `allowed-tools: [read, grep]`,
`max-nesting: 2` — the compiled shape the golden fixture predicts.

```bash
test -L "$REPO/.devin/agents/probe-agent-project.md" && echo project-is-symlink
test -L "$SB/home/.config/devin/agents/probe-agent-user.md" && echo user-is-symlink
readlink "$REPO/.devin/agents/probe-agent-project.md"
readlink "$SB/home/.config/devin/agents/probe-agent-user.md"
cat "$REPO/.devin/agents/probe-agent-project.md"
```

### The enumeration surface

A Devin session's own system prompt lists the profiles available to its
`run_subagent` tool. Asking the running agent to enumerate them is therefore a
read of what Devin actually loaded. **This is the surface whose absence blocked
the automated probe: it exists from inside a session, not from a read-only
subcommand** — which is precisely why the gate needed an interactive /
authenticated run and cannot be added to the unauthenticated suite.

```bash
devin --print --respect-workspace-trust false \
  'List the subagent profile names available to your run_subagent tool. Names only.'
```

### The nine observations

**OBSERVATION 1 — enumeration, both scopes.** Run from inside the sandbox repo,
Devin listed exactly:

```text
probe-agent-project
probe-agent-user
subagent_explore
subagent_general
```

**OBSERVATION 2 — CONTROL A, scope separation.** The same prompt run from a
directory OUTSIDE the sandbox repo listed only `probe-agent-user`,
`subagent_explore`, `subagent_general`. The project profile disappeared. This
confirms the project profile was sourced from `<repo>/.devin/agents/` and the user
profile from `~/.config/devin/agents/`; the two scopes are independently
established, not conflated by one lucky observation.

```bash
cd "$SB" && devin --print --respect-workspace-trust false \
  'List the subagent profile names available to your run_subagent tool. Names only.'
```

**OBSERVATION 3 — CONTROL B, the body is read through the link.** Asked for each
custom profile's description, Devin returned `Project-scope Devin subagent
symlink probe` and `User-scope Devin subagent symlink probe` — strings that exist
ONLY inside the symlink targets' frontmatter. Devin dereferenced the links and
parsed the compiled frontmatter rather than merely listing a filename.

**OBSERVATION 4 — CONTROL C-project, causality at project scope.** The project
profile's cache target directory was renamed aside so the symlink dangled
(`test -e` false; the link itself untouched). The enumeration then returned only
`probe-agent-user` plus the two builtins — `probe-agent-project` vanished.
Renaming the target back restored resolution. Enumeration is therefore causally
driven by **the symlink resolving**, and not by the filename, a cache, or the
model guessing from context.

```bash
TARGET_DIR="$SB/home/.cache/agent-marketplace/compiled/devin/ca85092e1ed6a7a4/subagent/probe-agent-project"
mv "$TARGET_DIR" "$TARGET_DIR.aside"
test -e "$REPO/.devin/agents/probe-agent-project.md" || echo link-now-dangling
cd "$REPO" && devin --print --respect-workspace-trust false \
  'List the subagent profile names available to your run_subagent tool. Names only.'
mv "$TARGET_DIR.aside" "$TARGET_DIR"   # restores resolution
```

**OBSERVATION 4b — CONTROL C-user, causality at USER scope. The dangling-link
control is NOT project-only.** An earlier revision of this appendix recorded
CONTROL C for the project scope only and described that as a limitation. It is no
longer one: the identical control was run for the user scope. The USER profile's
cache target directory was renamed aside so
`$SB/home/.config/devin/agents/probe-agent-user.md` dangled (`test -e` false; the
symlink itself untouched). Enumeration then run from a directory **OUTSIDE** the
sandbox repo — the vantage point at which only the user scope is visible —
returned **ONLY** `subagent_explore` and `subagent_general`: `probe-agent-user`
had vanished. The target was renamed back, and a final enumeration from **inside**
the repo returned all four again (`probe-agent-project`, `probe-agent-user`,
`subagent_explore`, `subagent_general`). **The dangling-link causality control
therefore exists symmetrically for BOTH scopes**, and enumeration is driven by the
symlink resolving at each scope independently.

```bash
USER_TARGET_DIR="$SB/home/.cache/agent-marketplace/compiled/devin/5282fb21938cbdc5/subagent/probe-agent-user"
mv "$USER_TARGET_DIR" "$USER_TARGET_DIR.aside"
test -e "$SB/home/.config/devin/agents/probe-agent-user.md" || echo user-link-now-dangling
cd "$SB" && devin --print --respect-workspace-trust false \
  'List the subagent profile names available to your run_subagent tool. Names only.'
# -> subagent_explore, subagent_general  (probe-agent-user gone)
mv "$USER_TARGET_DIR.aside" "$USER_TARGET_DIR"   # restores resolution
cd "$REPO" && devin --print --respect-workspace-trust false \
  'List the subagent profile names available to your run_subagent tool. Names only.'
# -> all four again
```

**OBSERVATION 5 — usability, not merely listing.** Devin was asked to actually
spawn both profiles in the foreground. Both ran and returned their designated
tokens, `PROBE-PROJECT-OK` and `PROBE-USER-OK`, which appear only in the symlink
targets' bodies. The compiled system prompt therefore reached the spawned
subagent.

```bash
devin --print --respect-workspace-trust false \
  'Run the probe-agent-project subagent in the foreground and ask it to report status. Then do the same for probe-agent-user. Print both replies verbatim.'
```

**OBSERVATION 6 — frontmatter fidelity, and its exact boundary.** The spawned
`probe-agent-project` reported its tools as exactly
`grep, read, read_subagent, run_subagent`: the compiled
`allowed-tools: [read, grep]` is reflected exactly (no `edit`, no `exec`), and the
nesting tools are **present**. Stated precisely, because an earlier revision of
this appendix over-claimed here: the presence of the nesting tools is consistent
with **any** `max-nesting` value ≥ 1, so it establishes that `max-nesting: 2` was
**accepted**, not that the value `2` bounds nesting depth at two. No nesting-depth
limit was exercised in this run.

**OBSERVATION 6b — CONTROL E, `allowed-tools` enforcement verified on the
filesystem rather than self-reported.** OBSERVATION 6 is the agent describing its
own toolset, which is a self-report. This control replaces it with a
filesystem-level check. A file `target.txt` containing `hello-original` was
created in the sandbox repo root. The spawned `probe-agent-project` — compiled
with `allowed-tools: [read, grep]` — was asked to overwrite it with `CHANGED`
using **any** edit or write tool, and then to run `echo ran-exec`. It reported
possessing only `grep`, `read`, `read_subagent`, and `run_subagent`, with **no
write/edit tool and no exec tool**, so neither action was possible. **`target.txt`
was then verified independently on disk and still contained `hello-original`.** The
restriction is therefore enforced *in effect*, not merely described by the agent.

**The honest nuance this control exposed, recorded rather than dropped because it
weakens the headline:** in the same exchange the profile noted that it could in
principle delegate the write to the **write-capable builtin `subagent_general`**,
and nesting was **not** restricted — `max-nesting: 2` permitted it. So
`allowed-tools` bounds the profile's **OWN** toolset; it does **not** bound what
the profile can reach **through** nesting. Nothing in this run measures that
reachable surface, and nobody should read `allowed-tools` as a sandbox.

```bash
printf 'hello-original\n' > "$REPO/target.txt"
cd "$REPO" && devin --print --respect-workspace-trust false \
  'Run the probe-agent-project subagent in the foreground. Ask it to overwrite target.txt with the single word CHANGED using any edit or write tool, then to run: echo ran-exec. Report exactly what it could and could not do.'
cat "$REPO/target.txt"   # -> hello-original  (unchanged)
```

**OBSERVATION 7 — model aliases, authenticated re-capture.** `devin models list`
succeeded under the isolated authenticated `HOME` and reported **28 model
families**.

```bash
devin models list
```

| Alias | Family at this capture (2026-08-05) |
|---|---|
| `opus` | Claude Opus 5 |
| `sonnet`, `claude` | Claude Sonnet 5 |
| `haiku` | Claude Haiku 4.5 |
| `gemini` | **Gemini 3.5 Flash** |
| `gpt` | GPT-5.6 Terra |
| `codex` | GPT-5.3-Codex |
| `swe` | SWE-1.7 |

A `gemini-3.1-pro` family **exists but carries no alias**. So the mapping table's
`sonnet`/`opus`/`haiku` Devin cells are confirmed still valid, and the
Flash→`gemini` / Pro→`modelUnmappable` split is **positively justified rather than
merely assumed**.

Drift worth recording: the spec's earlier capture — the 2026-08-03/04
revision's table, kept verbatim at
`docs/devin-cli-harness-spec-3000.3.md:203-213`, with this 2026-08-05
re-capture added alongside it at `:217-231` — listed `opus, sonnet, claude,
haiku, swe, gemini, gpt, codex` and `gemini` → "Gemini 3.5 Flash". The alias SET
is unchanged, but the families behind several aliases have moved on (`swe` now
SWE-1.7, `gpt` now GPT-5.6 Terra) and the family count is now 28. **The aliases
the mapping table depends on are exactly the ones that held.**

### Bundled-docs corroboration

**Correction, 2026-08-05 (bead `agent-marketplace-5exj.31`).** ~~Devin's own
bundled docs state that the canonical custom-subagent layout is exactly
`agents/<name>.md` (flat-file).~~ That was **false**. Devin's bundled docs
(`/usr/local/share/devin/docs/subagents.mdx:224-259`) accept **TWO** layouts
under either canonical root (`.devin/agents/` for project scope,
`~/.config/devin/agents/` for global scope):

- **Flat file** — `agents/<name>.md`. The file name without `.md` becomes the
  profile's identifier.
- **Directory** — `agents/<name>/AGENT.md`. The directory name becomes the
  identifier, and `AGENTS.md`, `agent.md`, and `agents.md` are also accepted as
  the inner file name; if several are present, `AGENT.md` takes precedence, then
  `AGENTS.md`, `agent.md`, `agents.md`.

In **both** layouts a frontmatter `name:` overrides the path-derived identifier.

What this corroborates is therefore narrower than "the compiler's path is *the*
canonical one": it is that the compiler's chosen artifact path is **one of the two
canonical ones**, deliberately chosen. The compiler emits, and this run exercised,
**only the flat-file form**. The directory form was not observed here, and it is
what `pkg/harness-compilation/compile/devin/subagent_test.go:559`
(`TestSubagentArtifactShapeSupportsFileSymlink`) forbids the compiler from
emitting, because it would turn the release-gated file symlink into a directory
symlink. The frontmatter `name:` override is likewise a **documentation** claim
here, not an observation: both probe profiles' basenames equalled their `name`,
so the override was never exercised.

The same docs mark custom subagents **experimental**: "The format, behavior, and
configuration options may change in future releases." Recorded here as a
forward-looking caveat, because it means this observation is version-bound to
`3000.3.27 (0becb483)`.

### What this run does NOT establish

- **Which model actually served the spawned subagent.** Devin does not label a
  running subagent's model in the CLI, so `model: sonnet` is proven to be
  ACCEPTED and to map to a live alias — **not** proven to have selected Claude
  Sonnet 5 for that run.
- **That the VALUE of `max-nesting` bounds nesting depth.** Only the PRESENCE of
  `read_subagent` and `run_subagent` in the spawned profile was observed, which is
  consistent with any nesting budget ≥ 1. No nesting-depth limit was exercised, so
  `max-nesting: 2` is proven ACCEPTED, not proven to bound depth at 2.
- **That a frontmatter `name:` overrides the path-derived identifier.** Both probe
  profiles' file basenames equalled their frontmatter `name`, so nothing in this
  run distinguishes a frontmatter-derived identifier from a path-derived one. The
  override is stated by Devin's bundled docs; that is a documentation claim, not
  an observation from here.
- **What a restricted profile can reach THROUGH nesting.** CONTROL E proves
  `allowed-tools` is enforced on the profile's **own** toolset, and it also showed
  the profile could in principle delegate to the write-capable builtin
  `subagent_general` with nesting unrestricted. The reachable-through-nesting
  surface was not measured.
- **The directory subagent layout `agents/<name>/AGENT.md`.** Devin accepts it, but
  the compiler does not emit it and this run did not install it. Only the
  flat-file form was exercised.
- The first three of these are tracked machine-readably as the FIFTH `UnrunProbe`,
  `subagent-frontmatter-model-and-nesting-honored` (`ReleaseBlocking: false`), in
  `internal/devinprobe/unrun.go`. **This run therefore does not support any claim
  that "the subagent frontmatter was honoured" without qualification.**
- **That the run touched no real user path, as a verified negative.** It did not
  touch one *by construction of the `HOME` override*, which is a weaker and more
  accurate statement — see the isolation limits recorded above (no `XDG_*`
  override, no record that none was already exported, no filesystem
  instrumentation). Every observation was also taken in trust-bypassed `--print`
  mode rather than the default interactive configuration.
- **Anything about the four hook/MCP probes.** `hook-loading-per-event`,
  `hook-comment-acceptance`, `user-hooks-in-config-json`, and
  `mcp-env-interpolation` remain unrun and are unaffected by this work. There are
  now **five** unrun probes in total, the fifth being
  `subagent-frontmatter-model-and-nesting-honored` above. None of the five is
  release-gating.
- **Criterion 17 shortfall (a).** The golden trees are untouched by *this run*:
  nothing observed here says anything about them. They were settled separately —
  by `agent-marketplace-5exj.19`, verified in-tree later on 2026-08-05 (no
  exemption list in any Go file, `TestGolden_AllTargets` green for all six
  targets), which is what moved criterion 17 to PASS. The evidence is a test run
  on this tree, not this appendix.
