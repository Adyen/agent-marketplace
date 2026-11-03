## Context

See `proposal.md` and
[ADR 0011](../../../docs/adr/0011-cooldown-clocks-and-history-rewrite-refusal.md).
Verified current behavior:

- `install` without a `revision:` and `update` for a Marketplace installation
  call `GET /api/v1/resources/{registry}/{kind}/{name}` through
  `Client.GetLatestResourceRevision` (`internal/marketplace/install.go` lines
  around 158 and 299) and validate the response with
  `validateLatestResourceRevision` and `enforceMarketplaceUse`.
- `enforceMarketplaceUse` checks only the assessment decision and revocation.
  An unknown `current_availability.availability` value is not a refusal
  (`TestOperatorExposedAssessmentIsUsable` shows "unknown" only fails to
  *grant* exposure). The server change therefore expresses a hold as a `409`
  on `latest`, which today's client turns into
  `resolve latest resource revision: <APIError>` and fails closed.
- The revisions list route exists (`listResourceRevisions`), returns assessed
  revisions, and after `marketplace-cooldown-server` carries
  `first_published_at`.
- Writing `sync` records selected Marketplace resources missing from an
  existing lock (`deployed-acquisition-locks`); read-only modes report
  `v4.marketplace-lock-mismatch` and never write.
- Cached revision metadata in `internal/marketplace/cache.go` drops
  `EffectiveAvailability`; `cachedRevisionUsable` governs reuse per mode.
- TUI Marketplace journeys run against `tools/tui-qa/tests/marketplace-stub.ts`.

## Goals / Non-Goals

Goals:

- Apply a consumer floor on top of the server floor without new API
  semantics.
- Report Marketplace holds exactly like Git holds.
- Make initial acquisitions of young resources explicit, with strict as an option.
- Degrade safely against Marketplaces that predate the server change.

Non-Goals:

- Server-side behavior and operator configuration.
- Changing what `sync --locked`, `sync --check`, `sync --offline`, or `doctor`
  do; they never advance a pin.
- A runtime bypass for a held update.

## Decisions

### D1: Effective period for a Marketplace installation

The consumer floor for a `marketplace` source is computed by `git-cooldown`'s
D2 (per-source `period`, else layer global, maximum across layers). The server
floor is not known to the client and does not need to be: `latest` already
excludes revisions under it, and `cooldown_until` in responses already
reflects it. The client never requests anything younger than `latest` offers.

### D2: Candidate selection

For a coordinate with consumer period `P` and cutoff `C = now − P`
(one `now` per invocation):

1. `GET latest`.
   - `200`: if `first_published_at` is absent → D5. If
     `first_published_at ≤ C` → candidate found (this is also the `P == 0`
     path, unchanged from today). Otherwise → step 2.
   - `409` with `code: marketplace.cooldown.held` → no revision is eligible
     under the server floor; remember `revision` and `cooldown_until` from the
     body → step 3 with an empty eligible set.
   - `404` → missing resource, unchanged handling.
2. `GET revisions`; keep revisions with `first_published_at ≤ C` and, for an
   existing pin, `first_published_at` later than the pin's. Order by first
   publication descending and take the first one that passes
   `enforceMarketplaceUse`. Index-only pins use that revision's occurrence
   commit through `GetRegistryOccurrence` as today.
3. No candidate:
   - Existing pin → **hold** (D3).
   - Initial acquisition → D4.

Selection is identical for `install`, `update`, `update --dry-run`, and the
TUI; `planMarketplaceUpdates` and `prepareLocator` share one helper. A pin
with an explicit `revision:` in configuration is exact and skips all of this.

### D3: Holds

A held Marketplace pin is reported as
`held <endpoint> <registry>/<kind>/<name>: head <short digest> first published <date>, eligible <date> (period <P>)`
where the eligible date is the later of the consumer cutoff and the server's
`cooldown_until` when present. The pin, its lock record, and installed files
are untouched; `update` exits 0 when holds are the only outcome;
`--include-diff` does not diff held candidates. The TUI shows the same line as
an update info diagnostic.

### D4: Initial acquisition and acknowledgement

No pin for the coordinate in the owning layer and no eligible revision:

- `initial_acquisition: acknowledge` and `install --acknowledge-cooldown`
  given → fetch the head revision by exact digest (`GET …/revisions/{digest}`
  using the `409` body's `revision`, or the youngest listed revision when the
  consumer floor alone held it), run the ordinary `enforceMarketplaceUse` and
  artifact verification, and pin it as a normal floating Marketplace
  installation. Under the server's `held: block` the artifact route refuses
  and the install fails closed with the server's reason.
- `acknowledge` without the flag → refuse with
  `cooldown.acknowledgement-required` naming the coordinate, the head's first
  publication, the eligible date, and the exact command to run.
- `strict` → refuse with the same code and the message that acknowledgement
  is disabled by configuration.

The flag is accepted only by `install`; a writing `sync` that reaches this
state for a missing lock record refuses with the same code and points at the
`install` command. Nothing is written in either refusal. Collections take the
same path with the collection revision as the head.

### D5: Missing evidence

When a `200` revision response has no `first_published_at`, the Marketplace
predates the server change. `missing_evidence: warn` (default) proceeds with
today's behavior and reports `cooldown.evidence-missing` once per endpoint per
invocation; `refuse` fails candidate selection for every coordinate on that
endpoint with the same code. With `P == 0` the field is never consulted, so
existing consumers see no warning until they opt in.

### D6: Cache and offline modes

Cached revision metadata gains `first_published_at`. Cached and
cached-no-fetch resolution never advance pins, so the only effect is that the
TUI detail view can show a held head from cache. `sync --offline` is
unchanged.

### D7: TUI

- Detail view: when the catalog head's `current_availability.availability` is
  `cooling-down`, show "Held until <date>" beside the version information,
  using the words held/eligible only.
- Install flow: when preparation reports `cooldown.acknowledgement-required`
  in `acknowledge` mode, open a confirm modal with the coordinate, age,
  eligible date, and the sentence that installing now skips the cooldown;
  confirm proceeds as `--acknowledge-cooldown`, cancel changes nothing. In
  `strict` mode the diagnostic is shown with no confirm action.
- Update result: Marketplace holds as info diagnostics (D3).
- Journeys: `tools/tui-qa/tests/marketplace-install-acknowledge-cooldown.spec.ts`
  (stub serves only a young revision; install; assert the modal text; confirm;
  assert the pin and lock record) and
  `tools/tui-qa/tests/marketplace-update-cooldown-hold.spec.ts` (installed
  pin, stub advances to a young head; press `u`; assert the hold and unchanged
  lock). The stub gains `first_published_at` per revision and the `409`
  response for `latest`.

### D8: Codes

| Code | Kind | Where |
|---|---|---|
| `cooldown.held` | info | install, update, preview, TUI |
| `cooldown.acknowledgement-required` | error | install, writing sync, TUI |
| `cooldown.evidence-missing` | warning (`warn`) / error (`refuse`) | any Marketplace candidate selection |

## Spec delta outline

`resource-installation`

- Requirement: Marketplace candidates respect the consumer cooldown
  - Scenario: Young latest resolves to the newest eligible older revision
  - Scenario: Install reports the held head when it selects an older revision
  - Scenario: Server-held coordinate with an eligible older revision installs it
  - Scenario: Explicit revision pins skip the cooldown
- Requirement: Initial acquisitions of young resources are acknowledged
  - Scenario: Install without acknowledgement refuses with the eligible date
  - Scenario: Acknowledged install pins the head as a floating installation
  - Scenario: Strict initial acquisition refuses acknowledgement
  - Scenario: Acknowledged install fails closed when the server blocks held artifacts
- Requirement: Missing first publication follows missing_evidence
  - Scenario: Warn proceeds and reports once per endpoint
  - Scenario: Refuse fails selection for the endpoint

`deployed-acquisition-locks`

- Requirement: Held Marketplace candidates never change lock records
  - Scenario: Writing sync refuses a held initial Marketplace acquisition and points at install
  - Scenario: Held update leaves the Marketplace lock record byte-identical

`update-output`

- Requirement: Marketplace holds are reported
  - Scenario: Held pin prints the hold line and exits 0
  - Scenario: Held pins are excluded from content diffs

`update-preview`

- Requirement: Preview reports Marketplace holds without writing
  - Scenario: Dry run reports the same Marketplace hold as the applying run

`tui-interactions`

- Requirement: Marketplace install acknowledges a cooldown
  - Scenario: Confirming the acknowledgement modal installs and records the pin
  - Scenario: Cancelling the acknowledgement modal changes nothing
- Requirement: Held heads are visible in the detail view
  - Scenario: Detail view shows the eligible date for a held head

`tui-feedback`

- Requirement: Marketplace holds appear in the update result
  - Scenario: Held pin appears with its eligible date after update

## Risks / Trade-offs

- **Two round trips when the head is young** (`latest` then `revisions`).
  Only paid when a hold is possible; the list route is already paginated and
  bounded.
- **Consumer and server clocks differ.** The client compares its own `now`
  with server-assigned `first_published_at`; skew shifts eligibility by the
  skew, which only affects that consumer.
- **Acknowledgement is per install, not remembered.** A later `update` applies
  the cooldown normally; this is the intended "floating pin" outcome and is
  what avoids pushing users to `revision:` pins.
- **Old servers**: with `P > 0` and no `first_published_at`, `warn` keeps
  consumers working but unprotected until the operator upgrades; the warning
  names the endpoint so the gap is visible.

## Migration Plan

1. Client parsing of the new fields and the `409` body; cache field.
2. Shared selection helper and hold reporting in install and update, with
   preview parity.
3. `--acknowledge-cooldown`, strict refusal, writing-sync refusal, witness
   tests.
4. TUI detail, modal, update diagnostics, stub extensions, two journeys.
5. E2E scenarios in the public gate and `make validate-e2e`; documentation.
6. Archive in the same pull request.

Rollback is `cooldown: {period: 0}` on the consumer side.

## Open Questions

- Whether `install` should offer the acknowledgement interactively on a TTY
  (prompt) in addition to the flag. The design keeps the CLI non-interactive
  for parity with scripts and leaves prompting to the TUI.
- Whether a hold should also be surfaced by `status` from cached metadata.
  Deferred per the design session unless cheap.
