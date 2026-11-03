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
- `groupRemoteEntries` collapses every source with the same canonical URL and
  ref into one group that is resolved once and recorded as one lock entry,
  even when several source names declare it.
- The exact object store is a full `clone --mirror --no-tags`
  (`cloneExactGitObjectStoreWithContext`), so `rev-list --first-parent`,
  `merge-base --is-ancestor`, `for-each-ref --contains`, and committer dates
  are local reads. Fetching a tag by name into `refs/agent-marketplace/resolved`
  stores the tag object, so its tagger date is readable with `cat-file`. The
  locked commit is present whenever it is still reachable from any branch of
  the mirror; when it is not, Git reports a missing object (exit 128).
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
- v4 loading rejects unknown top-level fields with `v4.unknown-field`, so a
  released client cannot read a configuration that declares `cooldown`.

## Goals / Non-Goals

Goals:

- Advance Git locks only to commits that have been in the ref's first-parent
  history for the effective period, using a clock that a push cannot backdate
  past the newest commit already measured.
- Make a rewritten branch history an explicit, evidenced decision for
  consumers who have opted into a cooldown.
- Keep lock bytes, `sync --locked`, and `sync --check` unchanged, and keep the
  default configuration's behavior identical to today.
- Ship the consumer `cooldown` schema and layering once, for both client
  changes.

Non-Goals:

- Marketplace-source behavior (`marketplace-cooldown-client`).
- Protecting initial acquisition against a repository whose entire history
  was fabricated; there is no anchor, and the ADR records that trust decision
  as the `git:` declaration itself.
- Providing an observation period on a dormant branch: when the newest real
  commit is already older than the period, a push backdated to its date is
  indistinguishable from an old commit. The ADR records this residual; an
  opt-in observed clock is a possible follow-up.
- Refusing history rewrites when no period is configured (follow-up for a
  major release).
- Recording acceptance anywhere other than the resulting lock.
- A runtime flag that bypasses a hold.

## Decisions

### D1: Consumer schema and validation

```yaml
apiVersion: agent-marketplace/v4
cooldown:
  period: 24h                     # Go duration string, or bare 0; default off; negative invalid
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
- `period` is a Go duration string. The bare YAML integer `0` is also
  accepted and means off, so `period: 0` reads naturally; any other bare
  number, a negative duration, or a non-string fails with `v4.cooldown`
  naming the file and field. Documentation writes `0s` where a string is
  shown.
- Per-source `cooldown` accepts only `period`; it is valid on `git` sources,
  rejected on `directory` sources with `v4.source-field`, and (for
  `marketplace-cooldown-client`) valid on `marketplace` sources.
- Unknown keys fail with `v4.unknown-field`; an unknown mode value fails with
  `v4.cooldown`.
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

**Shared identities.** Sources that `groupRemoteEntries` collapses into one
`(url, ref)` group are resolved once, so the group's period is the maximum of
its members' effective periods; a shorter per-source value can never weaken
another source's floor on the same lock entry.

The computation lives next to the allowlist resolution in
`internal/config` and is exposed on the effective configuration so
`acquireLockedRegistry`, the `--bump` planner, previews, and the TUI read one
value.

### D3: Effective commit date and candidate selection

Given a group `(url, ref)` with effective period `P`, cutoff `C = now − P`
(one `now` per invocation), and the exact object store after the existing
fetch of `ref` into `refs/agent-marketplace/resolved`:

1. **`P == 0` keeps today's behavior exactly**: the tip, with no history
   walk, no rewrite check, and no new output. Nothing below runs.
2. Read the first-parent chain from the tip to the root with one
   `git rev-list --first-parent --format=%H%x00%ct%x00%P <tip>`, skipping the
   `commit <hash>` header line Git prepends to each formatted record (or
   using `--no-commit-header` where available). A full first-parent walk is a
   local read of the mirror.
3. Compute each commit's **effective date** from the root upward as the
   running maximum of committer dates along that chain. If
   `refs/agent-marketplace/resolved` is an annotated tag object, the tip's
   effective date is additionally the later of itself and the tagger date read
   with `cat-file`. Other parents are not measured (glossary).
4. **Anchored** (a lock entry exists for the identity, commit `L`): candidates
   are the chain commits that descend from `L`. If `L` is on the chain, that
   is everything strictly above it, and the running maximum is additionally
   seeded with `L`'s committer date. If `L` is an ancestor only through a
   non-first parent (D4), candidates start at the lowest chain commit that
   descends from `L`, with the same seed. Their effective dates still include
   the full first-parent history below them, so a recent side-branch commit
   below the merge point is measured.
5. **Unanchored** (initial acquisition): candidates are the whole chain.
6. The candidate is the newest candidate whose effective date `≤ C`.
7. Anchored and no candidate → **hold**: keep `L`, report the tip's effective
   date `E` and `E + P` as the eligible time.
   Unanchored and no candidate → `initial_acquisition` decides (D5).

A `ref` that is a full commit ID never enters this path (it is already
excluded from `--bump` and resolves to itself).

### D4: History rewrite

Runs only when `P > 0` and the group is anchored. If `L` is not on the tip's
first-parent chain, run `git merge-base --is-ancestor L tip`:

- exit 0: `L` is reached through a non-first parent. That happens when the
  consumed branch was fast-forwarded to a side branch that had merged it; it
  is ordinary history and selection proceeds per D3 step 4.
- exit 1, or exit 128 / "not a valid object name" because `L` is no longer in
  the mirror: a **history rewrite**. Missing-object errors are translated,
  not propagated; transport failures remain transport failures.

A rewrite is refused with `lock.history-rewritten` before any lock, config,
or harness write, and the run's other groups continue to be planned so the
report lists every refusal at once. Today the applying `update` publishes
missing Marketplace lock records (`bootstrapMissingMarketplaceLocks`) before
Git reconciliation runs; to honor the no-write contract, the applying path
stages that evidence the way `update --dry-run` already does
(`prepareMissingMarketplaceLockEdits` into `PreseedLockEdits`) and publishes
it in the same dependency transaction as the Git locks, after every rewrite
check has passed. Evidence per refused source (each row degrades to
"unknown" when `L` is missing):

| Field | Source |
|---|---|
| `url@ref` | the group |
| locked commit, tip | `L`, resolved tip |
| locked commit still reachable | `git for-each-ref --contains L` after refreshing the mirror's remote heads (`fetch --prune <url> +refs/heads/*:refs/heads/*`); "no" on a missing object; labelled "cached" when the refresh fails |
| commits between merge-base and tip | `rev-list --first-parent --count <merge-base>..tip`, or the full count when there is no merge-base |
| tip effective date | D3 step 3 |

The reachability refresh is the only network operation the refusal adds and
runs only on the refusal path.

Followed by the remediation commands:
`agent-marketplace update --dry-run --include-diff --accept-history-rewrite`
(preview the rewritten candidate without writing),
`agent-marketplace update --accept-history-rewrite`, and the targeted form
`--registry <url> --ref <ref> --accept-history-rewrite`, plus the sentence:
"After a rewrite the cooldown cannot protect you from a backdated history;
review the diff before accepting."

`--accept-history-rewrite` is accepted by `update`, `lock --upgrade`, and
`update --dry-run`. With no targeting it accepts every refusal in that run.
With `--registry`/`--ref` the existing upgrade targeting already scopes the
run to the named groups: other groups keep their locked commits and are not
re-resolved, so a second rewritten source cannot refuse a targeted
acceptance; the user handles it in its own run. Acceptance is not persisted:
once the new commit is locked, later runs are anchored on it. After acceptance the group is selected per D3 step 5
(whole chain, measured from its root). If nothing in the rewritten history is
eligible, the **existing lock is kept** and the group is reported as a held
rewrite with its eligible time; the user repeats the acceptance once a commit
is eligible. The initial-acquisition fallback (D5) never applies to an
accepted rewrite, so acceptance cannot take a young tip.

A stale-lock `sync` (configuration changed for the group) is an initial
acquisition for the new identity and does not meet this check; only a
re-resolution of an unchanged identity can observe a rewrite.

### D5: Initial acquisition

Unanchored, not an accepted rewrite, with no eligible commit:

- `initial_acquisition: acknowledge` → take the tip, lock it, and warn with
  `cooldown.initial-acquisition` naming the source, the tip's effective date,
  and the eligible time. The acknowledgement is the `git:` declaration; no
  flag is required for Git sources (ADR 0011).
- `initial_acquisition: strict` → refuse with the same code and the
  remediation "wait until <time> or pin the exact commit in `ref`".

Unanchored with an eligible commit → that commit, silently. This is what a new
consumer of an established repository gets when the tip is fresh.

### D6: `--bump`

`discoverTagBump` gains an eligibility filter when `P > 0`. For each matching
tag in the mutable ref cache: resolve it to its commit; take the tagger date
for an annotated tag (`for-each-ref --format=%(taggerdate:unix)`); compute the
commit's effective date along its first-parent chain from the root, with
candidates restricted to commits at or above the merge-base with the locked
commit; the tag's effective date is the later of the two. Only tags with
effective date `≤ C` compete; the highest eligible numeric suffix wins as
today. Ineligible higher tags are reported as held candidates. No ancestry
between old and new tag is required. The mutable cache is a full clone, so
this is also local.

**Binding the plan to the validated object.** Today the bump plan carries
only the new tag name, which is written into `sources.<name>.ref`, and the
tag is resolved again at acquisition. The mutable cache can be stale (it
falls back to a stale checkout when its fetch fails) and a tag can move
between planning and acquisition, so the plan now records the validated tag
object ID and commit ID alongside the name. Acquisition of the bumped group is
**anchored at the previous lock's commit** (it is not an initial acquisition,
so D5 never applies to a bump), re-runs D3 against the exact store, and must
resolve the new ref to the recorded commit; otherwise it refuses with
`lock.bump-target-moved`, leaves the config edit unpublished, and reports
both commits. A moved-tag test moves the tag after the eligibility check and
asserts that acquisition fetches nothing new.

### D7: Output

- `update` and `lock --upgrade` print, per source whose tip is newer than the
  selected candidate (whether the lock stayed or advanced to an older
  eligible descendant):
  `held <url>@<ref>: tip <short> effective <date>, eligible <date> (period <P>)`
  in the existing update report, before the dependency/materialization
  sections, followed by `advanced to <short>` when the lock moved. A held
  accepted rewrite prints the same line prefixed with `rewrite accepted;`.
  Exit 0 when holds are the only outcome.
- `update --dry-run` prints the same lines and the rewrite refusal, with the
  statement that nothing was applied. With `--accept-history-rewrite` it
  previews the rewritten candidate and, with `--include-diff`, its diff
  against the locked content, still without writing. `--include-diff` diffs
  only selected candidates.
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
  "Accept all" and per-row toggles, then re-runs the update scoped to the
  accepted identities (the same targeting the CLI uses), so unaccepted rows
  keep their locks and cannot refuse the rerun. Cancel leaves everything
  unchanged. A held accepted rewrite is
  reported like any hold.
- Journeys: `tools/tui-qa/tests/update-cooldown-hold.spec.ts` (fixture
  repository with a fresh tip; press `u`; assert the hold text and unchanged
  lock) and `tools/tui-qa/tests/update-history-rewrite-accept.spec.ts`
  (fixture branch rewritten with an eligible commit; press `u`; assert
  refusal; open recovery, accept; assert the lock advanced). Both must fail
  without the change.

### D9: Codes

| Code | Kind | Where |
|---|---|---|
| `v4.cooldown` | error | configuration validation |
| `cooldown.relaxation-ignored` | warning | effective configuration |
| `cooldown.held` | info | update/lock/preview reports, TUI |
| `cooldown.initial-acquisition` | warning (acknowledge) / error (strict) | sync, update, lock, TUI |
| `lock.history-rewritten` | error | update, lock --upgrade, preview, TUI |
| `lock.bump-target-moved` | error | update --bump |

### D10: Time and determinism

`now` is captured once per invocation and passed to every selection so a
long-running update cannot straddle an eligibility boundary. The client
clock is the consumer's own machine; skew only affects the consumer. Given the
same history and the same `now`, selection is a pure function, which is what
lets previews and applying runs agree.

### D11: Rollout across client versions

A released client fails to load any layer that declares `cooldown`
(`v4.unknown-field`), refusing every configuration-dependent command. The
field therefore enters a shared repository or organization file only after
every client that reads it runs this release or newer. The released-client
gate adds a scenario that witnesses the refusal with the pinned stable client,
and the how-to documents the order: upgrade clients, then add the floor.

## Spec delta outline

`consumer-config-format`

- Requirement: Cooldown mapping
  - Scenario: Valid cooldown mapping loads in every layer
  - Scenario: Bare zero period means off
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
  - Scenario: Sources sharing a lock identity use the longest period

`deployed-acquisition-locks`

- Requirement: Git candidates respect the effective cooldown
  - Scenario: Fresh tip is held and the lock is retained
  - Scenario: Older eligible descendant is selected and the held tip is still reported
  - Scenario: Backdated tip is measured by its parent and held
  - Scenario: Backdated push onto a dormant branch is accepted
  - Scenario: Annotated tag ref uses the later of tagger and commit dates
  - Scenario: Full commit ref is exempt
  - Scenario: Zero period resolves the tip without a rewrite check
- Requirement: Initial acquisition selects the newest eligible commit
  - Scenario: Established repository with a fresh tip locks the newest eligible commit
  - Scenario: Entirely young history takes the tip with a warning
  - Scenario: Strict initial acquisition refuses an entirely young history
- Requirement: History rewrites are refused until accepted
  - Scenario: Rewritten branch refuses with evidence and leaves every lock unchanged
  - Scenario: Rewrite refusal publishes no Marketplace lock bootstrap
  - Scenario: Unreachable locked commit is reported as a rewrite
  - Scenario: Locked commit reached through a merge is not a rewrite
  - Scenario: Blanket acceptance advances every refused source
  - Scenario: Targeted acceptance advances only the named source
  - Scenario: Accepted rewrite with an entirely young history is held
- Requirement: Tag bumps respect the effective cooldown
  - Scenario: Fresh higher tag is held and the next eligible tag is selected
  - Scenario: Annotated tag bump uses the later of tagger and commit dates
  - Scenario: Tag moved after planning refuses acquisition

`update-output`

- Requirement: Holds and rewrites are reported
  - Scenario: Hold line names the tip, dates, and period and exits 0
  - Scenario: Partial advancement reports the held tip
  - Scenario: Rewrite refusal lists evidence and the remediation commands
  - Scenario: Held candidates are excluded from content diffs

`update-preview`

- Requirement: Preview reports holds and rewrites without writing
  - Scenario: Dry run reports the same hold as the applying run
  - Scenario: Dry run reports a rewrite and writes nothing
  - Scenario: Dry run with acceptance previews the rewritten candidate without writing

`tui-error-recovery`

- Requirement: History rewrite is recoverable from the TUI
  - Scenario: Recovery action shows evidence and accepting advances the lock
  - Scenario: Cancelling the acceptance modal changes nothing

`tui-feedback`

- Requirement: Update holds are visible
  - Scenario: Held source appears in the update result with its eligible date

## Risks / Trade-offs

- **Dormant branches.** When the newest real commit is already older than
  `P`, a push backdated to its date is accepted at once. The design states
  this instead of claiming an observation period; consumers who need one on
  dormant sources should consume through a Marketplace or wait for the
  opt-in observed clock follow-up.
- **Non-monotonic legitimate histories** (local commit pushed days later onto
  a newer tip) are measured by the parent's date, never flagged; the only
  cost is that such a commit is considered as new as its parent. Accepted.
- **Repositories with huge first-parent histories**: one `rev-list` per
  group per run, over the full chain. `rev-list --first-parent` over a
  hundred thousand commits is sub-second on the mirror; benchmarks in
  `internal/cache` bound it.
- **Rewrites of consumed branches need an explicit flag once a period is
  set.** Opt-in by construction; the evidence and remediation text are what
  make it tolerable for non-experts.
- **Adding a resource to a locked source is held** like any update. Accepted
  in the design session; the per-source and organization period are the
  levers.
- **Shared configurations cannot carry `cooldown` until clients upgrade.**
  Inherent to strict v4 loading; documented rollout order.

## Migration Plan

1. Schema, validation, layering, shared-identity rule, and diagnostics
   (`internal/config`) with tests.
2. History reads and effective-date computation in `internal/cache` with
   fixture repositories covering fresh tips, backdated commits, dormant
   branches, side-branch merges, rewrites, unreachable locked commits, and
   annotated tags.
3. Candidate selection, hold reporting, rewrite refusal and acceptance in
   `cmd/agent-marketplace/lock.go` and `internal/pipeline/update.go`,
   including `--dry-run` parity, preview with acceptance, and `--bump`.
4. TUI diagnostics, recovery action, modal, and the two journeys.
5. E2E scenarios in `make validate-e2e`; released-client gate run to confirm
   lock bytes stay identical and to witness the unknown-field refusal.
6. Documentation and archive in the same pull request.

Rollback is `cooldown: {period: 0s}`, which also disables the rewrite check.

## Open Questions

- Whether `cooldown.relaxation-ignored` should instead fail the load. The
  design session chose clamping so an organization tightening its floor does
  not break repositories; reviewers may prefer the noisier failure.
- Whether to offer the opt-in observed clock (`min` of effective commit date
  and local first-observed time) in this change or a follow-up. The design
  defers it so the first change stays free of new cache state.
