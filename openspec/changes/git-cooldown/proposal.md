## Why

`update` and `lock --upgrade` move every selected Git acquisition lock straight
to the tip of its ref, and accept a tip that no longer descends from the locked
commit without comment. A compromised registry repository therefore reaches
every consumer on their next `update`. A cooldown lets a consumer wait a
configured period before advancing, measured by a clock that a pusher cannot
backdate past the newest commit already in the branch's history (a commit is
never older than its first-parent ancestors), and turns a rewritten branch
history into an explicit decision instead of a silent acceptance.

This is the Git client half of the cooldown design recorded in
[ADR 0011](../../../docs/adr/0011-cooldown-clocks-and-history-rewrite-refusal.md).
It also carries the consumer configuration schema shared with
`marketplace-cooldown-client`.

## What Changes

- Consumer configuration (`agent-marketplace/v4`) gains a `cooldown` mapping in
  every layer: `period` (Go duration string, or the bare integer `0`; default
  off), `initial_acquisition` (`acknowledge`, the default, or `strict`), and
  `missing_evidence` (`warn`, the default, or `refuse`; it has effect only for
  Marketplace sources and is given behavior by `marketplace-cooldown-client`).
  Git sources accept a per-source `cooldown: {period}`; directory sources
  reject the field. Invalid values fail with `v4.cooldown`.
- The cooldown is a floor that layers may only raise. The effective period for
  a source is the maximum of each layer's contribution (its per-source value if
  declared, otherwise its global). A declared value below the floor inherited
  from higher layers is clamped and reported as `cooldown.relaxation-ignored`.
  `initial_acquisition: strict` is likewise never relaxed by a lower layer.
  Sources that share one acquisition-lock identity (same URL and ref) are
  resolved once with the longest of their effective periods.
- Git candidate selection measures the **effective commit date**: the later of
  a commit's committer date and the effective commit date of its first parent,
  computed over the whole first-parent history, plus the tagger date when the
  ref or candidate is an annotated tag. When advancing a lock, the candidate
  is the newest first-parent descendant of the locked commit whose effective
  commit date is at least `period` old. If none exists the lock is kept and
  the tip is reported as a **held candidate** with the date it becomes
  eligible; `update` exits 0. A `ref` that is a full commit ID is exempt. With
  the period at `0` nothing changes.
- When a period applies, a consumed branch whose locked commit is not an
  ancestor of its tip is a **history rewrite**. `update` and `lock --upgrade`
  refuse it with `lock.history-rewritten`, listing per source the locked
  commit, the tip, whether any branch of the refreshed remote still contains
  the locked commit, the number of first-parent commits between the
  merge-base and the tip, and the tip's effective commit date, followed by
  the remediation commands. `--accept-history-rewrite` accepts every refusal in the run, or
  only the sources named by the existing `--registry`/`--ref` targeting, on
  `update`, `lock --upgrade`, and `update --dry-run` (which then previews the
  rewritten candidate without writing). The resulting lock is the only record
  of acceptance. Acceptance does not bypass the cooldown: the rewritten
  history is measured from its root, and when no commit in it is old enough
  the existing lock is kept and the rewrite is reported as held, to be
  accepted again once eligible. The refusal text says that after a rewrite
  the cooldown offers no protection against a backdated history.
- **Initial acquisition** (no lock entry for the source identity) selects the
  newest eligible first-parent commit. When the whole history is younger than
  the period, `acknowledge` takes the tip and warns with
  `cooldown.initial-acquisition`; `strict` refuses with the same code.
- `update --bump` considers only tags whose effective commit date is eligible,
  measured back to the merge-base with the locked commit, does not require
  ancestry between the old and new tag, records the validated tag object and
  commit in its plan, and refuses with `lock.bump-target-moved` if the tag no
  longer resolves to that commit at acquisition.
- The Git clock shortens the exposure window on active branches. On a dormant
  branch, whose newest real commit is already older than the period, a push
  backdated to that date is accepted immediately; the documentation states
  this limit, and an opt-in first-observed clock is a possible follow-up.
- `update`, `update --dry-run`, and `lock --upgrade` print one hold line per
  source whose tip is newer than the selected candidate, including after the
  lock advanced to an older eligible commit; `--include-diff` never diffs a
  held candidate. A rewrite refusal happens before any lock, config, or
  harness write, including the Marketplace lock records an applying `update`
  bootstraps today, which are staged behind the rewrite checks; cache
  refreshes are not writes in that sense. `sync --locked`,
  `sync --check`, `sync --offline`, and `doctor` are unchanged: they never
  re-resolve.
- The TUI shows holds in the update result and surfaces a history rewrite as a
  recoverable update diagnostic whose recovery action opens a confirm modal
  listing the same evidence, with accept-all or per-source acceptance, then
  re-runs the update. Both flows ship real-terminal journeys.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `consumer-config-format`: the `cooldown` mapping, per-source `cooldown`,
  layer permissions, and `v4.cooldown` validation.
- `config-layering`: floor semantics across layers, clamping, shared-identity
  resolution, and the `cooldown.relaxation-ignored` diagnostic.
- `deployed-acquisition-locks`: effective-commit-date candidate selection,
  held candidates, initial-acquisition fallback and strict refusal,
  history-rewrite refusal and acceptance, `--bump` eligibility.
- `update-output`: hold lines, history-rewrite evidence and remediation,
  exclusion of held candidates from diffs.
- `update-preview`: previews use the same candidate selection, report holds
  and rewrites, and can preview an accepted rewrite without writing.
- `tui-error-recovery`: history-rewrite recovery action and confirm modal.
- `tui-feedback`: hold reporting in the update result.

## Impact

- `internal/config`: v4 schema, validation, layer permissions, effective-floor
  computation, diagnostics.
- `internal/cache`: first-parent history listing with committer dates,
  tag-object dates, and ancestry checks on the existing bare mirror object
  store, all local reads (the store is a full `clone --mirror --no-tags`);
  one new network operation, a remote-heads refresh, runs only on the
  rewrite-refusal path to compute the reachability evidence and falls back to
  labelled cached evidence when it fails; tagger dates from the mutable ref
  cache for `--bump`.
- `cmd/agent-marketplace/lock.go` (`reconcileLayerDocument`,
  `acquireLockedRegistry`): candidate selection replaces "resolve the tip";
  rewrite refusal and acceptance; new flag on `update` and `lock`.
- `internal/pipeline/update.go`: `--bump` tag eligibility; hold and rewrite
  reporting in `UpdateResult`; preview parity.
- `internal/tui`: update diagnostics, recovery action, confirm modal;
  `tools/tui-qa/tests/update-cooldown-hold.spec.ts` and
  `tools/tui-qa/tests/update-history-rewrite-accept.spec.ts`; README entries.
- Acquisition lock format is unchanged: effective dates are recomputed from
  history on every run, and `sync --locked`/`sync --check` bytes are identical,
  so the released-client interoperability gate's lock checks are unaffected.
- **Configuration rollout.** Clients that predate this change reject a
  configuration containing the unknown top-level `cooldown` field
  (`v4.unknown-field`), so a shared or organization file may gain `cooldown`
  only after every client that reads it has been upgraded. The released-client
  gate witnesses that refusal, and the how-to states the order: upgrade
  clients, then add the floor.
- E2E: `make validate-e2e` gains hold, initial-acquisition, rewrite, and
  shared-identity scenarios.
- Documentation: `site/docs/reference/configuration.md`,
  `configuration-layers.md`, `lockfiles.md`, and a how-to recommending a
  24-hour organization floor and describing the rollout order and the
  dormant-branch limit of the Git clock.

## Release note

Consumers can set `cooldown: {period: 24h}` in any configuration layer so
`update` advances a Git source only to commits that have been in its history
for that long, and, once a period is set, `update` refuses a branch whose
history was rewritten until `--accept-history-rewrite` is passed. The default
period is `0`, which leaves today's behavior unchanged; upgrade every client
that reads a shared configuration before adding `cooldown` to it. A 24-hour
organization floor is recommended.
