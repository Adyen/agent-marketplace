## Context

See `proposal.md` and
[ADR 0011](../../../docs/adr/0011-cooldown-clocks-and-history-rewrite-refusal.md).
Verified current behavior that this design builds on:

- `update` is `lockfile.OperationUpgrade` with an all-selected
  `UpgradeSelection`. `reconcileLayerDocument` (`cmd/agent-marketplace/lock.go`)
  re-acquires every selected `url+ref` group through `acquireLockedRegistry`,
  which calls `cache.ResolveExactGitRefWithContext` and **always takes the ref
  tip** (`fetch --force --no-tags <url> <ref>:refs/agent-marketplace/resolved`,
  then `rev-parse --verify …^{commit}`). Nothing compares the new commit with
  the locked one.
- The exact object store is a full `clone --mirror --no-tags`
  (`cloneExactGitObjectStoreWithContext`), so `rev-list --first-parent`,
  `merge-base --is-ancestor`, and committer dates are local reads. The locked
  commit is present whenever it is still reachable from any branch of the
  mirror.
- `AcquireExactGitWithContext(oid)` accepts any OID, so pinning a non-tip
  candidate needs no new acquisition path and the lock format needs no new
  field.
- Initial acquisition (`sync` with a lock missing the group) shares
  `acquireLockedRegistry`.
- `update --bump` is separate: `discoverTagBump` runs `git tag -l` in the
  **mutable ref cache** (`CloneOrPull`, a full clone with tags) and picks the
  highest numeric suffix regardless of age or ancestry.
- `update --dry-run` must use the same candidate selection as the applying
  invocation (`update-preview`).

## Goals / Non-Goals

Goals:

- Advance Git locks only to commits that have been in the ref's first-parent
  history for the effective period, using a clock that a push cannot backdate
  past the locked anchor.
- Make a rewritten branch history an explicit, evidenced decision.
- Keep lock bytes, `sync --locked`, and `sync --check` unchanged.
- Ship the consumer `cooldown` schema and layering once, for both client
  changes.

Non-Goals:

- Marketplace-source behavior (`marketplace-cooldown-client`).
- Protecting initial acquisition against a repository whose entire history
  was fabricated; there is no anchor, and the ADR records that trust decision
  as the `git:` declaration itself.
- Recording acceptance anywhere other than the resulting lock.
- A runtime flag that bypasses a hold.

## Decisions

### D1: Consumer schema and validation

```yaml
apiVersion: agent-marketplace/v4
cooldown:
  period: 24h                     # Go duration; default 0; negative invalid
  initial_acquisition: acknowledge # acknowledge (default) | strict
  missing_evidence: warn           # warn (default) | refuse; Marketplace only
sources:
  team-tools:
    git: https://github.com/example/team-tools.git
    ref: main
    cooldown:
      period: 72h                 # per-source: period only
```

- Allowed in organization, user, repository, and personal-local roots and in
  their included fragments; the mapping merges field by field like `backup`.
- Per-source `cooldown` accepts only `period`; it is valid on `git` sources,
  rejected on `directory` sources with `v4.source-field`, and (for
  `marketplace-cooldown-client`) valid on `marketplace` sources.
- Unknown keys fail with `v4.unknown-field`; a non-duration, negative, or
  non-string `period`, or an unknown mode value, fails with `v4.cooldown`
  naming the file and field.
- `missing_evidence` is validated here and has no effect on Git sources
  (commit dates always exist); `marketplace-cooldown-client` gives it
  behavior. Shipping it in the schema now avoids a second schema change.

### D2: Effective floor

For each source the effective period is
`max(contribution(layer) for layer in declaring layers)`, where a layer's
contribution is its per-source `period` if declared, else its global `period`
if declared, else nothing. Layers are evaluated organization → user →
repository → personal-local. A declared value lower than the floor
accumulated from higher layers is clamped and reported once per invocation as
the warning `cooldown.relaxation-ignored` with the file, field, declared and
effective values. `initial_acquisition` resolves to `strict` if any declaring
layer says `strict`. The built-in default `0` is a default, not a floor: it
applies only when no layer declares a `period`, so a repository can set `0`
when the organization is silent.

The computation lives next to the allowlist resolution in
`internal/config` and is exposed on the effective configuration so
`acquireLockedRegistry`, the `--bump` planner, previews, and the TUI read one
value.

### D3: Effective commit date and candidate selection

Given a group `(url, ref)` with effective period `P > 0`, cutoff
`C = now − P` (one `now` per invocation), and the exact object store after the
existing fetch of `ref` into `refs/agent-marketplace/resolved`:

1. Read the first-parent chain from the tip with one
   `git rev-list --first-parent --format=%H%x00%ct%x00%P <tip>`; a full
   first-parent walk is a local read of the mirror and needs no bound.
2. **Anchored** (a lock entry exists for the identity, commit `L`): the chain
   must contain `L`. Truncate the chain at `L`; compute effective dates from
   `L` upward as a running maximum of committer dates. `L`'s own effective
   date seeds the maximum, so nothing above it can claim to predate it.
3. **Unanchored** (initial acquisition, or an accepted rewrite): compute the
   running maximum from the root.
4. The candidate is the newest commit strictly above `L` (anchored) or in the
   whole chain (unanchored) whose effective date `≤ C`.
5. Anchored and no candidate → **hold**: keep `L`, report the tip's effective
   date and `tip_effective + P` as the eligible time.
   Unanchored and no candidate → `initial_acquisition` decides (D5).
6. `P == 0` keeps today's behavior exactly: the tip.

A `ref` that is a full commit ID never enters this path (it is already
excluded from `--bump` and resolves to itself).

### D4: History rewrite

In the anchored case, if `L` is not in the tip's first-parent chain, run
`git merge-base --is-ancestor L tip` to distinguish "reached through a
non-first parent" from a true rewrite (not an ancestor, or object missing).
The former happens when the consumed branch was fast-forwarded to a side
branch that had merged it; it is ordinary history. Then the anchor becomes the
lowest commit on the first-parent chain that descends from `L`, candidates are
restricted to that commit and above, and the running maximum is seeded with
`L`'s committer date so nothing above the anchor can claim to predate the
lock. A true rewrite is refused with `lock.history-rewritten` before any lock,
config, or harness write, and the run's other groups continue to be planned so
the report lists every refusal at once.

Evidence per refused source (all computed locally):

| Field | Source |
|---|---|
| `url@ref` | the group |
| locked commit, tip | `L`, resolved tip |
| locked commit still reachable | `git for-each-ref --contains L` on the mirror |
| commits between merge-base and tip | `rev-list --first-parent --count <merge-base>..tip`, or the full count when there is no merge-base |
| tip effective date | D3 step 3 |

Followed by two remediation commands: `agent-marketplace update --dry-run
--include-diff` and `agent-marketplace update --accept-history-rewrite`, plus
the targeted form `--registry <url> --ref <ref> --accept-history-rewrite`, and
the sentence: "After a rewrite the cooldown cannot protect you from a
backdated history; review the diff before accepting."

`--accept-history-rewrite` is accepted by `update` and `lock --upgrade`. With
no targeting it accepts every refusal in that run; with `--registry`/`--ref`
it accepts only matching groups and the others still refuse. Acceptance is not
persisted: once the new commit is locked, later runs are anchored on it.
After acceptance the candidate is selected per D3 step 3 (unanchored), so the
cooldown still applies to the measured dates.

A stale-lock `sync` (configuration changed for the group) is an initial
acquisition for the new identity and does not meet this check; only a
re-resolution of an unchanged identity can observe a rewrite.

### D5: Initial acquisition

Unanchored with no eligible commit:

- `initial_acquisition: acknowledge` → take the tip, lock it, and warn with
  `cooldown.initial-acquisition` naming the source, the tip's effective date,
  and the eligible time. The acknowledgement is the `git:` declaration; no
  flag is required for Git sources (ADR 0011).
- `initial_acquisition: strict` → refuse with the same code and the
  remediation "wait until <time> or pin the exact commit in `ref`".

Unanchored with an eligible commit → that commit, silently. This is what a new
consumer of an established repository gets when the tip is fresh.

### D6: `--bump`

`discoverTagBump` gains an eligibility filter. For each matching tag in the
mutable ref cache: resolve it to its commit; take the tagger date for an
annotated tag (`for-each-ref --format=%(taggerdate:unix)`); compute the
commit's effective date along its first-parent chain down to the merge-base
with the locked commit (or the root when there is none); the tag's effective
date is the later of the two. Only tags with effective date `≤ C` compete; the
highest eligible numeric suffix wins as today. Ineligible higher tags are
reported as held candidates. No ancestry between old and new tag is required.
The mutable cache is a full clone, so this is also local.

### D7: Output

- `update` and `lock --upgrade` print, per held source:
  `held <url>@<ref>: tip <short> effective <date>, eligible <date> (period <P>)`
  in the existing update report, before the dependency/materialization
  sections. Exit 0 when holds are the only outcome.
- `update --dry-run` prints the same lines and the rewrite refusal, with the
  statement that nothing was applied; `--include-diff` diffs only selected
  candidates.
- `sync` performing an initial acquisition prints the `cooldown.initial-acquisition`
  warning through the existing warning sink.
- `--verbose` additionally prints the selected candidate when it is not the
  tip, so a user can see why the lock stopped short.

### D8: TUI

- The update result shows holds as update-category info diagnostics with the
  same text as D7; they do not fail the operation.
- A history rewrite arrives as an update error diagnostic with a recovery
  action "Accept rewritten history…". The action opens a confirm modal built
  on the existing modal/forms toolkit, listing the D4 evidence rows, with
  "Accept all" and per-row toggles, then re-runs the update with the accepted
  identities. Cancel leaves everything unchanged.
- Journeys: `tools/tui-qa/tests/update-cooldown-hold.spec.ts` (fixture
  repository with a fresh tip; press `u`; assert the hold text and unchanged
  lock) and `tools/tui-qa/tests/update-history-rewrite-accept.spec.ts`
  (fixture branch rewritten; press `u`; assert refusal; open recovery, accept;
  assert the lock advanced). Both must fail without the change.

### D9: Codes

| Code | Kind | Where |
|---|---|---|
| `v4.cooldown` | error | configuration validation |
| `cooldown.relaxation-ignored` | warning | effective configuration |
| `cooldown.held` | info | update/lock/preview reports, TUI |
| `cooldown.initial-acquisition` | warning (acknowledge) / error (strict) | sync, update, lock, TUI |
| `lock.history-rewritten` | error | update, lock --upgrade, TUI |

### D10: Time and determinism

`now` is captured once per invocation and passed to every selection so a
long-running update cannot straddle an eligibility boundary. The client
clock is the consumer's own machine; skew only affects the consumer. Given the
same history and the same `now`, selection is a pure function, which is what
lets previews and applying runs agree.

## Spec delta outline

`consumer-config-format`

- Requirement: Cooldown mapping
  - Scenario: Valid cooldown mapping loads in every layer
  - Scenario: Invalid period fails with v4.cooldown
  - Scenario: Unknown mode value fails with v4.cooldown
  - Scenario: Directory source rejects cooldown
  - Scenario: Per-source cooldown accepts only period

`config-layering`

- Requirement: Cooldown is a floor that layers may only raise
  - Scenario: Repository raises the organization period
  - Scenario: Repository value below the organization floor is clamped with a warning
  - Scenario: Per-source value overrides the layer global within one layer
  - Scenario: Strict initial acquisition is never relaxed by a lower layer
  - Scenario: Built-in default applies only when no layer declares a period

`deployed-acquisition-locks`

- Requirement: Git candidates respect the effective cooldown
  - Scenario: Fresh tip is held and the lock is retained
  - Scenario: Older eligible descendant is selected instead of the tip
  - Scenario: Backdated tip is measured by its parent and held
  - Scenario: Full commit ref is exempt
  - Scenario: Zero period resolves the tip
- Requirement: Initial acquisition selects the newest eligible commit
  - Scenario: Established repository with a fresh tip locks the newest eligible commit
  - Scenario: Entirely young history takes the tip with a warning
  - Scenario: Strict initial acquisition refuses an entirely young history
- Requirement: History rewrites are refused until accepted
  - Scenario: Rewritten branch refuses with evidence and leaves every lock unchanged
  - Scenario: Side-branch merge is not a rewrite
  - Scenario: Blanket acceptance advances every refused source
  - Scenario: Targeted acceptance advances only the named source
  - Scenario: Accepted rewrite still applies the cooldown
- Requirement: Tag bumps respect the effective cooldown
  - Scenario: Fresh higher tag is held and the next eligible tag is selected
  - Scenario: Annotated tag uses the later of tagger and commit dates

`update-output`

- Requirement: Holds and rewrites are reported
  - Scenario: Hold line names the tip, dates, and period and exits 0
  - Scenario: Rewrite refusal lists evidence and both remediation commands
  - Scenario: Held candidates are excluded from content diffs

`update-preview`

- Requirement: Preview reports holds and rewrites without writing
  - Scenario: Dry run reports the same hold as the applying run
  - Scenario: Dry run reports a rewrite and writes nothing

`tui-error-recovery`

- Requirement: History rewrite is recoverable from the TUI
  - Scenario: Recovery action shows evidence and accepting advances the lock
  - Scenario: Cancelling the acceptance modal changes nothing

`tui-feedback`

- Requirement: Update holds are visible
  - Scenario: Held source appears in the update result with its eligible date

## Risks / Trade-offs

- **Non-monotonic legitimate histories** (local commit pushed days later onto
  a newer tip) are measured by the parent's date, never flagged; the only
  cost is that such a commit is considered as new as its parent. Accepted.
- **Repositories with huge first-parent histories**: one `rev-list` per
  group per run. Anchored walks stop at `L`; unanchored walks are rare
  (initial acquisition, accepted rewrite). Benchmarks in `internal/cache`
  bound it.
- **Legitimate history rewrites of consumed branches** now need an explicit flag.
  This is the intended behavior change; the evidence and remediation text are
  what make it tolerable for non-experts.
- **Adding a resource to a locked source is held** like any update. Accepted
  in the design session; the per-source and organization period are the
  levers.

## Migration Plan

1. Schema, validation, layering, and diagnostics (`internal/config`) with
   tests.
2. History reads and effective-date computation in `internal/cache` with
   fixture repositories covering fresh tips, backdated commits, side-branch
   merges, rewrites, and annotated tags.
3. Candidate selection, hold reporting, rewrite refusal and acceptance in
   `cmd/agent-marketplace/lock.go` and `internal/pipeline/update.go`,
   including `--dry-run` parity and `--bump`.
4. TUI diagnostics, recovery action, modal, and the two journeys.
5. E2E scenarios in `make validate-e2e`; released-client gate run to confirm
   lock bytes stay identical.
6. Documentation and archive in the same pull request.

Rollback is `cooldown: {period: 0}`; the rewrite refusal has no configuration
switch by design.

## Open Questions

- Whether `cooldown.relaxation-ignored` should instead fail the load. The
  design session chose clamping so an organization tightening its floor does
  not break repositories; reviewers may prefer the noisier failure.
- Whether to treat an ancestor reached only through a non-first parent (D4,
  side-branch merge) as a rewrite after all. The design treats it as a merge
  because that is the ordinary pull-request shape.
