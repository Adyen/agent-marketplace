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
- Candidate selection for a Marketplace coordinate reads `latest`; when the
  returned revision's `first_published_at` is younger than the consumer floor,
  the client reads the revisions list and selects the eligible revision with
  the most recent first publication that is also later than the current pin's.
  Revoked and rejected revisions are skipped as today. No new API semantics are
  required beyond the fields the server change exposes.
- A `409 marketplace.cooldown.held` from `latest`, or no eligible revision
  under the consumer floor, is a **held candidate** for an existing pin:
  `update` keeps the pin, reports the eligible date, and exits 0.
- For an **initial acquisition** (no pin for the coordinate in the owning
  layer) with no eligible revision, `install` requires
  `--acknowledge-cooldown`; without it, it refuses with
  `cooldown.acknowledgement-required` naming the eligible date and the flag.
  With it, the head revision is fetched by exact digest and pinned as an
  ordinary floating Marketplace installation, so later `update`s behave
  normally. `initial_acquisition: strict` refuses regardless. Under the
  server's `held: block`, the artifact is unavailable and the install fails
  closed. The TUI asks the same question in a confirm modal that shows the
  age and eligible date.
- A writing `sync` that must create a missing Marketplace lock record for a
  held coordinate refuses with the same code and points at
  `agent-marketplace install <locator> --acknowledge-cooldown`; it grows no
  flag of its own, and `sync --locked`, `sync --check`, `sync --offline`, and
  `doctor` are unchanged.
- `missing_evidence` gains behavior: when a Marketplace omits
  `first_published_at`, `warn` proceeds as today and reports
  `cooldown.evidence-missing` once per endpoint; `refuse` fails candidate
  selection for that endpoint.
- `install` prints a hold line when it resolves to an older eligible revision
  because the head is held; `update` and `update --dry-run` print hold lines
  for Marketplace pins in the same format as Git holds.
- Collections need no special rule: a collection revision is the candidate,
  and its manifest's member revisions are at least as old as it. Index-only
  acquisition pins the exact occurrence of the selected revision as today.
- The TUI shows "held until <date>" in the Marketplace detail view when the
  head is held, shows Marketplace holds in the update result, and ships
  journeys for the acknowledgement modal and the hold. The TUI QA Marketplace
  stub serves `first_published_at` and the `409` outcome.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `resource-installation`: Marketplace candidate selection under the consumer
  floor, `--acknowledge-cooldown`, `initial_acquisition: strict`,
  `missing_evidence` behavior, hold line on install.
- `deployed-acquisition-locks`: writing `sync` refusal for a held initial
  Marketplace acquisition; holds never rewrite existing Marketplace records.
- `update-output`: Marketplace hold lines; `409` evidence in the report.
- `update-preview`: preview parity for Marketplace holds.
- `tui-interactions`: acknowledgement confirm modal in the Marketplace install
  flow; held head in the detail view.
- `tui-feedback`: Marketplace holds in the update result.

## Impact

- `internal/marketplace` (`install.go`, `resolver.go`, `client.go`): parse
  `first_published_at`, `current_availability.cooldown_until`, and the `409`
  body; eligible-revision selection over the revisions list; acknowledgement
  path; cache entries carry first publication.
- `internal/pipeline/update.go` (`planMarketplaceUpdates`): hold decisions and
  reporting; preview parity.
- `cmd/agent-marketplace/install.go`: `--acknowledge-cooldown`; witness tests.
- `cmd/agent-marketplace/sync.go` and `marketplace_lock_bootstrap.go`: refusal for held initial
  acquisitions.
- `internal/tui`: detail view, install confirm modal, update diagnostics;
  `tools/tui-qa/tests/marketplace-install-acknowledge-cooldown.spec.ts`,
  `tools/tui-qa/tests/marketplace-update-cooldown-hold.spec.ts`,
  `marketplace-stub.ts` extensions; README entries.
- Lock format unchanged; a pin's `revision` and `content_digest` already
  identify the chosen revision. Released-client interoperability is unaffected.
- E2E: the Marketplace public gate and `make validate-e2e` gain consumer-floor
  and acknowledgement scenarios.
- Documentation: `site/docs/how-to/install-from-a-marketplace.md`,
  `site/docs/reference/configuration.md` (Marketplace rows of the cooldown
  table), `lockfiles.md`.

## Release note

Consumers can apply their own `cooldown` to Marketplace sources: `update`
holds a pin until a newer revision has been published for the configured
period, and installing a resource whose every revision is still young requires
`install --acknowledge-cooldown` (or `initial_acquisition: strict` to forbid
it). The default period is `0`.
