## Why

`marketplace-cooldown-server` lets a Marketplace operator hold young resource
revisions, but an organization that does not operate the Marketplace it
consumes from cannot ask for a longer period, and a consumer sees a held
coordinate only as an error. This change lets consumers apply their own
cooldown floor on top of the server's, reports holds the same way the Git
client does, and makes an initial acquisition of a young resource an explicit,
acknowledged choice instead of a silent one.

This is the Marketplace client half of the design recorded in
[ADR 0011](../../../docs/adr/0011-cooldown-clocks-and-history-rewrite-refusal.md).
It depends on the schema shipped by `git-cooldown` and the API fields shipped
by `marketplace-cooldown-server`.

## What Changes

- The consumer `cooldown` mapping and per-source `cooldown: {period}` from
  `git-cooldown` apply to `marketplace` sources. The effective period for a
  Marketplace installation is the consumer floor; the server floor is applied
  by the Marketplace itself and is never lowered by a client.
- Candidate selection for a Marketplace coordinate reads `latest`; with a
  zero period nothing else happens. A `200` response identifies whether it
  is the registry head or a server-side fallback (`held_head` present). The
  head is followed when it is eligible under the consumer floor, including
  after a registry revert to older content. A fallback, or a head younger
  than the consumer floor, is accepted only when it is eligible under the
  consumer floor and the registry published it more recently than the
  current pin; otherwise the client walks the revisions list, most recently
  published first, and selects the first eligible revision newer than the
  pin, skipping revoked and rejected revisions as today. A pin is therefore
  never replaced by content the registry published less recently.
- A `409 marketplace.cooldown.held` from `latest`, or no eligible revision
  under the consumer floor, is a **held candidate** for an existing pin:
  `update` keeps the pin, reports the eligible date, and exits 0. A `409`
  whose body cannot be parsed is still a hold, with an unknown eligible date.
- For an **initial acquisition** (no pin for the coordinate in the owning
  layer) with no eligible revision, `install` requires
  `--acknowledge-cooldown`; without it, it refuses with
  `cooldown.acknowledgement-required` naming the eligible date and the flag.
  With it, the head revision identified by the server (`held_head.revision`
  or the `409` body) is fetched by exact digest and pinned as an ordinary
  floating Marketplace installation, so later `update`s behave normally.
  `initial_acquisition: strict`, or a server reporting
  `acknowledgeable: false` (`held: strict`), refuses regardless; a `409` body
  that does not identify the head refuses too, because the client cannot know
  what it would be acknowledging. The TUI asks the same question in a confirm
  modal that shows the age and eligible date.
- A writing `sync` that must create a missing Marketplace lock record for a
  held coordinate refuses with the same code and points at
  `agent-marketplace install <locator> --acknowledge-cooldown`; it grows no
  flag of its own, and `sync --locked`, `sync --check`, `sync --offline`, and
  `doctor` are unchanged. This refusal reaches only consumers who declared a
  `cooldown` period; `git-cooldown`'s documentation states that the same
  mapping governs Marketplace sources once this change ships, so the scope
  of the opt-in is known before anyone sets it, and the two client changes
  should release together where possible.
- Candidate ordering is by what the registry published most recently, so a
  revision the registry rolled back from is never restored, and a collection
  is a candidate only when every member it selects, transitively, is
  eligible under the server's evidence and the consumer floor.
- `missing_evidence` gains behavior: when a Marketplace omits
  `first_published_at`, `warn` proceeds as today and reports
  `cooldown.evidence-missing` once per endpoint; `refuse` fails candidate
  selection for that endpoint.
- `install` prints a hold line when it resolves to an older eligible revision
  because the head is held, using the server's `held_head` evidence; `update`
  and `update --dry-run` print hold lines for Marketplace pins in the same
  format as Git holds, with the eligible date computed as
  `first_published_at + period` or the server's `cooldown_until`, whichever is
  later.
- Collections need no special rule: a collection revision is the candidate,
  and its manifest's member revisions are at least as old as it. Index-only
  acquisition pins the exact occurrence of the selected revision as today.
- The TUI shows "held until <date>" in the Marketplace detail view when the
  head is held, shows Marketplace holds in the update result, and ships
  journeys for the acknowledgement modal and the hold. The TUI QA Marketplace
  stub serves `first_published_at`, `held_head`, the `cooldown` object, and
  the `409` outcome.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `resource-installation`: Marketplace candidate selection under the consumer
  floor, no-downgrade rule, `--acknowledge-cooldown`,
  `initial_acquisition: strict` and server `acknowledgeable: false`,
  `missing_evidence` behavior, hold line on install.
- `deployed-acquisition-locks`: writing `sync` refusal for a held initial
  Marketplace acquisition; holds never rewrite existing Marketplace records.
- `update-output`: Marketplace hold lines; `409` and `held_head` evidence in
  the report.
- `update-preview`: preview parity for Marketplace holds.
- `tui-interactions`: acknowledgement confirm modal in the Marketplace install
  flow; held head in the detail view.
- `tui-feedback`: Marketplace holds in the update result.

## Impact

- `internal/marketplace` (`install.go`, `resolver.go`, `client.go`): parse
  `first_published_at`, `current_availability.cooldown`, `held_head`, and the
  `409` body; eligible-revision selection with the no-downgrade rule;
  acknowledgement path; cache entries carry first publication.
- `internal/pipeline/update.go` (`planMarketplaceUpdates`): hold decisions and
  reporting; preview parity.
- `cmd/agent-marketplace/install.go`: `--acknowledge-cooldown`; witness tests.
- `cmd/agent-marketplace/sync.go` and `marketplace_lock_bootstrap.go`: refusal
  for held initial acquisitions.
- `internal/tui`: detail view, install confirm modal, update diagnostics;
  `tools/tui-qa/tests/marketplace-install-acknowledge-cooldown.spec.ts`,
  `tools/tui-qa/tests/marketplace-update-cooldown-hold.spec.ts`,
  `marketplace-stub.ts` extensions; README entries.
- Lock format unchanged; a pin's `revision` and `content_digest` already
  identify the chosen revision. Released-client interoperability is unaffected
  beyond the configuration rollout rule documented by `git-cooldown`.
- E2E: the Marketplace public gate and `make validate-e2e` gain consumer-floor,
  no-downgrade, revert, and acknowledgement scenarios.
- Documentation: `site/docs/how-to/install-from-a-marketplace.md`,
  `site/docs/reference/configuration.md` (Marketplace rows of the cooldown
  table), `lockfiles.md`.

## Release note

Consumers can apply their own `cooldown` to Marketplace sources: `update`
holds a pin until a newer revision has been published for the configured
period, and installing a resource whose every revision is still young requires
`install --acknowledge-cooldown` (or `initial_acquisition: strict` to forbid
it). The default period is `0`.
