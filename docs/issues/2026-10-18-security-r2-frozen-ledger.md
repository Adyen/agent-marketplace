# R2 Frozen Scope and Evidence Ledger

Parent: [#215]. Round: [#431]. Frozen: 2026-10-18.
Code baseline: [`299bf2aba16e711663718a22057a7b3bfb085af8`][baseline].

## Threat Model

The CLI reads and writes the user's home directory and the repository
checkouts it runs in. The same OS user owns both, so file location is not the
boundary. Authorship is: the boundary is **content the user did not write**:

1. **A repository checkout** the user runs the CLI in. Its committed files,
   symlinks, `.agents/` config and harness directories are authored by
   whoever controls that repository.
2. **Registry and Marketplace content** the user selected: resource bytes,
   support files, symlinks inside a registry tree, and published artifacts.
3. **Other OS accounts** on a shared machine, which must not be able to read
   secrets the CLI writes.

A case is a real R2 threat only when one of those inputs makes the CLI do
something to the user that the input's author could not already do: write,
delete or overwrite files outside the checkout, copy the user's files or
secrets somewhere the author can see, install something that executes, or
remove tooling the user or organization installed machine-wide.

These are **not** security boundaries, and no work is owed against them:

- Anything that first requires writing to the user's own HOME, state
  directory, cache or backup store. A process with that access already
  controls the user.
- The user's own environment and configuration (`CODEX_HOME`, `XDG_*`,
  personal config, running sync with the repository root set to HOME).
- Another process of the same user editing a file at the same moment. That is
  a correctness concern, not an attack.
- A content author affecting only content they already control. For example,
  two refs of one registry, or two files of one commit.
- A library caller choosing its own output directory.

This matches the maintainer decision recorded as R3.4 in [#432]: same-user
control of local state, or control of the trusted environment, is not a
privilege boundary for this CLI.

## Result

| Section | Cases | Status |
|---|---:|---|
| Real threats | 25 | 24 fixed, **1 open** (FS08, fix in #750) |
| Not a security boundary | 19 | 8 fixed anyway, 6 no action, 2 removed with their command, 3 ordinary bugs |

**R2 closes when FS08 is fixed** (#750). The three ordinary bugs, FS32 (#746),
FS14 (#747) and FS25 (#748), are worth fixing, but they are not security work
and do not gate R2.

Much of the R2 hardening protects the second table: #500-#531 and #565-#589
(backup archives, ownership state and journals, safety backups) and
#558-#564 (the SDK quarantine), all against replacement by a same-user process. That code is
harmless and stays, but it is robustness, not security. No further R2 work
belongs there.

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
| `FS01`-`FS44` | 44 | This ledger: every filesystem or scope claim not already owned by another round |
| `r1` | 21 | #215: selection, acquisition and consent work |
| `r3` | 32 | [#432] (closed), including path-shaped log and telemetry-state claims decided by R3.4 |
| `r4` | 35 | [#433] (closed) |
| `r5` | 60 | [#434] frozen ledger (byte and work limits) |
| `parent` | 19 | #215: hosted-service, telemetry-authority, PATH and operator follow-ups |

Routes are pointers; this ledger does not change another owner's outcome. A new
suspicion goes to a new issue linked from #215, and must name which input in
the threat model it comes from.

## Real Threats

Untrusted input reaching the user's files, secrets or execution.

| Case | Report Aliases | Untrusted Input and Harm | Evidence on the Baseline | Status |
|---|---|---|---|---|
| FS03 | A21 / F16 | Repo commits `.agents/config.yaml` as a link to `~/.agents/config.yaml`; a repo-scope install edits the user's personal config, for example adding the attacker's Marketplace to every project | #453; `TestRepositoryEditRefusesExternalConfigurationTarget`, `TestManagedEditRejectsCrossLayerAliases`, `TestPrepareRuntimeLocksRejectsCrossLayerObjectAlias` | Fixed |
| FS04 | A48 / F32 | The same link trick on removal and Marketplace pin writes | #453; `TestManagedPublicationRefusesExternalLinks`, `TestConfigDescriptorParentReplacement` | Fixed |
| FS05 | F114 | The same link trick makes lock bootstrap write a lock into another checkout | #453; reproduction R-F114 refuses for `lock` and `sync`, and nothing is written there | Fixed |
| FS06 | A44 | Repo commits `.gitignore` as a link to a file in HOME; sync appends lines to it | `8fc58a92` (#191); `TestEnsureLocalConfigGitignored` | Fixed |
| FS07 | F137 | A relative directory in user/org config resolved against whichever repo the user is in, so repo content gained user-level selection | v4 resolves it against the config file's own directory; `TestV4DirectoryPreservesMachineAndFragmentBases`, `TestLayerEditorRejectsNewUserRelativeRegistry` | Fixed |
| FS08 | F1 | Repo commits `harnesses: [claude]`; plain `sync` inside it removes the user's and organization's machine-wide output for every other harness from HOME, and `deinit --scope user` there deletes organization output | Reproduction R-F1: `deinit` exits 0 and removes the organization's Factory skill; a `sync` probe on `main` reports `Removed 2` for the user and organization Factory skills. Control `TestDeinitUserPreservesOrgOwnedArtifacts` passes. Fix: #750 | **Open** |
| FS09 | A1 / F5 | Repo commits a link below a harness root (for example `.claude/commands` pointing into HOME); the SDK publication step that sync uses writes through it | #463 admission, SDK root/parent refusal, #543, #544; `TestMaterialize_RejectsSymlinkedArtifactParent`, `TestSDKPublicationRefusesReplacedRootAndPreservesForeignStage`, `TestSDKStageCreationRefusesReplacedOutputRoot` | Fixed |
| FS10 | A33 / F25 | Repo commits `.factory` (or another harness root) as a link to `~/.factory`; sync writes and prunes the user's machine directory | #463, #464; `TestRunUnsafeRootShapesPreserveState` covers all 8 root contexts | Fixed |
| FS11 | A40 / F29 | Repo commits `.devin` as a link outside the checkout; Devin MCP and hook files are written there. A link to another directory inside the same repo (F29) stays allowed, because the repo author owns both | #463; `TestRunUnsafeRootShapesPreserveState` | Fixed |
| FS12 | F141 | Repo commits `.factory/hooks` as a link to `.git/hooks`; sync installs an executable Git hook that runs on the next checkout | #463; `TestRunUnsafeOutputAncestorPreservesState`; reproduction R-F141 refuses in both modes | Fixed |
| FS15 | A96 / F70 | Two selected registries ship Codex agents whose names differ only in case; on a case-insensitive disk one could take over the other's file | #732 refuses both; `TestCaseOnlyNameCollisionsAreRefusedPortably`, `TestSyncRefusesCaseOnlyNameCollisions` | Fixed |
| FS16 | F151 | Repo selects a resource named like one of the user's machine-wide resources; sync removed the user's copy from HOME | `2815ef93` (#417); `TestResolveDifferentRegistriesCollideOnlyWithinInstallScope`, `TestSyncCrossScopeSkillAndHookStayInSync` | Fixed |
| FS18 | A35 / F27 | Skill or hook support files that link to, for example, `~/.ssh/id_rsa` are copied into harness output in the checkout | #471, #473, #533-#540; `TestRunSupportCopyRefusesEscapingSourceLink`, `TestRunGeminiHookSupportSourceRefusalPreservesNativeContent` | Fixed |
| FS19 | A113 / F78 | Factory resource files that link outside their source are read into output | #477-#484; `TestLoadPrimarySourceRefusesForeignPayloadAlias`, `TestLoadFactoryUnsafeDirectoryAliases`, `TestSourceFSUnsafeDirectoryAliases` | Fixed |
| FS20 | A117 / F82 | Claude bundle and command files that link outside their source are read into output | Same as FS19 | Fixed |
| FS21 | A121 / F86 | Devin resource files that link outside their source are read into output | Same as FS19 | Fixed |
| FS23 | A29 | A Marketplace publisher's second resource overwrites the support files of another selected resource | `05eb5f32` (#195); `TestIndexOnlyStagingNamesSeparateSameKindResources` | Fixed |
| FS24 | A30 | A Marketplace artifact names an absolute local path as its source, so sync copies the user's local files instead of the verified ones | `05eb5f32` (#195), #451; `TestCanonicalArtifactDecodeRejectsAbsolutePaths`, `TestMaterializationSourcePathMarketplaceUsesResolvedPath` | Fixed |
| FS26 | A36 / F169 | Repo commits a native config file (for example `.factory/mcp.json`) as a link into HOME; sync copies its bytes into the checkout | #466, #495; `TestRunMarkerMergeRefusesExternalAlias`, `TestRunReadOnlyMarkerMergeRefusesExternalAlias` | Fixed |
| FS27 | F167 | The same with Claude's `.mcp.json` | #466, #495, #541, #601; reproductions R-F167 refuse for repository, user and SDK paths | Fixed |
| FS28 | A39 / F28 | Repo commits a native config file as a link to `/dev/zero` or a FIFO; sync hangs (denial of service only) | `780b326a` (#242), #497, #512; `TestAggregateConsumersRejectFIFO`, `TestRunNativeAggregateInventoryFIFO` | Fixed |
| FS29 | A42 | Repo commits `.devin/mcp_config.json` as a link to `~/.config/devin/mcp_config.json`; sync copies the user's Devin tokens into the checkout | #497, #498; `TestStoreReadAggregateRefusesIntermediateAlias`; reproduction R-A42 refuses | Fixed |
| FS33 | A34 / F26 | Repo commits `.agents` as a link into HOME; `deinit --scope repo` rewrites the user's machine manifest and prunes machine files | #474, #475; `TestRunPortableManifestAliasRefusalPreservesNativeOutput` | Fixed |
| FS38 | A41 | A new `.devin/mcp_config.json` holding secrets is created `0644`, readable by other local accounts | `7afe907b` (#191); `TestPrivateNewAggregateModeAndExistingMode` | Fixed |
| FS39 | A107 / F74 | The same for SDK MCP output | `7afe907b` (#191); `TestPrivateMCPCompileModes`, `TestMaterializePrivateMCPModes` | Fixed |

FS08 is the one open threat, and it is broader than the report. One target
set applied to a whole run and the repository list won it, so ordinary `sync`,
not only `deinit`, reconciled machine output against a repository's
`harnesses:` list. Benign team repositories trigger it too; the output returns
when the user syncs elsewhere. #750 makes target sets follow the layer that
selected the output: user and organization selections use the user list, then
the organization list, and fall back to the repository list only when neither
declares one.

## Not a Security Boundary

Each of these needs the user's own access or environment, or lets an author
affect only what they already control.

| Case | Report Aliases | Claim | Why It Is Not a Boundary | Status |
|---|---|---|---|---|
| FS01 | A19 | A broken `.git` makes the CLI treat the parent project as the repository | The CLI then writes only the parent project's own configured output; nothing attacker-authored lands anywhere. `9f4b7312` (#242) | Fixed anyway |
| FS02 | A20 / F15 | `migrate` rewrites user config through a repository link | `migrate` no longer exists (`consumer-config-format`) | Removed |
| FS13 | F145 | Repository and machine Gemini settings collide when the repository root is HOME | That layout is the user's own choice. `da3a07f2` (#438) | Fixed anyway |
| FS14 | F146 | A case-variant `CODEX_HOME` on a case-insensitive disk lets machine Codex output silently replace the repository's | The trigger is the user's own environment variable. Reproduction R-F146 confirms the silent overwrite | Bug |
| FS17 | F113 | `convert --sync` follows a linked output directory | `convert` was removed in #665 | Removed |
| FS22 | F131 | A registry `path` that is a link selects another directory of the same commit | Same author and same commit; `allowed_registries` matches URLs, not paths. Reproduction R-F131 | No action |
| FS25 | F129 | A ref like `foo/x/y` resolves inside the cached checkout of ref `foo` | Both refs belong to one allowed registry whose owner already controls each. Mutable cache only (TUI refresh, `update`); locked sync is unaffected. Reproduction R-F129 | Bug |
| FS30 | A109 / F75 | A concurrent edit between merge read and write is overwritten | The other writer is the user's own process. #601 | Fixed anyway |
| FS31 | F148 | Ownership `Apply` replaces a file another process just changed | Same as FS30. #522, #531, #586 | Fixed anyway |
| FS32 | A31 / F24 | Updating or retiring a managed Codex MCP server deletes a handwritten table placed between that server's tables | In the repository file the author owns the file anyway; in the user file it is the user's own layout. Still real data loss: reproduction R-A31 shows the table deleted while sync reports success | Bug |
| FS34 | A37 / F170 | A forged repository manifest nominates files for deletion | It can only nominate files inside harness output whose exact content it already knows. #485-#489, #598 | Fixed anyway |
| FS35 | A108 / F73 | SDK sync pruning deletes files in its output directory | Pruning stays inside the output directory the caller passes, and FS09/FS10 fence repo-derived roots. No untrusted input chooses what is pruned outside that directory. #549-#564, #600 | Fixed anyway |
| FS36 | A87 / F64 | A failed run leaves earlier artifacts published | Documented partial-result behavior, not a trust crossing | No action |
| FS37 | F144 | Selected Codex outputs stay installed when a duplicate MCP name is refused | The refusal is scoped to the duplicate; installing the other selected resources is intended. Reproduction R-F144 | No action |
| FS40 | F124 | A planted backup archive replaces the harness root with a link | Planting needs write access to the user's backup directory. Restore refuses it anyway (reproduction R-F124) | Fixed anyway |
| FS41 | F125 | A planted Codex backup manifest authorizes arbitrary restore paths | Same precondition as FS40 | No action |
| FS42 | F126 | A planted ordinary backup overwrites harness files on restore | Same precondition as FS40; restore is an explicit overwrite. Reproduction R-F126 | No action |
| FS43 | F133 | A forged dependency journal deletes a file | Needs write access to the user's state directory or control of their environment | No action |
| FS44 | F147 | A forged ownership journal deletes a file | Same as FS43. #516 | Fixed anyway |

The three bugs are ordinary defects, not R2 gates:

- **FS32** (#746) loses user data and is the most worth fixing. `codexMCPSections`
  spans from a server's first table to its last, swallowing tables in between.
  A safety backup keeps the prior bytes.
- **FS14** (#747): Codex partition checks compare path strings, not files.
- **FS25** (#748): the mutable cache accepts a cache hit inside another ref's
  checkout.

## Closure

- [x] Freeze both source hashes, the 211-ID index and FS01-FS44.
- [x] Classify every case against the threat model with baseline evidence.
- [ ] Merge #750 (FS08): failing-first `sync` and `deinit` regressions, an
  archived `resource-selection` requirement, `make validate`, `make spec-check`,
  `make validate-e2e` and an `open-code-review-delegate` review. Rerun R-F1 on
  `main` after merge.
- [ ] Close #431 and update #215.

The three bugs are tracked in #746, #747 and #748. The one unresolved review thread on the
merged R2 PRs (#601, a wording note on a `materialization` scenario) is a spec
nit, not R2 work.

## Evidence

All checks ran on the baseline on 2026-10-18, macOS arm64, `CGO_ENABLED=0`.

- **Witnesses.** The 80 tests in
  [`witnesses.txt`](security-r2-2026-10-18/witnesses.txt) ran with
  `go test -json -count=1` (not short mode). Each reported `pass`, and none
  skipped.
- **Reproductions.** Sources are in
  [`security-r2-2026-10-18/repro/`](security-r2-2026-10-18/repro/). Fixtures are
  inert local directories and repositories; nothing executes a resource payload
  or contacts a network service. Every probe asserts its outcome. A failing
  test with a `REPRODUCED` message confirms the claim; a passing test confirms
  the refusal (diagnostic, preserved links and bytes) or the stated behavior.
  After review, all probes ran again on the baseline with these assertions and
  gave the results below. R-F146's source is unchanged; its case-insensitive
  result comes from the first run.

| Reproduction | Package | Result |
|---|---|---|
| R-F1 `TestR2LedgerF1DeinitHarnessNarrowing` | `cmd/agent-marketplace` | Fails: FS08 confirmed |
| R-F114 `TestR2LedgerF114LockBootstrapThroughConfigSymlink` | `cmd/agent-marketplace` | Passes: refused |
| R-F141 `TestR2LedgerF141FactoryHooksIntoGitHooks` | `internal/materialize` | Passes: refused |
| R-F167 `TestR2LedgerF167ClaudeMCPExternalAlias` | `internal/materialize` | Passes: refused |
| R-F167 `TestR2LedgerF167SDKMergeLeafAlias` | `pkg/harness-compilation` | Passes: refused |
| R-A42 `TestR2LedgerA42DevinMCPLeafAlias` | `internal/materialize` | Passes: refused |
| R-F146 `TestR2LedgerF146CaseAliasedCodexHome` | `internal/materialize` | Fails on case-insensitive APFS: FS14 confirmed (the `.CODEX` subtest skips on case-sensitive volumes) |
| R-A31 `TestR2LedgerA31CodexInterleavedTable{,Run}` | `internal/materialize` | Fails: FS32 confirmed |
| R-F144 `TestR2LedgerF144CodexPartialOutputs` | `internal/materialize` | Passes: scoped refusal |
| R-F129 `TestR2LedgerF129NestedRefCacheAlias` | `internal/cache` | Fails: FS25 confirmed |
| R-F131 `TestR2LedgerF131ConfiguredRootAlias` | `internal/sourcerevision` | Passes: link inside the snapshot accepted, escaping link refused |
| R-F124 `TestR2LedgerF124RestoreRootLink` | `internal/backup` | Passes: refused |
| R-F126 `TestR2LedgerF126RestoreOverwritesExisting` | `internal/backup` | Passes: planted archive overwrites, as described |

To rerun one, copy the source into its package and run it there. A source
file's name is its package path with `/` replaced by `_`. For example, from a
checkout of the baseline:

```bash
cp docs/issues/security-r2-2026-10-18/repro/internal_cache_r2ledger_repro_test.go.txt \
  internal/cache/zz_r2ledger_repro_test.go
CGO_ENABLED=0 go test ./internal/cache -run '^TestR2Ledger' -count=1 -v
```

For R-F146 on macOS, mount a case-insensitive APFS image and set `TMPDIR` to a
directory on it. Do not commit the copied files.

- **Scoped gate.** `make validate` with the 15 witness packages and the
  witness-name filter passed: short-mode tests, `go vet`, CI lint (0 issues)
  and the build. This is not a full-suite result.
- **Review threads.** The 95 merged R2 PRs have 296 review threads, and only
  the #601 thread is unresolved. The other cited PRs (#191, #195, #242, #417,
  #438, #451, #665, #713) have 16 threads, none unresolved.
- **Reconciliation.** The check below passes against both exact exports, also
  under `python3 -O`. Negative controls reject swapped exports, an altered index
  alias, a missing case row, a moved status, tampered index bytes and a
  missing R5 row.

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


def check(condition, message):
    # Explicit checks stay active under python -O, unlike assert.
    if not condition:
        sys.exit("FAIL: " + message)


docs = Path("docs/issues")
index = docs / "2026-10-18-security-r2-source-index.csv"
ledger = docs / "2026-10-18-security-r2-frozen-ledger.md"
r5_index = docs / "2026-10-16-security-r5-source-index.csv"
check(hashlib.sha256(index.read_bytes()).hexdigest() ==
      "8ce78a2598979dc7787c41f6cd8ebf2bbcc62a8eee1c35e3c129e93238d9c931",
      "source index digest")
with index.open(newline="") as stream:
    rows = list(csv.DictReader(stream))
by_id = {row["instance_id"]: row for row in rows}
check(len(rows) == len(by_id) == 211, "source index has 211 distinct IDs")
expected = {}
sources = [
    ("a_ref", "A", 0, 131, 6921063,
     "67554afc2e08a359137bc66899a17547d812ca2d5022dfa08065753fa8d1690b"),
    ("f_ref", "F", 1, 173, 9141312,
     "91906e9899dbc253d659ba8b87b7fdb03b837aa0d1f4a24a160d776594379e98"),
]
check(len(sys.argv) == 3, "supply the A and F export paths, in that order")
for filename, (column, prefix, offset, count, size, digest) in zip(sys.argv[1:], sources):
    data = Path(filename).read_bytes()
    check(len(data) == size and hashlib.sha256(data).hexdigest() == digest,
          f"export {prefix} size and digest")
    sarif = json.loads(data)
    check(sarif["version"] == "2.1.0" and len(sarif["runs"]) == 1, f"export {prefix} shape")
    results = sarif["runs"][0]["results"]
    check(len(results) == count, f"export {prefix} result count")
    seen = set()
    for number, result in enumerate(results, offset):
        identity = result["properties"]["instance_id"]
        check(identity not in seen, f"duplicate ID in export {prefix}")
        seen.add(identity)
        check(result["partialFingerprints"]["staticFingerprint"] == identity,
              f"{prefix}{number} fingerprint")
        expected.setdefault(identity, {"a_ref": "", "f_ref": ""})[column] = prefix + str(number)
check(set(expected) == set(by_id), "index IDs match the exports")
for identity, aliases in expected.items():
    check(all(by_id[identity][key] == value for key, value in aliases.items()),
          f"aliases of {identity}")
with r5_index.open(newline="") as stream:
    r5 = {row["instance_id"]: row["scope"] for row in csv.DictReader(stream)}
check(r5 == {i: row["r5_scope"] for i, row in by_id.items()},
      "r5_scope matches every R5 index row")
for row in rows:
    if row["r5_scope"].startswith("R") and row["r5_scope"][1:].isdigit():
        check(row["scope"] in ("r5", "FS28"), f"R5 case {row['r5_scope']} routed to R5")
cases = {f"FS{number:02}" for number in range(1, 45)}
counts = Counter(row["scope"] for row in rows)
check(counts == Counter({**dict.fromkeys(cases, 1), "r1": 21, "r3": 32,
                         "r4": 35, "r5": 60, "parent": 19}), "scope counts")
text = ledger.read_text()
row_re = re.compile(r"^\| (FS\d{2}) \| ([^|]+) \|(?:[^|]*\|){2} ([^|]+) \|$", re.M)
def section(title):
    body = text.split("## " + title + "\n", 1)[1].split("\n## ", 1)[0]
    return row_re.findall(body)
threats, others = section("Real Threats"), section("Not a Security Boundary")
check(len(threats) == 25 and len(others) == 19, "section row counts")
check({case for case, _, _ in threats + others} == cases, "every case listed once")
for case, aliases, _ in threats + others:
    row = next(row for row in rows if row["scope"] == case)
    check(set(re.findall(r"[AF]\d+", aliases)) ==
          {row[key] for key in ("a_ref", "f_ref") if row[key]}, f"{case} aliases")
status = lambda part: Counter(s.strip("* ") for _, _, s in part)
check(status(threats) == Counter({"Fixed": 24, "Open": 1}), f"threat statuses {status(threats)}")
check(status(others) == Counter({"Fixed anyway": 8, "No action": 6, "Bug": 3, "Removed": 2}),
      f"non-boundary statuses {status(others)}")
print("PASS: 304 occurrences, 211 IDs, 25 threats, 19 non-boundary, 167 routed")
PY
```

[#215]: https://github.com/Adyen/agent-marketplace/issues/215
[#431]: https://github.com/Adyen/agent-marketplace/issues/431
[#432]: https://github.com/Adyen/agent-marketplace/issues/432
[#433]: https://github.com/Adyen/agent-marketplace/issues/433
[#434]: https://github.com/Adyen/agent-marketplace/issues/434
[baseline]: https://github.com/Adyen/agent-marketplace/commit/299bf2aba16e711663718a22057a7b3bfb085af8
