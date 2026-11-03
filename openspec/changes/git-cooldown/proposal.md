## Why

`update` and `lock --upgrade` move every selected Git acquisition lock straight
to the tip of its ref, and accept a tip that no longer descends from the locked
commit without comment. A compromised registry repository therefore reaches
every consumer on their next `update`. A cooldown lets a consumer wait a
configured period before advancing, measured by a clock a pusher cannot
trivially rewrite (a commit is never older than its ancestors), and turns a
rewritten branch history into an explicit decision instead of a silent
acceptance.

This is the Git client half of the cooldown design recorded in
[ADR 0011](../../../docs/adr/0011-cooldown-clocks-and-history-rewrite-refusal.md).
It also carries the consumer configuration schema shared with
`marketplace-cooldown-client`.

## What Changes

- Consumer configuration (`agent-marketplace/v4`) gains a `cooldown` mapping in
  every layer: `period` (Go duration, default `0`), `initial_acquisition`
  (`acknowledge`, the default, or `strict`), and `missing_evidence` (`warn`,
  the default, or `refuse`; it has effect only for Marketplace sources and is
  given behavior by `marketplace-cooldown-client`). Git sources accept a
  per-source `cooldown: {period}`; directory sources reject the field. Invalid
  values fail with `v4.cooldown`.
- The cooldown is a floor that layers may only raise. The effective period for
  a source is the maximum of each layer's contribution (its per-source value if
  declared, otherwise its global). A declared value below the floor inherited
  from higher layers is clamped and reported as `cooldown.relaxation-ignored`.
  `initial_acquisition: strict` is likewise never relaxed by a lower layer.
- Git candidate selection measures the **effective commit date**: the later of
  a commit's committer date and the effective commit date of its first parent,
  plus the tagger date for an annotated tag. When advancing a lock, the
  candidate is the newest first-parent descendant of the locked commit whose
  effective commit date is at least `period` old. If none exists the lock is
  kept and the tip is reported as a **held candidate** with the date it becomes
  eligible; `update` exits 0. A `ref` that is a full commit ID is exempt.
- A consumed branch whose locked commit is not an ancestor of its tip is a
  **history rewrite**. `update`, `lock --upgrade`, and a stale-lock `sync`
  refuse it with `lock.history-rewritten`, listing per source the locked
  commit, the tip, whether the locked commit is still reachable from any
  branch, the number of first-parent commits between the merge-base and the
  tip, and the tip's effective commit date, followed by the two remediation
  commands. `update --accept-history-rewrite` and
  `lock --upgrade --accept-history-rewrite` accept every refusal in the run, or
  only the sources named by the existing `--registry`/`--ref` targeting. The
  resulting lock is the only record of acceptance. Acceptance does not bypass
  the cooldown; the candidate is then measured along the full first-parent
  history, and the refusal text says that after a rewrite the cooldown offers
  no protection against a backdated history.
- **Initial acquisition** (no lock entry for the source identity) selects the
  newest eligible first-parent commit. When the whole history is younger than
  the period, `acknowledge` takes the tip and warns with
  `cooldown.initial-acquisition`; `strict` refuses with the same code.
- `update --bump` considers only tags whose effective commit date is eligible,
  measured back to the merge-base with the locked commit, and does not require
  ancestry between the old and new tag.
- `update`, `update --dry-run`, and `lock --upgrade` print one hold line per
  held source; `--include-diff` never diffs a held candidate. `sync --locked`,
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
- `config-layering`: floor semantics across layers, clamping, and the
  `cooldown.relaxation-ignored` diagnostic.
- `deployed-acquisition-locks`: effective-commit-date candidate selection,
  held candidates, initial-acquisition fallback and strict refusal,
  history-rewrite refusal and acceptance, `--bump` eligibility.
- `update-output`: hold lines, history-rewrite evidence and remediation,
  exclusion of held candidates from diffs.
- `update-preview`: previews use the same candidate selection and report holds
  and rewrites without writing.
- `tui-error-recovery`: history-rewrite recovery action and confirm modal.
- `tui-feedback`: hold reporting in the update result.

## Impact

- `internal/config`: v4 schema, validation, layer permissions, effective-floor
  computation, diagnostics.
- `internal/cache`: first-parent history listing with committer dates and
  ancestry checks on the existing bare mirror object store (no new network
  operations; the store is a full `clone --mirror --no-tags`), tagger dates
  from the mutable ref cache for `--bump`.
- `cmd/agent-marketplace/lock.go` (`reconcileLayerDocument`,
  `acquireLockedRegistry`): candidate selection replaces "resolve the tip";
  rewrite refusal and acceptance; new flags on `update` and `lock`.
- `internal/pipeline/update.go`: `--bump` tag eligibility; hold and rewrite
  reporting in `UpdateResult`; preview parity.
- `internal/tui`: update diagnostics, recovery action, confirm modal;
  `tools/tui-qa/tests/update-cooldown-hold.spec.ts` and
  `tools/tui-qa/tests/update-history-rewrite-accept.spec.ts`; README entries.
- Acquisition lock format is unchanged: effective dates are recomputed from
  history on every run, and `sync --locked`/`sync --check` bytes are identical,
  so the released-client interoperability gate is unaffected.
- E2E: `make validate-e2e` gains hold, initial-acquisition, and rewrite
  scenarios.
- Documentation: `site/docs/reference/configuration.md`,
  `configuration-layers.md`, `lockfiles.md`, and a how-to recommending a
  24-hour organization floor.

## Release note

Consumers can set `cooldown: {period: 24h}` in any configuration layer so
`update` advances a Git source only to commits that have been in its history
for that long, and `update` now refuses a branch whose history was rewritten
until `--accept-history-rewrite` is passed. The default period is `0`; a
24-hour organization floor is recommended.
