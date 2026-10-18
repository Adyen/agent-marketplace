# R2 Frozen Scope and Evidence Ledger

Parent: [#215]. Round: [#431]. Frozen: 2026-10-18.
Code baseline: [`299bf2aba16e711663718a22057a7b3bfb085af8`][baseline].

## Scope Rule

This ledger freezes the **complete** R2 case set: FS01-FS44. It replaces the
open-ended C1-C4 checklist in #431 with a finite disposition per case. At
freeze, 40 cases are final and 4 are confirmed residuals that need a fix:

| Freeze disposition | Count | Meaning |
|---|---:|---|
| Delivered | 31 | Fixed by merged work; the cited witnesses or reproductions passed on the baseline |
| Removed | 2 | The reported consumer is no longer shipped |
| No change | 4 | Reproduced as refused, or as no authority crossing under the normative contract |
| Accepted boundary | 3 | An existing maintainer decision applies; the reported mechanism remains |
| Residual | 4 | Reproduced on the baseline; R2 stays open until each is fixed and merged |

- R2 admits every filesystem-admission and scope-authority claim in both
  exports that no other round already owns, including F-only claims that #431
  never named. Path-shaped claims already decided in #432 stay there (see
  Exclusions). Every other finding is routed (below). Nothing outside
  FS01-FS44 extends R2.
- Keep case IDs and report aliases stable. Update evidence in place. A new
  suspicion, or a variant outside a row's reported claim, belongs in a new issue
  linked from #215.
- Scanner severity, reachability and triage fields are not evidence. Every
  disposition here rests on baseline code, a passing witness or a recorded
  reproduction with inert fixtures.
- R2 owns filesystem admission and scope authority; R5 ([#434]) owns byte and
  work limits. FS28 shares an instance with R5's R09; only its admission
  portion is R2's.
- `Delivered` is not a claim of complete filesystem snapshots, authenticated
  ownership state, atomic exclusion of a hostile same-user writer or
  whole-operation rollback. Those remain explicit non-goals of the delivered
  contracts.
- `openspec/specs/` remains the runtime authority. This is a documentation-only
  freeze; it introduces no runtime change.

## Source Provenance

The report labels follow #433 and the R5 ledger: `A<n>` is the 2026-09-17
export, zero-based; `F<n>` is the 2026-09-24 export, one-based. #431's bare case
numbers (for example "case 33") are `A` numbers. Both files declare SARIF 2.1.0
and `depthfirst-code-scanner` 1.0.0, with no version-control provenance.

| Export | Supplied Filename | Bytes | Results | SHA-256 |
|---|---|---:|---:|---|
| A | `beyildirim-agent-marketplace-vulnerabilities.sarif.json` | 6921063 | 131 | `67554afc2e08a359137bc66899a17547d812ca2d5022dfa08065753fa8d1690b` |
| F | `beyildirim-agent-marketplace-vulnerabilities.sarif (3).json` | 9141312 | 173 | `91906e9899dbc253d659ba8b87b7fdb03b837aa0d1f4a24a160d776594379e98` |

The [R2 source index](2026-10-18-security-r2-source-index.csv) maps all 211
distinct `properties.instance_id` values (304 occurrences) to an R2 case or a
route, and carries the R5 index scope for cross-checking. Source-index SHA-256:
`8ce78a2598979dc7787c41f6cd8ebf2bbcc62a8eee1c35e3c129e93238d9c931`.

| Index scope | IDs | Destination |
|---|---:|---|
| `FS01`-`FS44` | 44 | This ledger |
| `r1` | 21 | #215: selection, acquisition and consent work (R1 carry-over and decisions 2/45) |
| `r3` | 32 | [#432] (closed), including its R3.4 same-user/environment decision |
| `r4` | 35 | [#433] (closed); its F101 transfer is #715 (closed) |
| `r5` | 60 | [#434] frozen ledger |
| `parent` | 19 | #215: hosted-service, telemetry-authority, PATH and operator follow-ups |

Routes are pointers, not dispositions; this ledger does not record or change
another owner's outcome.

## FS.1 Configuration, Lock and Workspace Scope (was C4)

| Case | Report Aliases | Consumer / Claim | Baseline Evidence | Freeze Disposition |
|---|---|---|---|---|
| FS01 | A19 | Failed Git probe adopts an ancestor project as the repository root | `9f4b7312` (merged via #242); `TestFailedGitDiscoveryDoesNotAdoptParent` | Delivered |
| FS02 | A20 / F15 | `migrate` rewrites a user config through a repository config link | No `migrate` command or legacy editor (`consumer-config-format`); `TestConsumerMigrationSurfaceIsAbsent` | Removed |
| FS03 | A21 / F16 | Repository-scope install/registration edits the user config through a link | #453; `TestRepositoryEditRefusesExternalConfigurationTarget`, `TestManagedEditRejectsCrossLayerAliases`, `TestPrepareRuntimeLocksRejectsCrossLayerObjectAlias`; supported link control `TestWriteConfigDocumentPreservesUserDotfileLink` | Delivered |
| FS04 | A48 / F32 | Removal and Marketplace pin publication follow an external config link | #453; `TestManagedPublicationRefusesExternalLinks`, `TestConfigDescriptorParentReplacement`; control `TestDeinitPurgeRemovesApprovedUserDotfileLink` | Delivered |
| FS05 | F114 | Marketplace lock bootstrap writes a lock beside a linked config in another checkout | #453; reproduction R-F114: `lock` and `sync` refuse (`link target escapes its captured root`); the other checkout gains no lock and no entries | Delivered |
| FS06 | A44 | Online sync appends to a linked `.gitignore` outside the checkout | `8fc58a92` (#191); `TestEnsureLocalConfigGitignored` (symlink, FIFO child); control `TestSyncCommandAddsLocalConfigAndLockToGitignore` | Delivered |
| FS07 | F137 | A relative directory source in a machine layer resolves against the checkout | v4 resolves it against the declaring file's directory and refuses new relative user sources; `TestV4DirectoryPreservesMachineAndFragmentBases`, `TestLayerEditorRejectsNewUserRelativeRegistry` | No change |
| FS08 | F1 | A committed `harnesses:` list narrows the organization preservation set of `deinit --scope user` | Reproduction R-F1: after `harnesses: [claude]` is committed, `deinit --scope user` exits 0 and removes the organization's Factory skill. Control `TestDeinitUserPreservesOrgOwnedArtifacts` passes | **Residual** |

FS08 root cause: `resolveOrgOwnedArtifactsMode` resolves through the real
repository workspace, so the project target set wins. Fix direction: compute
organization preservation from machine-layer targets only, consistent with the
per-scope rule in `resource-selection` (a repository selection cannot displace
machine-scope output).

## FS.2 Harness Roots, Ancestors and Output Identity (was C2)

| Case | Report Aliases | Consumer / Claim | Baseline Evidence | Freeze Disposition |
|---|---|---|---|---|
| FS09 | A1 / F5 | SDK `Materialize` writes through symlinked output roots or ancestors | SDK root/parent refusal, #543, #544; `TestMaterialize_RejectsSymlinkedArtifactParent`, `TestMaterializeRefusesReplacedRootWithoutStagingResidue`, `TestSDKPublicationRefusesReplacedRootAndPreservesForeignStage`, `TestSDKStageCreationRefusesReplacedOutputRoot` | Delivered |
| FS10 | A33 / F25 | A repository harness root link escapes to machine directories | #463, #464; `TestRunUnsafeRootShapesPreserveState` (escaping, dangling, cyclic and non-directory roots for all 8 root contexts), `TestRunRejectsEscapingHarnessRoot`, `TestRunCacheLinkPublicationRefusesReplacedParent`; control `TestRunHarnessRootAliasesRemainUsable` | Delivered |
| FS11 | A40 / F29 | A committed `.devin` link redirects Devin aggregate writes | #463; `TestRunUnsafeRootShapesPreserveState` refuses an escaping `.devin` root. A contained root alias (F29, `.devin` to `.windsurf`) is explicitly permitted by `materialization`; the repository author already controls both trees | Delivered |
| FS12 | F141 | `.factory/hooks` linked into `.git/hooks` installs hook support there | #463 ancestor rule; `TestRunUnsafeOutputAncestorPreservesState`; reproduction R-F141: refused in both modes, `.git/hooks` stays empty | Delivered |
| FS13 | F145 | Repository and machine Gemini scopes share one `settings.json` when the repository is HOME | `da3a07f2` (#438); `TestGeminiSharedScopeFileKeepsBothScopes` | Delivered |
| FS14 | F146 | A case-variant `CODEX_HOME` aliases the repository `.codex` on a case-insensitive volume | Reproduction R-F146 on case-insensitive APFS: `CODEX_HOME=<repo>/.CODEX` produces no diagnostic and the machine definition silently replaces the repository definition. The lexical `.codex` control refuses with `materialize.destination.overlap` | **Residual** |
| FS15 | A96 / F70 | Case-only Codex agent names alias one output file | #732; `TestCaseOnlyNameCollisionsAreRefusedPortably`, `TestSyncRefusesCaseOnlyNameCollisions` | Delivered |
| FS16 | F151 | A repository collision winner evicts a machine-scope artifact | `2815ef93` (#417); `TestResolveDifferentRegistriesCollideOnlyWithinInstallScope`, `TestSyncCrossScopeSkillAndHookStayInSync` | Delivered |
| FS17 | F113 | `convert --sync` follows a symlinked default output directory | `convert` removed in #665; library output admission is FS09 | Removed |

FS14 root cause: `codexPartitionKey` and the managed-path intersection check
compare cleaned path strings. The trigger is the user's own `CODEX_HOME`, not
lower-trust input, so this is an integrity defect rather than an authority
crossing. Fix direction: compare existing partition roots by filesystem object
identity, consistent with the case-only refusal policy of #732.

## FS.3 Source and Support Readers

| Case | Report Aliases | Consumer / Claim | Baseline Evidence | Freeze Disposition |
|---|---|---|---|---|
| FS18 | A35 / F27 | Skill and hook support-file links copy host files into output | #471, #473, #533-#540; `TestRunSupportCopyRefusesEscapingSourceLink`, `TestRunGeminiHookSupportSourceRefusalPreservesNativeContent`; control `TestRunSupportCopyKeepsContainedAliasAndMode` | Delivered |
| FS19 | A113 / F78 | Factory ingestion follows links outside the source root | Shipped catalog/sync ingestion reads a captured `fsboundary` source view (#477-#484); direct conversion removed (#665); `TestLoadPrimarySourceRefusesForeignPayloadAlias`, `TestLoadFactoryUnsafeDirectoryAliases`, `TestSourceFSUnsafeDirectoryAliases` | Delivered |
| FS20 | A117 / F82 | Claude bundle and command reads follow links | Same captured source view and witnesses as FS19 | Delivered |
| FS21 | A121 / F86 | Devin ingestion follows nested links | Same captured source view and witnesses as FS19 | Delivered |
| FS22 | F131 | A configured registry `path` that is itself a link selects a sibling subtree | Reproduction R-F131: an in-snapshot root alias is accepted, an escaping one refused. `allowed_registries` matches URLs only, so `path` selects a layout, not an authority; the link is authored at the configured path in the same commit. `TestBuildRejectsTraversalAndSymlinkEscape` | No change |
| FS23 | A29 | Index-only Marketplace staging shares one directory across same-kind resources | `05eb5f32` (#195); `TestIndexOnlyStagingNamesSeparateSameKindResources` | Delivered |
| FS24 | A30 | An absolute `Source.File` replaces the verified Marketplace source path | `05eb5f32` (#195), #451; `TestCanonicalArtifactDecodeRejectsAbsolutePaths`, `TestMaterializationSourcePathMarketplaceUsesResolvedPath`, `TestHookDefinitionBasenameRequiresSafeCanonicalSourceFile` | Delivered |
| FS25 | F129 | A slash-containing ref nests inside another ref's mutable cache checkout | Reproduction R-F129: resolving `foo/skills/evil` after `foo` returns a path inside the `foo` checkout (Git top-level is `foo`) and moves the `foo` checkout's HEAD | **Residual** |

FS19-FS21: public library ingestion of a caller-supplied `fs.FS` keeps the
caller's authority; that is the library contract, not a shipped consumer.

FS25 scope: the mutable cache serves TUI refresh and `update` discovery, not
exact locked synchronization, and both refs belong to one configured registry.
Fix direction: refuse a cache hit whose Git top-level is not the cache path, or
encode the ref as a single path component.

## FS.4 Native and Durable Aggregates

| Case | Report Aliases | Consumer / Claim | Baseline Evidence | Freeze Disposition |
|---|---|---|---|---|
| FS26 | A36 / F169 | Marker-merge leaf links import external bytes into the checkout | #466, #495; `TestRunMarkerMergeRefusesExternalAlias`, `TestRunReadOnlyMarkerMergeRefusesExternalAlias`; control `TestRunContainedMarkerAliasesPreserveNativeContent` | Delivered |
| FS27 | F167 | A linked Claude `.mcp.json` is copied before marker merging | #466, #495 (native); #541, #601 (SDK reader). Reproductions R-F167: repository `.mcp.json` and user `~/.claude.json` links refuse with `materialize.write.error`; SDK merge refuses the leaf link (`leaf is a symlink`); link and external bytes preserved | Delivered |
| FS28 | A39 / F28 | Aggregate links or FIFOs hang or redirect reads | `780b326a` (merged via #242), #497, #512; `TestAggregateConsumersRejectFIFO`, `TestRunNativeAggregateInventoryFIFO`, `TestRunNativeOwnershipMarkerRejectsFIFO`. Size limits remain R5 R09 | Delivered |
| FS29 | A42 | `.devin/mcp_config.json` linked to the user's Devin config imports its secrets | #497, #498; `TestStoreReadAggregateRefusesIntermediateAlias`, `TestRunDurableAggregateReadRefusesRetargetedRoot`; reproduction R-A42: leaf link refused, user config unchanged, nothing copied | Delivered |
| FS30 | A109 / F75 | A concurrent edit between merge read and publication is overwritten | #495, #601; `TestRunMarkerAggregateMergeRetainsReadGeneration`, `TestSDKMergeComparisonRetainsOriginalInputGeneration` | Delivered |
| FS31 | F148 | Ownership `Apply` replaces an aggregate without rechecking it | Generation/digest CAS with #522, #531, #586; `TestApplyRefusesSameJSONOriginalRecordReplacement`, `TestRecoverRefusesOriginalJournalReplacementBeforeAction` | Delivered |
| FS32 | A31 / F24 | Codex MCP reconciliation deletes unrelated tables inside a managed server's range | Reproduction R-A31 through `materialize.Run`: with `[profiles.safe]` placed between `[mcp_servers."foo"]` and `[mcp_servers."foo".env]`, both a source update and a retirement delete it; the run reports success with only the trust-review warning | **Residual** |

FS30/FS31 are generation checks, not atomic exclusion after the last check;
the `materialization` and `owned-entry-reconciliation` contracts state that
non-goal. #580 (ADR 0009) separately accepts the SDK quarantine deletion window.

FS32 root cause: `codexMCPSections` extends one range from a server's first
table to its last, absorbing interleaved non-owned tables. The mandatory safety
backup keeps the prior bytes, but the deletion violates the unproven-entry
preservation rule of `owned-entry-reconciliation`. Fix direction: patch only
the owned tables, or refuse non-contiguous ranges with the existing coded
refusal.

## FS.5 Ownership, Manifests and Pruning (was C1)

| Case | Report Aliases | Consumer / Claim | Baseline Evidence | Freeze Disposition |
|---|---|---|---|---|
| FS33 | A34 / F26 | A committed `.agents` link redirects portable manifest reads and writes | #474, #475; `TestRunPortableManifestAliasRefusalPreservesNativeOutput`, `TestRunReadOnlyPortableManifestParentRefusalIsNotAbsence`, `TestRunPortableManifestPublicationPreservesReplacedGeneration` | Delivered |
| FS34 | A37 / F170 | A forged portable manifest nominates unrelated files for deletion | #485-#489, #598; `TestRunPortableManifestCannotNominateUnrelatedRepositoryFile`, `TestRunPortableManifestCannotNominateRootFilesOrComponentDirectories`, `TestRunCodexManifestCannotNominateSharedPartitionContent` | Delivered |
| FS35 | A108 / F73 | SDK sync pruning deletes unowned output | #549, #551, #558-#564; #600 records the accepted quarantine window; `TestSDKRemovalReceiptsDistinguishActualDeletion`, `TestSDKStaleReadsRefuseMultiplyLinkedPayloads`, `TestSDKStaleDiscoveryRefusesDirectoryReplacementBeforeImportingMembers`, `TestSDKQuarantineRemovalPreservesReplacementGeneration` | Delivered |
| FS36 | A87 / F64 | A failed run leaves earlier artifacts published | SDK partial-result contract (`materialization`: partial-result semantics remain compatible); partial results do not prune; `hcc` removed (#713). `TestMaterialize_WritesPartialResultAndReportsItAsUnclean`, `TestMaterialize_PartialResultDoesNotPrune` | No change |
| FS37 | F144 | Codex outputs survive a failed MCP duplicate check | Reproduction R-F144: the duplicate refuses with `materialize.codex.duplicate-identity`, no `config.toml` is written, and an independently selected agent installs as intended. `TestCodexMCPDuplicateDesiredIdentityFailsClosed`, `TestDevinPartialFailureCommitsSiblingsAndPostponesPrune` | No change |

Matching-digest nominations within an authorized output namespace remain the
existing FS34 contract; no tamper-proof ownership redesign is implied.

## FS.6 Sensitive New-File Modes

| Case | Report Aliases | Consumer / Claim | Baseline Evidence | Freeze Disposition |
|---|---|---|---|---|
| FS38 | A41 | A new Devin `mcp_config.json` is created `0644` | `7afe907b` (#191); `TestPrivateNewAggregateModeAndExistingMode`, `TestGeminiMixedPrivateAggregateMode` | Delivered |
| FS39 | A107 / F74 | SDK MCP output uses default permissions | `7afe907b` (#191); `TestPrivateMCPCompileModes`, `TestMaterializePrivateMCPModes`. Existing shared modes stay as authored (accepted compatibility) | Delivered |

## FS.7 Archive and Recovery (was C3)

| Case | Report Aliases | Consumer / Claim | Baseline Evidence | Freeze Disposition |
|---|---|---|---|---|
| FS40 | F124 | A crafted archive replaces the harness root with a link and writes through it | #503, #527, #569, #570; `TestRestoreTreeRefusesRetargetedRoot`; reproduction R-F124: `.` to `.ssh` refuses (`path escapes its root`), `.` to `../.ssh` refuses (`invalid symlink target`); nothing written | Delivered |
| FS41 | F125 | A planted Codex set manifest authorizes arbitrary `managed_paths` | Mechanism present: `managed_paths` are syntax-checked only. Planting needs same-user write to the private backup store; restore is an explicit opt-in overwrite. #500, #530; `TestRestoreCodexSetRefusesAliasedManifest`, `TestRestoreCodexSetRefusesArchiveReplacementAfterPreflight` | Accepted boundary (R3.4) |
| FS42 | F126 | A planted ordinary archive overwrites unmanaged harness files | Reproduction R-F126: the newest planted archive restores `settings.json` over the existing file. Same precondition and opt-in restore as FS41; `TestOrdinaryRestoreRetainsSelectedArchiveAndDirectoryGenerations`, `TestCreateAndRestore` | Accepted boundary (R3.4) |
| FS43 | F133 | A forged dependency journal under a chosen `XDG_STATE_HOME` deletes a file | Mechanism present for legacy destinations: deletion needs a matching path, SHA-256 and mode; managed destinations rebind through a fresh layer capture. Requires control of the state directory or launch environment | Accepted boundary (R3.4) |
| FS44 | F147 | A forged ownership journal `staged_path` deletes an arbitrary file | #516, #526, #529, #587, #588; `TestRecoverStagedCleanupRefusesForeignPathAuthority` | Delivered |

R3.4 ([#432]) is the maintainer decision that same-user control of local state,
or control of the trusted environment, is not a privilege boundary for this
CLI. FS41-FS43 apply it; they are not repaired races. Reopen only if a
lower-trust input can supply the backup store or state directory.

## Exclusions, Overlaps and Leads

| Item | Reason |
|---|---|
| A25/F19, A57/F38, A58/F39, A61/F42, F121 (`r3`) | Path-shaped log and telemetry-state claims; decided by R3.4 in #432 |
| A72/F49, A82/F60 (`parent`) | Hosted lease and assessment races retained by #215 |
| A22/F17, A24/F20, A111/F77, F102, F140 (`r5`) | R2 admission is delivered; the remaining byte limits are R5 cases |
| #732 known limits | Skill/command case-only pairs on command-as-skill targets via `sync`, and Unicode normalization, are outside F70's claim; not admitted |
| Hosted ingestion support enumeration | `catalog.HookMaterializationSupportFiles` lists names through `os.DirFS` for `pkg/marketplace/ingest`; not a reported CLI consumer; not admitted |
| Earlier #431 leads | Same-second Codex backup-set ordering and the transient source-revision CI flake are not report cases; #510 tracks staging-residue scaling separately |

## Delivery and Closure

- [x] Freeze both source hashes, the complete 211-ID index, FS01-FS44 and routes.
- [x] Record fresh baseline evidence: 80 witnesses executed and passed;
  14 reproduction tests cover 12 cases (see Freeze Verification).
- [x] Audit review threads on the 95 merged R2 PRs: 296 threads, 1 unresolved.
- [ ] Fix FS08, FS14, FS25 and FS32 in scoped PRs from `main`. Start each from
  its recorded reproduction as a failing-first consumer regression with a valid
  control, preserve bytes/metadata on refusal and leave no staging residue,
  archive an OpenSpec delta, run `make validate`, `make spec-check` and
  `make validate-e2e`, and run `open-code-review-delegate` before pushing.
- [ ] Resolve the #601 CodeRabbit thread: the `materialization` scenario
  "Native marker alias detachment preserves concurrent generations" names
  replacement staging as a read-only trigger. Correct it through an OpenSpec
  delta, or reply with the reason it stands.
- [ ] After the last residual merges, rerun the witness list and every
  reproduction on integrated `main` (all must pass; R-F146 on a
  case-insensitive volume) plus `make spec-check`, and record that SHA here.
- [ ] Close #431 and update #215 M1/M2.

Nothing else is required to close R2. In particular, re-auditing merged layers,
reopening routed findings or adding new budgets is out of scope.

## Freeze Verification

All checks ran on the baseline on 2026-10-18, macOS arm64, `CGO_ENABLED=0`.

- **Witnesses.** The 80 tests in
  [`witnesses.txt`](security-r2-2026-10-18/witnesses.txt) ran with
  `go test -json -count=1` (not short mode) and each reported `pass`; none
  skipped. The FIFO child helper is excluded because its parent test executes it.
- **Reproductions.** Sources are in
  [`security-r2-2026-10-18/repro/`](security-r2-2026-10-18/repro/). A failing
  test with a `REPRODUCED` message records a residual; a passing test records a
  refusal or the stated behavior.

| Reproduction | Package | Result |
|---|---|---|
| R-F1 `TestR2LedgerF1DeinitHarnessNarrowing` | `cmd/agent-marketplace` | Fails: residual FS08 |
| R-F114 `TestR2LedgerF114LockBootstrapThroughConfigSymlink` | `cmd/agent-marketplace` | Passes: refused |
| R-F146 `TestR2LedgerF146CaseAliasedCodexHome` | `internal/materialize` | Fails on case-insensitive APFS: residual FS14 (the `.CODEX` subtest skips on case-sensitive volumes) |
| R-F141 `TestR2LedgerF141FactoryHooksIntoGitHooks` | `internal/materialize` | Passes: refused |
| R-F167 `TestR2LedgerF167ClaudeMCPExternalAlias` | `internal/materialize` | Passes: refused |
| R-F167 `TestR2LedgerF167SDKMergeLeafAlias` | `pkg/harness-compilation` | Passes: refused |
| R-A42 `TestR2LedgerA42DevinMCPLeafAlias` | `internal/materialize` | Passes: refused |
| R-A31 `TestR2LedgerA31CodexInterleavedTable{,Run}` | `internal/materialize` | Fails: residual FS32 |
| R-F144 `TestR2LedgerF144CodexPartialOutputs` | `internal/materialize` | Passes: scoped refusal |
| R-F129 `TestR2LedgerF129NestedRefCacheAlias` | `internal/cache` | Fails: residual FS25 |
| R-F131 `TestR2LedgerF131ConfiguredRootAlias` | `internal/sourcerevision` | Passes: contained alias accepted, escape refused |
| R-F124 `TestR2LedgerF124RestoreRootLink` | `internal/backup` | Passes: refused |
| R-F126 `TestR2LedgerF126RestoreOverwritesExisting` | `internal/backup` | Passes: planted archive overwrites (accepted boundary) |

To rerun one, copy `repro/<package>_r2ledger_repro_test.go.txt` to
`<package>/zz_r2ledger_repro_test.go` and run
`CGO_ENABLED=0 go test ./<package> -run '^TestR2Ledger' -count=1 -v`. For
R-F146 on macOS, mount a case-insensitive APFS image and set `TMPDIR` to a
directory on it. Fixtures are inert local directories and repositories; nothing
executes a resource payload or contacts a network service. Do not commit the
copied files.

- **Scoped gate.** `make validate` with the 15 witness packages and the
  witness-name filter passed: short-mode tests, `go vet`, CI lint (0 issues)
  and the build. This is not a full-suite result.
- **Review threads.** GraphQL over the 95 merged R2 PRs (head branches
  `security-431`, `security-580` and #732) found 296 review threads; the only
  unresolved one is the #601 thread in Delivery and Closure. The other cited
  PRs (#191, #195, #242, #417, #438, #451, #665, #713) have 16 threads, none
  unresolved.
- **Reconciliation.** The check below passed against both exact exports.
  Negative controls reject swapped exports, an altered index alias, a missing
  case row and a changed disposition count.

No E2E, native probe, race or benchmark gate is claimed for this
documentation-only freeze.

## Reproduce the Source Reconciliation

From the repository root, set `REPORT_A` and `REPORT_F` to the original files,
then run this read-only check. It needs only Python's standard library, opens
no network connection and never interprets report payloads.

```bash
python3 - "$REPORT_A" "$REPORT_F" <<'PY'
import csv
import hashlib
import json
import re
import sys
from collections import Counter
from pathlib import Path

docs = Path("docs/issues")
index = docs / "2026-10-18-security-r2-source-index.csv"
ledger = docs / "2026-10-18-security-r2-frozen-ledger.md"
r5_index = docs / "2026-10-16-security-r5-source-index.csv"
assert hashlib.sha256(index.read_bytes()).hexdigest() == (
    "8ce78a2598979dc7787c41f6cd8ebf2bbcc62a8eee1c35e3c129e93238d9c931")
with index.open(newline="") as stream:
    rows = list(csv.DictReader(stream))
by_id = {row["instance_id"]: row for row in rows}
assert len(rows) == len(by_id) == 211
expected = {}
sources = [
    ("a_ref", "A", 0, 131, 6921063,
     "67554afc2e08a359137bc66899a17547d812ca2d5022dfa08065753fa8d1690b"),
    ("f_ref", "F", 1, 173, 9141312,
     "91906e9899dbc253d659ba8b87b7fdb03b837aa0d1f4a24a160d776594379e98"),
]
assert len(sys.argv) == 3, "supply the A and F export paths, in that order"
for filename, (column, prefix, offset, count, size, digest) in zip(sys.argv[1:], sources):
    data = Path(filename).read_bytes()
    assert len(data) == size and hashlib.sha256(data).hexdigest() == digest
    sarif = json.loads(data)
    assert sarif["version"] == "2.1.0" and len(sarif["runs"]) == 1
    results = sarif["runs"][0]["results"]
    assert len(results) == count
    seen = set()
    for number, result in enumerate(results, offset):
        identity = result["properties"]["instance_id"]
        assert identity not in seen
        seen.add(identity)
        assert result["partialFingerprints"]["staticFingerprint"] == identity
        expected.setdefault(identity, {"a_ref": "", "f_ref": ""})[column] = prefix + str(number)
assert set(expected) == set(by_id)
for identity, aliases in expected.items():
    assert all(by_id[identity][key] == value for key, value in aliases.items())
with r5_index.open(newline="") as stream:
    r5 = {row["instance_id"]: row["scope"] for row in csv.DictReader(stream)}
assert all(by_id[i]["r5_scope"] == scope for i, scope in r5.items())
for row in rows:
    if row["r5_scope"].startswith("R") and row["r5_scope"][1:].isdigit():
        assert row["scope"] == "r5" or row["scope"] == "FS28"
cases = {f"FS{number:02}" for number in range(1, 45)}
counts = Counter(row["scope"] for row in rows)
assert counts == Counter({**dict.fromkeys(cases, 1), "r1": 21, "r3": 32,
                          "r4": 35, "r5": 60, "parent": 19})
table = re.findall(r"^\| (FS\d{2}) \| ([^|]+) \|(?:[^|]*\|){2} ([^|]+) \|$",
                   ledger.read_text(), re.M)
assert len(table) == 44 and {case for case, _, _ in table} == cases
for case, aliases, _ in table:
    row = next(row for row in rows if row["scope"] == case)
    assert set(re.findall(r"[AF]\d+", aliases)) == {
        row[key] for key in ("a_ref", "f_ref") if row[key]}
dispositions = Counter(re.sub(r"\s*\(.*\)$", "", d).strip("* ") for _, _, d in table)
assert dispositions == Counter({"Delivered": 31, "Removed": 2, "No change": 4,
                                "Accepted boundary": 3, "Residual": 4}), dispositions
print("PASS: 304 occurrences, 211 IDs, 44 R2 cases, 167 routed")
PY
```

[#215]: https://github.com/Adyen/agent-marketplace/issues/215
[#431]: https://github.com/Adyen/agent-marketplace/issues/431
[#432]: https://github.com/Adyen/agent-marketplace/issues/432
[#433]: https://github.com/Adyen/agent-marketplace/issues/433
[#434]: https://github.com/Adyen/agent-marketplace/issues/434
[baseline]: https://github.com/Adyen/agent-marketplace/commit/299bf2aba16e711663718a22057a7b3bfb085af8
