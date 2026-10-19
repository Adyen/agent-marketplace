# #215 Frozen Scope and Evidence Ledger

Parent: [#215]. Frozen: 2026-10-19.
Code baseline: [`f44101fa0203736fe4ff6202ad23215ab42b02a6`][baseline] (v5.0.0).

This ledger closes the last unledgered part of both reports: the 21 `r1` IDs
(selection, acquisition and consent) and the 19 `parent` IDs (hosted service,
telemetry, PATH and operator) that the [R2 source index] routed back to #215.
R2 ([#431]), R3 ([#432]) and R4 ([#433]) are closed. R5 ([#434]) keeps its
own frozen ledger; this document only records how its rows split between a
security track and a new performance track.

## Threat Model

The rule is R2's: the boundary is **content the user did not write**. That is
a repository checkout the user runs the CLI in, registry or Marketplace content
the user selected, and other OS accounts on the same machine. A case is a real
threat only when such input makes the CLI do something to the user that the
input's author could not already do: write, delete or overwrite files outside
the checkout, expose the user's files or secrets, install something that
executes, remove or replace tooling the user or organization installed
machine-wide, keep content the organization has blocked, or hide executable
content from the review the user or organization relies on. The user's own HOME, state, environment and configuration are
not boundaries (R3.4 in [#432]).

Two consequences decide most cases in this scope:

1. **Repository scope is the repository author's.** A repository author can
   already commit hooks and MCP servers into the checkout. The CLI writes no
   harness approval or trust state (Claude MCP approvals, Codex project trust,
   Gemini folder trust), so a generated project file gets the same harness
   prompts as a committed one. A repository's configuration selecting attacker
   content *into repository scope* is therefore not a boundary. Harm starts
   when it reaches machine scope, removes or replaces organization tooling, or
   bypasses a lock or organization policy.
2. **Marketplace consent is not a boundary against a publisher.** Consent
   covers analyzer findings, and findings are heuristics a malicious publisher
   can avoid. Capabilities on their own never require consent, and `update`
   already advances to newer revisions with warning findings without asking
   (X1). What does bind a publisher is enforced: exact digests, revocation and
   the operator's `reject` decisions.

For the hosted Marketplace the same rule applies with its parties: registry
contributors and catalog callers are untrusted; the operator and its
deployment controls are trusted (see the [trust model]). A detection gap in an
analyzer is not a boundary; overwriting or ignoring a decision the operator
already made is.

## Result

| Section | Cases | Status |
|---|---:|---|
| Real threats | 12 | 9 fixed, **3 open** (P09, P11, P19) |
| Not a security boundary | 28 | 4 fixed anyway, 16 no action, 2 removed, 6 ordinary bugs |

All three open threats are reproduced on the baseline. P11 and P19 need no
policy decision; P09 needs the withdrawal contract in #NEW-A49.

## Source Provenance

The labels follow the R2 and R5 ledgers: `A<n>` is the 2026-09-17 export,
zero-based; `F<n>` is the 2026-09-24 export, one-based. Both files declare SARIF
2.1.0 and `depthfirst-code-scanner` 1.0.0, with no version-control provenance.

| Export | Supplied Filename | Bytes | Results | SHA-256 |
|---|---|---:|---:|---|
| A | `beyildirim-agent-marketplace-vulnerabilities.sarif.json` | 6921063 | 131 | `67554afc2e08a359137bc66899a17547d812ca2d5022dfa08065753fa8d1690b` |
| F | `beyildirim-agent-marketplace-vulnerabilities.sarif (3).json` | 9141312 | 173 | `91906e9899dbc253d659ba8b87b7fdb03b837aa0d1f4a24a160d776594379e98` |

No new index is needed. The unchanged [R2 source index] (SHA-256
`8ce78a2598979dc7787c41f6cd8ebf2bbcc62a8eee1c35e3c129e93238d9c931`) already maps
all 211 IDs; P01-P40 below are exactly its 40 `r1` and `parent` rows, in report
order. The check at the end verifies this.

## Real Threats

| Case | Report Aliases | Route | Untrusted Input and Harm | Evidence on the Baseline | Status |
|---|---|---|---|---|---|
| P03 | A15 | r1 | A registry commit whose root is an empty container: its nested hooks and MCP servers were left out of the lock's source-revision inventory, so a reviewed lock change did not show executable resources that sync then installed | `3d8ac4f4` from #242; `TestNestedRevisionInventoryMatchesDiscovery`, `TestLockedNestedHookAndMCPInventory` | Fixed |
| P09 | A49 / F34 | r1 | The organization blocks a registry during an incident. Every `sync` then refuses and leaves that registry's installed output in place, and removing another registry entry rebuilds the blocked output from cache. The organization cannot withdraw tooling it has blocked | Reproduction R-A49: after the block, `sync` fails with `Installed 0  Removed 0  Malformed 0  Errors 1` and the skill stays; after deleting it, `remove --from <other registry>` restores it (a named `remove <name> --from` does not). No new content is fetched (#216; `TestResolveBlockedRegistryRemovalDoesNotAcquire`, `TestRemoveBlockedRegistryRefusesNewAcquisition`) | **Open** (#NEW-A49) |
| P10 | A50 / F35 | r1 | A repository, or another lower-precedence selection, selects a resource named like a member of a required organization collection, and the member is suppressed | #426, #684; `TestResolveRequiredCollectionSelectionSuppressesRepositorySource`, `TestResolveRequiredCollectionMemberDisplacesOptionalSameScopeSelection`, `TestSyncRequiredCollectionMemberDisplacesUserSelection` | Fixed |
| P11 | A51 / F33 | r1 | A command the user selected from a registry or Marketplace, named like an organization-required skill, takes that skill's `skills/<name>/SKILL.md` on Devin and Codex. Sync reports success | Reproduction R-A51: replaced in 3 of 4 runs (symlink mode when the organization source's cache path sorts first, file mode in both orders), with no warning or error | **Open** (#759) |
| P12 | A65 | r1 | A repository commits two Marketplace pins whose colon-joined TUI keys collide; enabling the benign row globally writes the other pin into user configuration | NUL-separated keys over validated segments (`config.MarketplaceCatalogKey`); `TestMarketplaceCatalogKeyDimensionsAndAliases`, `TestMarketplaceSelectionIndexKeepsDistinctPins` | Fixed |
| P13 | A67 | r1 | Enabling one side of a TUI name collision globally copies the repository's rival registry into user configuration | #195 (`competingRowsInScope`), #320 (lookup-only edits); `TestCompetingRegistryCleanupIsScopeBound`, `TestCompetingRegistryCleanupV4`, `TestValidateToggleRegistryWriteScopeBoundary` | Fixed |
| P14 | A85 | r1 | A mirror Marketplace serves a payload whose resource name differs from the pin, so its output lands under another resource's name | #195 (`validateDecodedArtifactIdentity`); `TestResolverRejectsMirrorPayloadIdentityAndInvalidSource` | Fixed |
| P16 | F118 | r1 | `doctor --repair` resolved current refs instead of the lock, so a newly pushed MCP command entered an aggregate the lock did not pin | #293; `TestDoctorRepairUsesExactGitLockWithoutRefreshingMutableCache`, `TestDoctorRepairReplacesConflictFromLockedGitRevision` | Fixed |
| P19 | F149 | r1 | `update --bump` reads an exact commit pin that ends in digits as a version. A tag named `<commit prefix><larger number>` replaces the pin, and the registry owner's new content is synced | Reproduction R-F149: the configured ref is rewritten to the tag and the replacement skill is installed | **Open** (#758) |
| P20 | F150 | r1 | A repository's top-level collection name disables a required organization registry's automatic selection | #438; `TestRequiredRegistrySurvivesTopLevelCollectionPropagation`, `TestRequiredAutoSelectionStillReportsMissingTopLevelCollection` | Fixed |
| P27 | A78 | parent | An approved registry origin redirects the worker's fetch; credentials go to the redirect target and its repository is published under the approved identity | #222 (`http.followRedirects=false`, origin-scoped headers); `TestCloneRejectsAuthenticatedHTTPRedirects` | Fixed |
| P28 | A82 / F60 | parent | A stale worker republishes a rejected executable artifact as allowed | #440; `TestStaleSameCommitPublicationDoesNotReplaceCurrentAssessment` (SQLite here; #440 also ran it against PostgreSQL 17.11) | Fixed |

P11 and P19 have the same shape as other fixed threats: P11 removes
organization tooling like P10 and FS08, and P19 bypasses an exact pin like
P16. In P11 the resolver's projection keys collisions by kind and name, so a
skill and a command never compete, although Devin and Codex write both to the
same directory. File mode even writes both and counts them (`Installed 4`).

P03 and P09 are maintainer decisions (2026-10-19). P03 counts because hiding
executable resources from the lock a team reviews is the behavior the lock
exists to prevent, even though the registry author already controlled them.
P09 counts because an organization block is the incident-response control: it
must withdraw installed output, not only refuse new work. Only **organization**
policy may withdraw. A repository or user `allowed_registries` that removed
machine output would reopen FS08, so those layers keep today's refusal.

## Not a Security Boundary

| Case | Report Aliases | Route | Claim | Why It Is Not a Boundary | Status |
|---|---|---|---|---|---|
| P01 | A2 / F6 | r1 | A rerun with `--accept-risk` accepts whatever revision the unpinned locator resolves to then | Consent is not a boundary against a publisher (threat model, point 2). The flags are documented as applying to the revision verified in that run, and `name@sha256:<digest>` pins one exactly | No action |
| P02 | A3 | r1 | A first install from an unlocked mutable ref can lock a newer commit than the one discovered | The registry owner controls both commits, and no pin existed yet | Fixed anyway |
| P04 | A18 / F14 | r1 | A configured install edits a same-URL, same-path entry at another ref | The other entry is the repository author's and the edit stays in repository scope. The v4 editor's source identity now includes the ref | Fixed anyway |
| P05 | A23 / F18 | r1 | `repo` and `repo.git` share one lock identity but can fetch different routes | Needs a hostile repository or personal entry plus a server that routes the two spellings differently; the effect stays in that entry's own scope | No action |
| P06 | A32 | r1 | Two paths of one registry ship same-name Devin MCP servers, and the first wins | Both paths are selected by the user's configuration. A deterministic winner with a collision warning is the documented contract, and required sources still win | Fixed anyway |
| P07 | A43 | r1 | A repository-selected Git hook or MCP server beats the Marketplace artifact the user reviewed | It lands in repository scope, which the repository author controls | Fixed anyway |
| P08 | A45 / F30 | r1 | A web URL `tree/release/v1/...` resolves ref `release` | The registry owner controls both refs, as in FS25. It still installs the wrong content silently | Bug |
| P15 | F2 | r1 | Name-based removal edits a decoy entry and leaves the resource enabled | The decoy, the resource and the scope all belong to the repository author | No action |
| P17 | F138 | r1 | Legacy v2/v3 installs clear `exclude_all` | The v2/v3 editors no longer exist (`consumer-config-format`) | Removed |
| P18 | F143 | r1 | Codex repair shows one of two duplicate MCP identities and installs the other | The user selected both; ordinary sync refuses the duplicate, and repair asks per entry | No action |
| P21 | F155 | r1 | A TUI collection collision creates a user shadow entry with inherited selectors | The precondition is a legacy v2/v3 user configuration | Removed |
| P22 | A0 / F4 | parent | `doctor` runs `devin --version` from `PATH` | The user's own `PATH`. Go's `exec.LookPath` refuses current-directory matches, and every command resolves `git` the same way | No action |
| P23 | A60 / F41 | parent | User configuration or environment enables community telemetry | The user's own configuration (R3.4). Repository layers cannot set telemetry | No action |
| P24 | A62 / F43 | parent | A malformed personal configuration disables organization telemetry for a run | Same as P23 | No action |
| P25 | A72 / F49 | parent | A slow approved origin exhausts the worker's lease retries | Availability of the operator's deployment; no user file, secret or execution is involved | No action |
| P26 | A74 / F53 | parent | The manifest route serves a manifest that no current revision owns | Needs the exact SHA-256 of bytes that no current revision publishes. It contradicts the trust model ("a digest that no published revision owns returns 404") | Bug |
| P29 | A92 / F68 | parent | Decoded text from Gemini inline MCP fields is copied into public findings | The value is the contributor's own and is already in the published source. It contradicts the analyzer's rule that MCP credential fields never reach findings | Bug |
| P30 | A127 / F90 | parent | Server-rendered errors name the internal API origin | Hostname disclosure only | No action |
| P31 | A129 / F92 | parent | Large raster previews are expensive to decode | Costs only the viewer's browser tab | No action |
| P32 | A130 / F93 | parent | The bundled Nginx profile has no rate limits | Operator deployment policy | No action |
| P33 | F3 | parent | In file mode the content-digest route serves unowned blobs | Same as P26 | Bug |
| P34 | F111 | parent | The Compose proxy has no concurrency limits | Same as P32 | No action |
| P35 | F112 | parent | Worker egress can reach private addresses through an approved host's DNS | Needs control of an approved registry's DNS; limiting worker egress is an operator responsibility | No action |
| P36 | F158 | parent | In file mode, inspection routes serve blocked resources | Artifact downloads refuse blocked and revoked revisions in both modes (`writeArchiveAvailability`), so nothing blocked can be installed. It contradicts the documented block and revoke contract | Bug |
| P37 | F159 | parent | OIDC callback replay repeats token exchanges | Availability only; it grants no access | No action |
| P38 | F160 | parent | Support files with invalid UTF-8 skip the text scanners | A detection gap: a malicious contributor does not need to evade a heuristic | No action |
| P39 | F161 | parent | In file mode, inspection routes serve blocked and revoked revisions | Same as P36 | Bug |
| P40 | F163 | parent | The MCP analyzer scans only the server name | Intentional, to keep credentials out of findings; CLI output escapes controls and bidi characters (`QuoteTerminal`) | No action |

Evidence for the fixed rows: P02 #229, #230 (`TestConfiguredInstallFreezesDiscoveryBeforePublication`,
`TestAdHocInstallPreparationPinsOriginalMutableRef`);
P04 `v4EditSourcePhysicalMatches` in `internal/config/v4_editor.go` (the witness
`TestLayerEditorRemoveSourcePreservesSiblings` covers the removal path only);
P06 `resolver.MaterializationWinners` (`TestMaterializationWinnersLegacyEmptyPriorityAndOrdering`);
P07 #264 (`TestMarketplaceResolvedCollision`).

The six bugs are ordinary defects, not #215 gates. They group into three
issues: #760 (P26, P33, P36, P39: digest and inspection routes serve
content that no current revision owns and, in file mode, blocked or revoked
content), #761 (P08) and #762 (P29).

### Found During Review, Outside the Reports

**X1.** The [trust model] says the CLI shows findings and asks before it
installs a revision with security findings. `update` advances writable
Marketplace pins to newer revisions whose assessment is `warn` without showing
or confirming anything (`validateMarketplaceUpdateArtifact` in
`internal/pipeline/update.go`). This is not a boundary, for the reason given in
P01, but the page and the behavior disagree. Tracked in #763 as a product
decision: show and confirm new findings on `update`, or correct the page.

## Adversarial Review

The classifications were challenged before freezing:

- **Does generated repository output gain trust a committed file would not?**
  No. The CLI writes no harness approval or trust state. Repository layers
  cannot set backup, logging, telemetry or update checks
  (`validateLayerFields`); a repository's `materialization` mode applies only to
  project scope; and since #750 a repository's `harnesses:` list no longer
  narrows machine output. Point 1 of the threat model holds.
- **Is consent a documented security promise?** Partly: the trust model page
  promises confirmation, and `update` does not provide it. That is X1. P01 is
  unchanged, because a publisher who can move a revision between two runs can
  also publish it without findings.
- **Is P28 consistent with P38 and P40?** Yes, by an explicit rule: overwriting
  a `reject` the operator already decided is a boundary; a scanner that does
  not fire is detection.
- **Did a "not a boundary" call hide a broken contract?** For six rows in three
  groups it did: P08, P26/P33/P36/P39 and P29 moved from "no action" to "bug".
  They stay out of the security count.
- **Does every real threat pass "could the author already do this?"** P03 did
  not, because the registry author already controls every resource a
  `default` or `required` selection accepts. Review moved it to "fixed anyway";
  the maintainer moved it back, since review visibility is part of the harm
  (threat model). P09 was "no action" because nothing new is fetched; the
  maintainer moved it to real, since the organization must be able to withdraw.
- **Was any "needs a probe" case quietly closed?** No. P11 was the open
  question; its reproduction confirmed it, so it moved to Real/Open. P19's
  earlier helper-only result was replaced by an end-to-end reproduction. P09's
  two claims were probed separately: the block keeping output reproduces, and
  so does the rebuild through a whole-entry `remove --from`; a named removal
  does not rebuild.
- **Could P09's fix create a new FS08?** Yes, if any layer's allowlist could
  withdraw: a repository may set `allowed_registries` when the organization and
  user do not. Withdrawal is therefore organization-only (#NEW-A49).
- **Do the fixed rows rest on current tests?** All 24 witnesses ran fresh on
  the baseline and passed. P28's witness uses SQLite; the PostgreSQL run is
  #440's.

Judgement calls: P03 and P09 are decided above. Still open for the maintainer:

- P38: one invalid byte turns off text scanning for a whole file.
- P18: the repair prompt names the wrong resource of a duplicate pair.

Not covered: no audit beyond the 40 IDs; vendor harness trust prompts were not
probed, so point 1 rests on the CLI writing no trust state; PostgreSQL was not
rerun here.

## R5 Split

Direction from the maintainer (2026-10-19): R5 continues, and most of it moves
to a new **performance track** (#765). The [R5 ledger] and its case IDs
stay unchanged.

A row stays in the **security track** only when content the user did not write
makes the user's own machine pay far more than the input's size (super-linear
expansion or decompression) or hang without progress. Proportional cost (big
input, big cost), the hosted service's capacity, and plain speed-ups belong to
the performance track.

| Track | Cases | Count |
|---|---|---:|
| Security | R03, R06, R08, R11, R30, R49, R51, R61 | 8 |
| Performance | R01, R02, R04, R05, R07, R09, R10, R12, R13, R14, R15, R16, R17, R18, R19, R20, R21, R22, R23, R24, R25, R26, R27, R28, R29, R31, R32, R33, R34, R35, R36, R37, R38, R39, R40, R41, R42, R43, R44, R45, R46, R47, R48, R50, R52, R53, R54, R55, R56, R57, R58, R59, R60 | 53 |

| Case | Report Aliases | Untrusted Input and Cost | Status |
|---|---|---|---|
| R03 | A17 | A repository's include files were read without a bound | Closed: the existing 4 MiB reader is revalidated in the [R5 execution report] |
| R51 | F134 | A repository's include graph expands a shared fragment once per branch; only cycles are refused | Verify; a budget needs D2 |
| R11 | A52 / F171 | A registry's collection graph is expanded per branch by the CLI resolver | Verify; D2 |
| R30 | A91 / F67 | The same expansion in the library used for Marketplace collections | Verify; D2 |
| R06 | A26 / F21 | Marketplace nested collection installation | Verify; D2 |
| R49 | F123 | Marketplace collection preparation before consent | Verify; D2 |
| R08 | A28 / F23 | Leaf acquisition during lock bootstrap; in scope only if acquisitions repeat per expanded path | Verify; D2 |
| R61 | F172 | A 64 MiB artifact decompresses into members of up to 64 MiB each with no running total. `DefaultArchiveLimits` (64 MiB total, 1024 files) are checked only after the whole archive is decoded | Open; enforce the existing limits during decoding, so no new budget is needed |

Consequences: R5's D1 is not needed for R61, and D3-D6 move with their rows to
the performance track. Every merged R5 slice (#725, #730, #736, #742, #743,
#745, #749, #751) and the R38/R44 preview work in progress are performance
work. They keep their measurements, but they are not security closure
evidence.

## What This Changes in #215

- **Decision (2), cross-invocation consent:** not a boundary (P01). X1 tracks
  the `update` and documentation mismatch.
- **Decision (45), ambiguous web refs:** not a boundary (P08); tracked as a bug.
- **New processing budgets:** only D2, for the R5 security graph rows.
- **R1 blocked-registry carry-over:** withdrawing a blocked registry's output is
  now security (P09, #NEW-A49). Making `remove` of a blocked registry touch only
  its own output for every artifact kind protects the user's own edits; that
  part stays robustness in #764.
- **Hosted-service follow-ups:** secret exposure is P29, redirects P27, stale
  assessments P28, images P31, and lease and telemetry authority P25, P23 and
  P24.

## Closure

- [x] Freeze both source hashes and P01-P40 against the unchanged R2 index.
- [x] Classify every case against the threat model with baseline evidence, and
  reproduce all three open threats.
- [ ] Fix P19 (#758), P11 (#759) and P09 (#NEW-A49): failing-first regressions, an
  archived OpenSpec delta, `make validate`, `make spec-check`,
  `make validate-e2e` and an `open-code-review-delegate` review. Rerun R-F149,
  R-A51 and R-A49 on `main` after each merge.
- [ ] Close the R5 security track: R61 and the D2 graph rows in [#434].
- [ ] Update #215 and close it.

The bugs (#760, #761, #762), X1 (#763), the R1
carry-over (#764) and the performance track (#765) do not gate #215.

## Evidence

All checks ran on the baseline on 2026-10-19, macOS arm64, `CGO_ENABLED=0`.

- **Witnesses.** The 24 tests in
  [`witnesses.txt`](security-215-2026-10-19/witnesses.txt) ran with
  `go test -json -count=1` (not short mode). Each reported `pass`, and none
  skipped.
- **Reproductions.** The source is
  [`security-215-2026-10-19/repro/`](security-215-2026-10-19/repro/). Fixtures
  are local bare repositories with inert skills and commands; nothing executes
  a resource or contacts a network service. A failing test with a `REPRODUCED`
  message confirms the claim.

| Reproduction | Package | Result |
|---|---|---|
| R-F149 `TestLedger215F149UpdateBumpReplacesCommitPin` | `cmd/agent-marketplace` | Fails: P19 confirmed (`refs bumped: 1`, replacement content installed) |
| R-A51 `TestLedger215A51RequiredSkillVersusSameNameCommand` | `cmd/agent-marketplace` | Fails: P11 confirmed in `org-sorts-first/symlink`, `org-sorts-first/file` and `user-sorts-first/file`; `user-sorts-first/symlink` keeps the required skill |
| R-A49 `TestLedger215A49OrganizationBlockKeepsInstalledOutput` | `cmd/agent-marketplace` | Fails: P09 confirmed (blocked output stays; whole-entry `remove --from` rebuilds it; named removal does not) |

To rerun them, copy the source into its package from a checkout of the
baseline:

```bash
cp docs/issues/security-215-2026-10-19/repro/cmd_agent-marketplace_215ledger_repro_test.go.txt \
  cmd/agent-marketplace/zz_215ledger_repro_test.go
CGO_ENABLED=0 go test ./cmd/agent-marketplace -run '^TestLedger215' -count=1 -v
```

Do not commit the copied file.

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
r5_index = docs / "2026-10-16-security-r5-source-index.csv"
ledger = docs / "2026-10-19-security-215-frozen-ledger.md"
check(hashlib.sha256(index.read_bytes()).hexdigest() ==
      "8ce78a2598979dc7787c41f6cd8ebf2bbcc62a8eee1c35e3c129e93238d9c931",
      "R2 source index digest")
check(hashlib.sha256(r5_index.read_bytes()).hexdigest() ==
      "e96e2bf20fe84944e8a7a5c4f0739c721fb11bb0463ea9512f27d6b86b430d3e",
      "R5 source index digest")
with index.open(newline="") as stream:
    rows = list(csv.DictReader(stream))
by_id = {row["instance_id"]: row for row in rows}
check(len(rows) == len(by_id) == 211, "R2 index has 211 distinct IDs")
check(len(sys.argv) == 3, "supply the A and F export paths, in that order")
expected = {}
sources = [
    ("a_ref", "A", 0, 131, 6921063,
     "67554afc2e08a359137bc66899a17547d812ca2d5022dfa08065753fa8d1690b"),
    ("f_ref", "F", 1, 173, 9141312,
     "91906e9899dbc253d659ba8b87b7fdb03b837aa0d1f4a24a160d776594379e98"),
]
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


def number(alias):
    return int(alias[1:]) if alias else 10**6


owned = sorted((row for row in rows if row["scope"] in ("r1", "parent")),
               key=lambda row: (row["scope"] != "r1", number(row["a_ref"]), number(row["f_ref"])))
check(Counter(row["scope"] for row in owned) == Counter({"r1": 21, "parent": 19}), "#215 scope counts")
cases = {f"P{n:02}": row for n, row in enumerate(owned, 1)}
text = ledger.read_text()
row_re = re.compile(r"^\| (P\d{2}) \| ([^|]+) \| (r1|parent) \|(?:[^|]*\|){2} ([^|]+) \|$", re.M)


def section(title):
    body = text.split("## " + title + "\n", 1)[1].split("\n## ", 1)[0]
    return row_re.findall(body)


threats, others = section("Real Threats"), section("Not a Security Boundary")
check(len(threats) == 12 and len(others) == 28, "section row counts")
listed = [case for case, _, _, _ in threats + others]
check(len(listed) == len(set(listed)) and set(listed) == set(cases), "every case listed once")
for case, aliases, route, _ in threats + others:
    row = cases[case]
    check(route == row["scope"], f"{case} route")
    check(set(re.findall(r"[AF]\d+", aliases)) ==
          {row[key] for key in ("a_ref", "f_ref") if row[key]}, f"{case} aliases")
# Unlisted threats are Fixed; unlisted non-boundary cases are No action.
status = {"P09": "Open", "P11": "Open", "P19": "Open", "P02": "Fixed anyway",
          "P04": "Fixed anyway", "P06": "Fixed anyway", "P07": "Fixed anyway",
          "P17": "Removed", "P21": "Removed", "P08": "Bug", "P26": "Bug", "P29": "Bug",
          "P33": "Bug", "P36": "Bug", "P39": "Bug"}
tally = {}
for title, part, default in (("Real threats", threats, "Fixed"),
                             ("Not a security boundary", others, "No action")):
    for case, _, _, value in part:
        value = re.sub(r"\s*\(.*\)$", "", value.strip("* ")).strip("* ")
        check(value == status.get(case, default), f"{case} status {value!r}")
        tally.setdefault(title, Counter())[value] += 1
words = {"Fixed": "fixed", "Open": "open", "Fixed anyway": "fixed anyway",
         "No action": "no action", "Removed": "removed", "Bug": "ordinary bugs"}
for title, counts in tally.items():
    summary = re.search(rf"^\| {title} \| (\d+) \| ([^|]+) \|$", text, re.M)
    check(summary and int(summary.group(1)) == sum(counts.values()), f"{title} total")
    claimed = {word: int(n) for n, word in re.findall(r"(\d+) ([a-z ]+?)(?:\*\*)?(?: \(|,|$)",
                                                       summary.group(2).replace("**", "").strip())}
    check(claimed == {words[key]: n for key, n in counts.items()}, f"{title} summary {claimed}")
with r5_index.open(newline="") as stream:
    r5 = {row["scope"]: row for row in csv.DictReader(stream) if re.fullmatch(r"R\d{2}", row["scope"])}
check(set(r5) == {f"R{n:02}" for n in range(1, 62)}, "R5 index has R01-R61")
split = {track: (cells, int(count)) for track, cells, count in
         re.findall(r"^\| (Security|Performance) \| ([^|]+) \| (\d+) \|$", text, re.M)}
check(set(split) == {"Security", "Performance"}, "both R5 tracks listed")
security = re.findall(r"R\d{2}", split["Security"][0])
performance = re.findall(r"R\d{2}", split["Performance"][0])
check(len(security) == split["Security"][1] and len(performance) == split["Performance"][1],
      "R5 track counts")
check(len(security) == len(set(security)) and len(performance) == len(set(performance)),
      "no duplicate R5 rows within a track")
check(not set(security) & set(performance) and set(security) | set(performance) == set(r5),
      "R5 split covers R01-R61 exactly once")
detail = re.findall(r"^\| (R\d{2}) \| ([^|]+) \|", text, re.M)
check({case for case, _ in detail} == set(security), "every security row has a detail row")
for case, aliases in detail:
    check(set(re.findall(r"[AF]\d+", aliases)) ==
          {r5[case][key] for key in ("a_ref", "f_ref") if r5[case][key]}, f"{case} aliases")
print(f"PASS: 304 occurrences, 211 IDs, 12 threats, 28 non-boundary, "
      f"R5 split {len(security)}/{len(performance)}")
PY
```

[#215]: https://github.com/Adyen/agent-marketplace/issues/215
[#431]: https://github.com/Adyen/agent-marketplace/issues/431
[#432]: https://github.com/Adyen/agent-marketplace/issues/432
[#433]: https://github.com/Adyen/agent-marketplace/issues/433
[#434]: https://github.com/Adyen/agent-marketplace/issues/434
[R2 source index]: 2026-10-18-security-r2-source-index.csv
[R5 ledger]: 2026-10-16-security-r5-frozen-ledger.md
[R5 execution report]: 2026-10-17-security-r5-execution.md
[trust model]: ../../site/docs/explanation/marketplace-trust-model.md
[baseline]: https://github.com/Adyen/agent-marketplace/commit/f44101fa0203736fe4ff6202ad23215ab42b02a6
