# R5 Frozen Scope and Evidence Ledger

Parent: [#215]. Round: [#434]. Frozen: 2026-10-16.
Code baseline: [`7ec83fa38bdec8b4250524af6817c0d38982768e`][baseline].

## Scope Rule

This ledger freezes the report-derived **disposition queue**, not a list of
confirmed vulnerabilities or approved implementations. R01-R61 are its complete
case set. At freeze, 58 cases need current-code validation, R15 is partially
delivered, R45 has an existing-bound no-change disposition, and R54's reported
command is removed. The 59 non-final cases are not 59 confirmed defects.

- Keep case IDs and report aliases stable. Update evidence and dispositions in
  place; do not expand a row to unrelated consumers or invent extra obligations.
- A new suspicion outside these cases belongs in a separate issue linked from
  #215. An exclusion is not a claim that the excluded code is safe.
- Scanner severity, reachability assertions and triage state are not evidence of
  a current defect, an accepted contract, or a fix. Read both versions of a
  shared finding; a common identifier does not imply identical claims.
- First prove the actual supported consumer, lower-trust input and applicable
  contract. Fix only a reproduced boundary violation or measured repeated work.
  Close an unconfirmed case with evidence, not an unsuccessful probe alone.
- R2 owns filesystem admission and scope authority; R5 owns the applicable byte,
  response and work limits. Preserve R2's delivered descriptor/type/growth guards
  and R3/R4's accepted contracts. Do not reimplement merged work.
- No new threshold, finding truncation policy, graph semantics, authentication
  contract or operator policy is approved by freezing this queue.
- The sequencing in #434 remains unchanged. This document does not authorize
  skipping preceding milestones or declare the round complete.

This is a documentation-only reconciliation. Apart from the explicitly recorded
controls below, it is **not** a fresh runtime audit of all cases. The baseline
pins inspected code; future closure evidence must name the revision actually
tested. `openspec/specs/` remains the runtime contract authority.

## Source Provenance

The report labels follow #433: `A<n>` is the 2026-09-17 export, zero-based;
`F<n>` is the 2026-09-24 export, one-based. These dates identify the supplied
exports using the existing issue convention, not a scan timestamp or commit.
Both files declare SARIF 2.1.0 and `depthfirst-code-scanner` 1.0.0; neither
supplies `versionControlProvenance` or invocation metadata. Do not infer a
scanned revision from file timestamps, blame records or source line numbers.

| Export | Supplied Filename | Bytes | Results | SHA-256 |
|---|---|---:|---:|---|
| A | `beyildirim-agent-marketplace-vulnerabilities.sarif.json` | 6921063 | 131 | `67554afc2e08a359137bc66899a17547d812ca2d5022dfa08065753fa8d1690b` |
| F | `beyildirim-agent-marketplace-vulnerabilities.sarif (3).json` | 9141312 | 173 | `91906e9899dbc253d659ba8b87b7fdb03b837aa0d1f4a24a160d776594379e98` |

The [frozen source index](2026-10-16-security-r5-source-index.csv) maps every
occurrence to `properties.instance_id`: 304 occurrences, 211 distinct IDs,
93 shared, 38 A-only and 80 F-only. Each export has unique instance IDs.
Deduplication uses that ID, not a mutable title, severity or source location.
An absent alias means absent from that export, not fixed or withdrawn.

The 65 resource-processing complaints reconcile to 61 R5 cases and four explicit
boundary exclusions below. The other 146 IDs remain indexed as `outside-r5`.
The weakness label is an inventory cross-check, not the reason to confirm a
defect. The index retains references, not private report payloads or exploit
instructions; retain the original hashed files separately for full evidence.
Source-index SHA-256:
`e96e2bf20fe84944e8a7a5c4f0739c721fb11bb0463ea9512f27d6b86b430d3e`.

## R5.1: Existing Read and Response Ceilings

`Verify` means open: establish the consumer, existing limit and bounded valid
control before choosing fix/no-change/decision. `Verify + decision` additionally
flags that an aggregate or absolute limit may be new, not already authorized.
Rows can share a fix, but every listed claim still needs its own disposition.

| Case | Report Aliases | Consumer / Boundary to Verify | Freeze Disposition |
|---|---|---|---|
| R03 | A17 | Current v4 root/include/overlay loading, `internal/config/v4.go`; historical unbounded include read | Verify existing 4 MiB reader and refusal through current consumers; keep repeated include work in R51 |
| R04 | A22 / F17 | CLI collection discovery, `internal/collection/collection.go`; manifest allocation/parsing | Verify; source admission is delivered R2 work, not proof of an absolute ceiling |
| R05 | A24 / F20 | Exact source-revision support hashing, `internal/sourcerevision/builder.go` | Verify + decision; distinguish per-file/growth admission from aggregate support bytes |
| R09 | A39 / F28 | Native/owned aggregate reads, `internal/materialize/owned_json_reconcile.go` | Verify remaining size contract; boundary already delivered under #431, do not reopen static FIFO/symlink admission |
| R21 | A76 / F55 | API artifact/blob delivery, `pkg/marketplace/api/server.go` and blob stores | Verify applicable response/store limit before allocation and during growth |
| R29 | A90 / F66 | Collection manifest parser, `pkg/harness-compilation/artifact/collection.go` | Verify + decision; establish caller-supplied byte limits separately from a new parser ceiling |
| R31 | A94 | YAML frontmatter parsing, `pkg/harness-compilation/internal/yamlfm/yamlfm.go` | Verify caller byte bounds and parser work; do not infer a worker bypass from an unbounded generic reader alone |
| R32 | A106 / F72 | Markdown/frontmatter ingestion through the same parser and native ingesters | Verify + decision; retain this separate read-allocation claim despite the shared helper |
| R34 | A111 / F77 | SDK existing-output reads before merge/compare, `pkg/harness-compilation/harnesscompile.go` | Verify + decision; preserve R2 captured output admission and supported initial aliases |
| R37 | A120 / F85 | Public bundle ingestion and the shipped sync path, native ingesters | Verify + decision; standalone `convert` removal does not remove library/sync reachability |
| R39 | F102 | Autodetection marker reads, `pkg/harness-compilation/ingest/ingest.go` | Verify reader/caller limits and regular-file refusal before detection |
| R40 | F103 | Devin native JSON/Markdown ingestion, `pkg/harness-compilation/ingest/devin` | Verify caller bounds and parsing/allocation, not only source containment |
| R41 | F105 | Gemini settings/hook ingestion, `pkg/harness-compilation/ingest/gemini` | Verify caller bounds and nested native decoding |
| R45 | F110 | Admin preview descriptions via `artifact.ExtractDescription` and `store.BoundDescription` | No change for the reported oversized stored/served description; existing 1024-byte normalization/cap, controls E2 |
| R54 | F140 | Legacy `migrate --to v2` configuration reader | Removed consumer; shipped CLI has no migrator or legacy decoder/editor, controls E3; do not restore it for testing |
| R58 | F164 | Publication and blob-owner manifest indexing, `pkg/marketplace/store/blob_owners_sql.go` | Verify existing manifest ceiling and incomplete-coverage failure; test healthy unrelated digest reads, not only size rejection |
| R61 | F172 | Canonical tar decoding, `pkg/harness-compilation/artifact/canonical.go` | Verify member limit; total decoded bytes/member count need decision D1 |

## R5.2: Repeated Work and Graph Processing

These are measurement obligations, not confirmed complexity vulnerabilities.
Use bounded inert fixtures at several sizes, positive semantic controls and
allocation measurements. Do not add brittle timing thresholds to unit tests.

| Case | Report Aliases | Consumer / Cost to Verify | Freeze Disposition |
|---|---|---|---|
| R06 | A26 / F21 | Marketplace nested collection installation, `internal/marketplace/install.go` | Verify repeated manifest/network/cache work; preserve branch-local ancestry, decision D2 for a new budget |
| R11 | A52 / F171 | CLI resolver collection expansion, `internal/resolver/resolver.go` | Verify graph scaling and required/order/scope behavior; decision D2 |
| R15 | A68 / F47 | Ordered analyzer source locations and total finding work, `pkg/marketplace/analyze` | Partially delivered by #469, E1; remaining loops/result amplification and budget D3 stay open |
| R16 | A69 / F48 | Unterminated OSC suffix scanning, `pkg/marketplace/analyze/scanners.go` | Verify suffix rescanning separately; #469's position cursor is not an OSC algorithm fix |
| R17 | A70 / F50 | Registry collection graph validation, `pkg/marketplace/ingest/ingest.go` | Verify shared-DAG traversal and cancellation; preserve valid repeated branches, decision D2 |
| R18 | A71 / F52 | Reject findings retained after `MaxFindings`, `pkg/marketplace/analyze/analyzer.go` | Verify count/storage work; rejecting evidence cannot be silently dropped to meet a cap, decision D3 |
| R28 | A89 / F65 | Hook equality/deduplication, `pkg/harness-compilation/artifact/fragment.go` | Verify repeated serialization/comparisons and retain order/equality/native semantics |
| R30 | A91 / F67 | Library collection expansion, `pkg/harness-compilation/artifact/collection.go` | Verify public consumer graph cost; do not globally deduplicate branch semantics, decision D2 |
| R33 | A110 / F76 | Duplicate-key JSON validation, `pkg/harness-compilation/internal/jsonutil/jsonutil.go` | Verify growing diagnostic-path construction while retaining exact duplicate/alias diagnostics |
| R35 | A112 / F79 | SDK `Materialize` unchanged/skipped artifact bookkeeping | Verify repeat materialization cost with matching output; preserve no-op bytes/modes/mtimes |
| R36 | A119 / F84 | Nested JSONC diagnostic paths, `pkg/harness-compilation/jsonc/parse.go` | Verify path allocation at existing accepted depths; any new depth/node limit needs D1 |
| R38 | A128 / F91 | Raw preview highlighting, `web/app/lib/markdown.ts` | Verify repeated highlight/render work through the actual browser consumer |
| R42 | F106 | Gemini hook-name allocation, `pkg/harness-compilation/ingest/gemini` | Verify collision/name scanning while retaining generated names and ordering |
| R43 | F108 | JSONC aggregate merging, `pkg/harness-compilation/artifact/merge.go` | Verify repeated parse/patch work while preserving handwritten syntax and fragment semantics |
| R44 | F109 | Large MCP preview rendering/SSR, `web/app/lib/markdown.ts` | Verify server and browser consumers separately; existing input limits before any new preview budget |
| R47 | F116 | Duplicate Git requirements during lock reconciliation, `cmd/agent-marketplace/lock.go` | Verify repeated source builds; retain source/policy/scope identity and exact snapshots |
| R48 | F117 | `ProjectGitRoots` freshness matching, `internal/lockfile/roots.go` | Verify nested comparisons with equivalent lock freshness/error results |
| R49 | F123 | Marketplace collection preparation before consent, `internal/marketplace/install.go` | Verify effective caller limit, cancellation and work before consent; decision D2 |
| R51 | F134 | Recursive v4 includes, `internal/config/v4.go` | Verify repeated parse/merge with valid shared fragments; byte bounds in R03 do not bound total work, decision D2 |
| R52 | F135 | v4 Marketplace lock loading/adaptation, `internal/config/v4.go` | Verify repeated scans under the existing file limit without changing lock/source compatibility |
| R53 | F136 | v4 selection normalization/adaptation, `internal/config/v4.go` | Verify source/group/selection scaling with equivalent precedence and diagnostics |
| R57 | F162 | `toStoreFindings` / `lineColumnBytes`, `pkg/marketplace/ingest/ingest.go` | Open; baseline still converts each location from the beginning; consumer scaling not yet measured, #469 did not change it |
| R60 | F166 | Recursive metadata copies during canonicalization, `pkg/harness-compilation/artifact/canonical.go` | Verify reachable recursion after parsing/caller bounds; a new depth/node budget needs D1 |

## R5.3: Acquisition, Search and API Amplification

Trace actual caller limits, effective policy, cancellation and authority. A
trusted operator choosing a large deployment limit is not itself a defect.
Deployment rate limiting alone is not an app-level disposition either.

| Case | Report Aliases | Consumer / Boundary to Verify | Freeze Disposition |
|---|---|---|---|
| R01 | A5 / F8 | Federated search aggregation, `internal/marketplace/search.go` | Verify retention before global result limit and per-endpoint response bounds; aggregate budget D4 |
| R02 | A12 / F12 | Exact Git mirror acquisition, `internal/cache/exact_git.go` | Verify current context and object/byte contracts; new CLI repository/cache quotas need D5 |
| R07 | A27 / F22 | Discovery response cloning/marshal before cache admission, `internal/marketplace/discovery.go` | Verify existing HTTP/page/cache limits at allocation boundaries; aggregate budget D4 |
| R08 | A28 / F23 | Marketplace leaf acquisition during lock bootstrap, `internal/marketplacelock/lock.go` | Verify acquisition count/cancellation independently of encoded-lock size, decision D2/D5 |
| R10 | A47 / F31 | Update tag enumeration, `internal/pipeline/update.go` | Verify requested-ref semantics, buffering and cancellation; new tag limit needs D5 |
| R12 | A63 / F44 | Telemetry endpoint DNS validation, `internal/telemetry/config/config.go` | Verify supported startup caller and cancellation; preserve #432's trusted-user/environment exclusion |
| R13 | A64 / F45 | TUI local bundle enumeration, `internal/tui/bundle_tree.go` | Verify actual discovery workload/cancellation and any existing count limits, decision D4 |
| R14 | A66 / F46 | TUI name/layout processing, `internal/tui/list.go` | Verify upstream name bounds and rendering cost before proposing a new width/length policy |
| R19 | A73 / F51 | Worker effective acquisition limits, `pkg/marketplace/worker/worker.go` | Verify registry overrides versus operator defaults; intentional override is not a bypass |
| R20 | A75 / F54 | Revision history listing, `pkg/marketplace/api/server.go` and metadata stores | Verify query/response pagination and cancellation; new history budget needs D6 |
| R22 | A77 / F56 | API registry filter parsing/query expansion | Verify existing request/SQL limits and multiplicative work, decision D6 |
| R23 | A79 / F58 | Hosted dumb-HTTP Git acquisition, `pkg/marketplace/acquire/acquire.go` | Verify existing clone-duration/repository limits during transfer, not just after checkout |
| R24 | A80 / F57 | Hosted blob size before checkout in the same acquisition path | Verify configured file/repository ceilings before expensive materialization |
| R25 | A81 / F59 | Hosted Git object accounting in the same acquisition path | Verify what counts toward existing byte/file contracts; new object quotas need D5 |
| R26 | A83 / F61 | Manifest-owner metadata lookups, API/store | Verify actual request fan-out, existing owner limits and cancellation, decision D6 |
| R27 | A84 / F62 | SQLite search filtering/counting, `pkg/marketplace/store/sqlite/resources.go` | Verify query work before result limits, retaining search/ranking behavior |
| R46 | F115 | Git acquisition before lock-size rejection, `cmd/agent-marketplace/lock.go` | Verify supported v4 path and existing admission/encoding limits; new batch budget needs D5 |
| R50 | F128 | Ordinary CLI Git clone/fetch, `internal/cache/registry.go` | Verify propagated contexts and existing duration policy; new deadlines/quotas need D5 |
| R55 | F142 | Index-only revision cache writes, `internal/marketplace/status.go` | Verify existing per-response/cache/GC contracts; new aggregate cache budget needs D5 |
| R56 | F156 | TUI discovery goroutines, snapshots and sorting, `internal/marketplace/discovery.go` | Verify semaphore, queued work, endpoint policy and cumulative copy costs, decision D4 |
| R59 | F165 | Content-digest owner fan-out, `pkg/marketplace/store/blob_owners_sql.go` | Verify existing `BlobOwnerReadLimit` and incomplete-coverage behavior; distinguish bounded expensive work from absent limits |

## Recorded Evidence

### E1: Delivered Ordered Source-Position Work

[#469] merged on 2026-10-02 as `1a3490d6dcb0dbe66c1ba15b01e21043147a64ad`.
Its invocation-local cursor covers HTML comments, zero-width characters, bidi
characters and terminal-control **source positions**. It does not close OSC
suffix scanning, ingest byte-offset conversion, reject-finding policy, graph
expansion or total-work/finding budgets.

The PR records five two-second samples per size: dense-comment medians before
versus after were 150.26 vs 42.94 us (128), 1.962 ms vs 275.9 us (512), and
29.810 vs 1.297 ms (2048), with approximately unchanged allocations. This is
historical PR evidence, not a new benchmark run on the freeze baseline.

Retained controls: `pkg/marketplace/analyze/range_boundary_test.go`
(`TestDenseCommentFindingsRetainOrderedRuneLocations`,
`TestOrderedCharacterScannersRetainRuneLocations`,
`TestStandaloneSourceRangePreservesOffsetBoundaries`) and
`range_scaling_bench_test.go` (`BenchmarkDenseCommentSourceRanges`).
The archived delta is `reuse-finding-range-scan-state`; the normative capability
is `marketplace-analysis`. R15 remains partial and R16/R57 remain open.

### E2: Already Bounded Admin Preview Descriptions

`artifact.ExtractDescription` calls `store.BoundDescription`; store writes and
API egress also apply it. `store.MaxDescriptionBytes` is 1024 bytes, including
the ellipsis. The report itself describes this control rather than a remaining
oversized-description defect. This closes only R45's stored/served value claim,
not aggregate preview size, pre-normalization CPU or all browser rendering.

Controls: `TestExtractDescriptionBoundsUntrustedContent`,
`TestPutResourceRevisionBoundsDescription` (SQLite), and
`TestSearchBoundsHostileDescriptionOnEgress`. Their inputs include oversized
descriptions; sibling description tests retain valid-value controls.

### E3: Removed Legacy Migrator

The baseline has neither the shipped `migrate` command nor
`internal/config/migrate.go`. `consumer-config-format` explicitly requires
absence of the command, target flags, startup conversion and migration modal.
R54's legacy `--to v2` path is therefore not a current supported consumer.
This does not close R03/R51's supported v4 root/include obligations or imply
that an older released migrator is repaired.
`TestConsumerMigrationSurfaceIsAbsent` exercises unknown-command refusal,
help/completion absence and byte-preservation of an unsupported root.

## Exclusions and Ownership

| Source Index Scope | Cases | Destination / Reason |
|---|---|---|
| `parent-image` | A129 / F92 | Image/raster handling remains with #215's explicitly retained hosted-service follow-up; not silently absorbed into R5 |
| `operator-proxy` | A130 / F93, F111 | Operator exposure/rate/concurrency choices; retained under #215 for deployment-policy disposition, not admitted as an application defect without a trust-boundary witness |
| `parent-oidc` | F159 | Authentication callback replay belongs to a hosted-auth disposition under #215; no new OIDC one-time-state/rate policy is authorized here |
| `outside-r5` | 146 other distinct IDs | Selection, filesystem/ownership, credential/terminal, conversion-authority and other hosted-service claims; #215 and #431/#432/#433 retain their existing dispositions |

For overlap, keep the R5 read/work portion only. In particular R09's FIFO and
unsafe-alias handling already has R2 delivery evidence in #431; R5 must not
count it as an unfixed filesystem case. A72/F49's lease-race claim is retained
outside R5 with #215, not smuggled in as a new processing budget.

These are scope-routing decisions for this ledger, not new maintainer risk
acceptances or proof of final resolution in the destination. Publishing the
ledger must preserve these links and #215's unfinished follow-ups.

## R5.4: Decision Gates

All new policies below are **unapproved**. First map existing contracts and
measure reachable cost. A semantics-preserving optimization or enforcement of
an existing ceiling does not require inventing a budget.

| Gate | Compatibility Surface | Decision Required Before Implementation |
|---|---|---|
| D1 | Parser/canonical archive depth, nodes, members and aggregate decoded bytes | Limit units/defaults/overrides; failure code and stage; no unsafe partial output; accepted native payloads |
| D2 | Includes/collection DAGs, preparation and leaf acquisitions | Unique nodes vs edges vs expanded paths; ordering/multiplicity/branch-local cycle semantics; cancellation and failure atomicity |
| D3 | Total/reject findings and persisted assessments | Preserve reject outcome and valid evidence; define truncation/summary identity, deterministic ordering and stored compatibility |
| D4 | Search/discovery/TUI buffering, endpoint count and previews | Per-endpoint vs global budget, incomplete/error presentation, streaming/ordering and offline cache compatibility |
| D5 | CLI/hosted Git, tags, batch acquisition and persistent cache | Trusted operator override vs lower-trust input; deadlines/quotas, reuse/GC, failure and lock publication semantics |
| D6 | Revision/filter/search/owner API query work | Existing pagination/owner ceilings, new query caps if needed, response/error behavior and healthy unrelated reads |

An in-scope unresolved decision blocks closure unless a maintainer explicitly
approves a recorded exclusion/follow-up with an owner and destination. Moving
an item to "needs decision" is not a completed no-change disposition.

## Delivery and Closure

- [x] Freeze both source hashes, full occurrence mapping, case IDs and exclusions.
- [x] Record #469's delivered slice without closing R15/R16/R57 as a family.
- [ ] Attach fresh evidence to every remaining row: revision, actual consumer,
  input authority, existing contract, valid control, result and final disposition.
- [ ] For a confirmed defect, add a bounded consumer regression that fails before
  the minimal shared-boundary fix and passes after; include preservation on
  refusal and no staging residue where publication is involved.
- [ ] For repeated work, record multiple-size before/after time and allocation
  samples using equivalent results/diagnostics; retain a runnable benchmark.
- [ ] Obtain explicit decisions for new policies, or approved named exclusions.
- [ ] Review/merge accepted fixes, resolve actionable review comments, and
  reconcile integrated-main evidence before closing #434 and updating #215.

Each implementation PR carries an archived OpenSpec delta for behavior changes
and `// spec:` witnesses, or an appropriate no-impact explanation. Run affected
package tests first, `make validate`, and the touched spec/service/E2E/TUI/CI
gates on the delivered head. Perform `open-code-review-delegate` adversarial
review before every push. A passing suite is not proof of defect absence.

No fixture may execute a resource hook/command/MCP server, use live credentials,
contact a real credential-bearing service or consume unbounded resources.

## Freeze Verification

Fresh local controls passed on the recorded baseline on 2026-10-16:

- The reconciliation check below passed against both exact exports. Negative
  controls reject swapped exports, an altered index alias and a missing case row.
- `make validate` passed with explicit test packages `pkg/marketplace/{analyze,artifact,store,store/sqlite,api}`
  and `cmd/agent-marketplace`, selecting the three E1 tests, description tests
  and `TestConsumerMigrationSurfaceIsAbsent`, with `-count=1`. This includes
  CI lint, vet and the CLI build; it is not a full-suite test result.
- `TestConsumerMigrationSurfaceIsAbsent` also passed alone with verbose child
  output, proving the E3 witness executed rather than merely matched no tests.
- Host-driven `open-code-review-delegate` review covers both added artifacts:
  2 total, 2 reviewed, 0 skipped, 100% coverage. OCR excludes Markdown/CSV by
  default; both exclusions were explicitly reviewed with its resolved rules.
  Alias/provenance, evidence overclaims, scope ownership, decision gates,
  refusal checks and unsafe publication claims were checked; no actionable
  finding remains in this documentation diff.

No new scaling measurement, full E2E, native probe or browser/TUI gate is claimed
for this documentation-only freeze. The remaining rows retain their evidence
obligations. Publication and issue updates are separate from local verification.

## Reproduce the Source Reconciliation

From the repository root, set `REPORT_A` and `REPORT_F` to the original files,
then run this read-only check. It verifies provenance, complete alias coverage,
case membership and table/index agreement, not vulnerability truth or runtime
safety. It needs only Python's standard library and never opens a network
connection or interprets report payloads as commands.

```bash
python3 - "$REPORT_A" "$REPORT_F" <<'PY'
import csv
import hashlib
import json
import re
import sys
from collections import Counter
from pathlib import Path

base = Path("docs/issues/2026-10-16-security-r5")
index = Path(str(base) + "-source-index.csv")
ledger = Path(str(base) + "-frozen-ledger.md")
assert hashlib.sha256(index.read_bytes()).hexdigest() == (
    "e96e2bf20fe84944e8a7a5c4f0739c721fb11bb0463ea9512f27d6b86b430d3e")
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
assert sum(bool(row["a_ref"] and row["f_ref"]) for row in rows) == 93
cases = {f"R{number:02}" for number in range(1, 62)}
counts = Counter(row["scope"] for row in rows)
assert counts == Counter({**dict.fromkeys(cases, 1), "outside-r5": 146,
                          "parent-image": 1, "operator-proxy": 2, "parent-oidc": 1})
table = re.findall(r"^\| (R\d{2}) \| ([^|]+) \|", ledger.read_text(), re.M)
assert len(table) == 61 and {case for case, _ in table} == cases
for case, aliases in table:
    row = next(row for row in rows if row["scope"] == case)
    assert set(re.findall(r"[AF]\d+", aliases)) == {
        row[key] for key in ("a_ref", "f_ref") if row[key]}
print("PASS: 304 occurrences, 211 IDs, 61 R5 cases, 150 explicit exclusions")
PY
```

[#215]: https://github.com/Adyen/agent-marketplace/issues/215
[#431]: https://github.com/Adyen/agent-marketplace/issues/431
[#432]: https://github.com/Adyen/agent-marketplace/issues/432
[#433]: https://github.com/Adyen/agent-marketplace/issues/433
[#434]: https://github.com/Adyen/agent-marketplace/issues/434
[#469]: https://github.com/Adyen/agent-marketplace/pull/469
[baseline]: https://github.com/Adyen/agent-marketplace/commit/7ec83fa38bdec8b4250524af6817c0d38982768e
