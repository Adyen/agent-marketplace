## Context

See `proposal.md` for motivation. The current update path combines preparation
and effects: CLI update recovery and missing-lock bootstrap run before
`pipeline.Update`, and the dependency reconciler both prepares exact candidates
and publishes configuration/acquisition locks before `silentSyncCollect`
materializes resources. Skipping the final sync is not a safe preview.

`guardNewlyMissingSelections` already resolves prior and candidate state with
config/lock overlays and `SkipInventory`. It provides the comparison boundary
to reuse, not a second selection algorithm.

Marketplace resource artifacts contain canonical definitions and supporting
files, not necessarily the publisher's original source files. Existing
inspection projections already use stable readable primary paths such as
`SKILL.md`, `COMMAND.md`, and `AGENT.md`. They are useful rendering precedents,
but a review must not omit semantic metadata merely because an existing
inspection body is narrower.

The artifact/source-revision formats and historical verification rules are
compatibility identities. This feature must not change their bytes, digests,
generators, source admission, or Marketplace protocol.

## Goals / Non-Goals

**Goals**

- Expose one prepared candidate state to summary, diff, and apply consumers.
- Preserve the current update selection and publication authority, including
  existing applicability of Git-specific targeting flags.
- Keep a no-diff update free of diff-generation work and extra historical-body
  acquisition solely for presentation.
- Keep pure comparison/rendering separate from acquisition and publication.

**Non-goals**

- A second resolver, whole-installation transaction, materialization preflight,
  new Marketplace API, or new persistent review-plan format.
- A byte-exact source-layout round trip, original-file patch, generic secret
  detector, or expansion of local secret references.
- Changing current source admission to support symlinks or special files, or
  changing Marketplace consent/revocation policy.

## Decisions

### 1. Prepare candidates separately from publishing them

Extract an update-specific prepared result from existing dependency preparation.
It retains original input generations, proposed config/lock overlays, exact
Git and Marketplace evidence, warnings/refusals, and the prior/candidate
effective selected-resource views. Reuse the existing stable-read and exact
snapshot leases rather than reacquiring "latest" while applying.

The preview entry point checks pending transactions without replay. It prepares
required initial acquisition evidence in memory/private temporary state, not by
calling the mutating missing-lock bootstrap. Consumer-format and policy checks
still precede new acquisition. Applying update retains established safe recovery
before planning new work.

The applying path consumes the prepared result, revalidates input generations,
publishes through the existing dependency transaction, and materializes from the
same exact candidates. Hold the necessary cache leases through preparation and
consumption; close them on every outcome.

**Rejected:** treating `lockMode.check` as preview. Current check mode validates
existing locks and intentionally avoids acquisition; it does not plan an online
upgrade. Also rejected: running update then restoring files, which cannot safely
undo publication, recovery, backups, or changed symlink targets.

### 2. Compare resolved resources, not registry trees

Use the prior/candidate resolver views and selected collection roots to form
resource review records. Keep scope and complete source identities; do not join
by installed name alone. Preserve complementary scope selections and additive
hooks, show changed collision provenance, and account for multiple installation
selectors when a collection removes a member.

Use lock/source manifests for summaries where sufficient. Only `--include-diff`
adds the requirement to acquire historical bodies for a complete comparison.
Missing latest Marketplace resources retain their old selection and existing
partial-update treatment; no nonexistent candidate diff is required for them.
Selections refused by the existing guard remain refused.

An absent writable acquisition lock means no locked historical baseline, not
that installed files are empty. Initial acquisition may expose full candidate
content with an explicit label. Local directories and intentionally unlocked
organization sources have no versioned comparison and must not look unchanged.

### 3. Render a complete normalized definition plus exact supporting files

Derive review content from verified resource data for both acquisition paths.
Reuse canonical resource conversion and existing readable inspection rendering
where they are complete. Add a deterministic semantic-metadata view when needed
to cover all preserved fields, extension data, and source-layout-specific
semantics. Do not change an existing inspection projection or its digest as a
side effect.

Instruction bodies remain multiline Markdown. Structured definitions and
metadata use deterministic readable ordering. Original comments, source
locations, and formatting do not define this view. Shared MCP/hook source files
contribute only the selected resource's data.

Supporting files retain their exact admitted bytes and executable modes.
Diff text by resource-relative review path; identify non-renderable bytes with
size/digest/mode metadata. Retain existing read bounds, opened-file authority,
and containment checks rather than introducing an unconstrained filesystem walk.
Do not infer rename similarity.

**Rejected:** original Git source files plus normalized Marketplace definitions.
That makes the same resource review differently by acquisition path and exposes
unrelated entries in shared source files. Generated harness files also conflate
upstream review with compilation and installed-file drift.

### 4. Complete requested comparisons before new publication

Prepare all required diff records before publishing new updates. A missing
recorded baseline, failed verification, or comparison failure is an error, not
an empty baseline or a silently omitted hunk. Do not bypass effective revocation
to recover historical content.

The prepared representation need not be a large in-memory rendered string.
If private temporary storage is needed, use owner-only directories/files,
retain authority to their original objects, and clean only attempt-owned
objects on success, failure, and cancellation. Acquisition-cache reuse remains
permitted and distinct from temporary preparation residue.

This is not a guarantee that output delivery or later materialization succeeds.
An applying result must preserve phase information even when synchronization
returns the existing dependency-committed failure. The CLI can then distinguish
published dependencies from complete harness application and from output failure.

### 5. Content output is an explicit disclosure surface

`--include-diff` intentionally shows authored content, like a source diff.
Do not substitute environment values, read credentials for resource expansion,
or insert acquisition authentication. Continue locator/error redaction and
terminal sanitization. Literal sensitive data authored into a resource can
appear in stdout; document this rather than promising a secret scanner.

Do not reuse ordinary diagnostic or logging paths for diff payloads. Log
operation boundaries, counts, durations, and safe failure identities, not
definitions, commands, values, hunks, or file bodies.

### 6. Preserve the small command contract

Plain update retains its current output and update behavior. `--dry-run`
requests a successful/nonzero-error preview, not a check-for-drift exit code.
`--include-diff` requests content in the report, not confirmation. No saved
state binds a future invocation.

The summary states that it covers versioned resource changes and not every
subsequent materialization effect. In particular, applying update may also
reconcile live directory content or encounter handwritten aggregate collisions
that were not checked by this preview.

## Risks / Trade-offs

- **Review hides semantic data** -> Exercise independently authored metadata
  changes for every resource kind, including preserved extension fields and
  native semantic provenance; do not accept a body-only rendering as complete.
- **Dry run writes through a reused entry point** -> Test the actual command with
  acquisition counters, missing-root fixtures, both materialization modes,
  pending journals, and byte/mode/mtime snapshots of protected state.
- **Historical content is unavailable or revoked** -> Fail the explicitly
  requested comparison before new publication. Summary-only behavior retains
  existing validation requirements without adding a diff-only dependency.
- **Large or adversarial content consumes resources** -> Retain acquisition/read
  bounds, use an appropriately bounded diff algorithm, and benchmark realistic
  many-file and long-line inputs before choosing the implementation. Do not add
  silent truncation or arbitrary new acceptance limits.
- **Preview appears to approve a later update** -> Use proposed/applied labels
  and document independent invocations and the absence of an approval prompt.
- **Source diff is mistaken for a security assessment** -> Document intentional
  content disclosure and retain independent security-findings/revocation policy.
- **Partial publication is mislabeled** -> Test real dependency publication
  followed by harness/output failure; preserve the phase boundary in results.

## Migration Plan

No persisted-format migration is required. Add opt-in flags and documentation;
keep default command behavior and historical identities stable. Removing the
new flags would remove the feature without requiring on-disk conversion.

## Implementation evidence

The initial implementation on `spec/update-preview-diffs` was based on
`93e7778f`. No persisted formats or historical content identities change.
Command tests and v4 E2E witnesses cover the new flags. Additional command
regressions exercise cold/warm organization Marketplace collections and a real
upstream push between comparison and dependency publication. The latter checks
the reported diff, published lock, installed content, and a subsequent independent
update. Removing the organization exclusion makes its regression fail with the
former unresolved-collection error; restoring it passes.

Adversarial review found and fixed the missing previous-source label for collision
winners, a weakened missing-selection check for never-locked sources, and
organization selections entering update comparisons. Each has a command-level
regression. Before integration, `make validate`, `make spec-check`, and
`make validate-e2e` passed after the production fixes. The combined E2E gate covered
115/115 command requirements with zero listed gaps and all three required witnesses
passing.
TUI behavior is unchanged, so no TUI journey changes were needed.

### Performance evidence

The existing narrow organization-pin benchmarks were compared against a temporary
sibling worktree at `93e7778f`; that task-created worktree was removed afterward.
They do not measure end-to-end acquisition or materialization.

| Benchmark | Base ns/op | Implemented ns/op | B/op delta | Allocs/op delta |
|---|---:|---:|---:|---:|
| `WriteUpdateOutputOrganizationPins` | 260.4 | 266.6 | 0 | 0 |
| `MarketplaceUpdatePlanningSkippedPins` | 13,792 | 14,065 | 0 | 0 |

The observed runtime differences are approximately +2.4% and +2.0%. The first
benchmark's runs overlap; these measurements do not establish a meaningful
performance change.

New comparison benchmarks take already-read bytes and exclude disk/network I/O:

| Benchmark | Time/op | B/op | Allocs/op |
|---|---:|---:|---:|
| `UnifiedDiffLongLines` (200 changed long lines) | 494.8 µs | 1,855,880 | 461 |
| `CompareManySupportFiles` (64 changed support files) | 14.07 µs | 23,268 | 190 |
| `DefinitionRenderSkill` (50 body lines plus metadata) | 2.28 µs | 2,291 | 35 |

The definition benchmark was renamed from `DefinitionRenderAllKinds` to describe
its actual skill workload. Reproduce the comparison workloads with
`CGO_ENABLED=0 go test ./internal/resourcediff -run '^$' -bench . -benchmem`.

The initial implementation and archive were kept local for review. The subsequent
publication request authorizes fixing verified findings, integrating current
`main`, validating the result, and opening a pull request linked to issue #932.

Follow-up adversarial regressions reproduced invalid leading hunk coordinates,
missing final-newline markers, ref bumps mislabeled as initial acquisition, and
organization pin rows appearing without `--verbose`. They now pass with the fixes.
Previous-source labels include Git ref/path and Marketplace instance/coordinate.
Preservation tests now inspect the real cache and hash protected file contents;
successful previews may still add disposable acquisition entries.

Integration with `main` at `867731e5` retained completed-hook reviews and
materialization warnings. An existing regression caught the new partial-report
return path bypassing the post-publication failure message; the command now
reports that phase boundary whether synchronization returns a partial report or
none. Focused tests also verify that preview summaries disclose no hook payloads
and neither preview mode reports completed hook mutations.
