# R5 R35: SDK Materialization Bookkeeping

## Scope

Start independently at main `de87921b` while R28 PR #743 is queued.
Frozen R35/A112/F79 concerns unchanged/skipped artifact bookkeeping in
SDK `Materialize`. Preserve the frozen ledger/source index and all D1-D6
obligations. No new ceiling, truncation or filesystem policy is introduced.

## Existing Contract

Planning admits paths, coalesces duplicate artifacts, computes marker
merges, captures opened-file identity and compares existing bytes. Reports
retain ordered skipped/written paths. Unchanged bytes never cause a write,
chmod or file replacement; this also applies when the requested mode differs.
The all-artifact path set protects skipped files during stale-file discovery.

The staging and publication loops currently rescan `report.Skipped` for
every artifact. A mostly unchanged generation repeats those searches.

## Options

- Retain the scans: minimal code but quadratic skipped-path bookkeeping.
- Add a skipped-path lookup map: expected linear membership, with another
  index and allocation.
- Compact the already-local artifact slice into the actual write list
  during planning, then stage/publish only that list. The independently
  populated all-path map remains unchanged for pruning.

Measure the third option. `MergeFragments` and merge preparation own the
local artifact slice, so compaction must not mutate the caller's `Result`.
Do not add an all-skipped early return: root admission/revalidation,
staging lifecycle, stale-file checks and partial-result behavior remain.

## Evidence

Before changing production, run public SDK preservation matrices and
structured cases with mixed unchanged/changed/missing files. Assert exact
report order, caller inputs, skipped bytes/modes/inodes/mtimes, actual
positive writes, stale-file retention/removal and absence of stage residue.
Keep marker-merge, dry-run, partial-result and refusal controls.

Benchmark the actual SDK with isolated temporary filesystems: repeated
all-skipped, mixed-write and all-written inputs. Include SDK disk I/O;
exclude fixture creation/reset, compilation and network. Retain identical
five-sample before/after data and benchstat rather than flaky time assertions.
Run focused package/SDK/catalog/materializer/CLI/race checks, strict specs
and combined E2E. Review every intended file via host OCR delegation before
push. Use a justified `Spec-Impact: none` trailer for unchanged behavior.
