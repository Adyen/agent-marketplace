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
  `first_published_at` and the `current_availability.cooldown` object.
- After `marketplace-cooldown-server`, a `200` from `latest` is either the
  registry head or a server-side fallback marked by `held_head`.
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
- Never replace a pin with older content as a cooldown side effect.
- Report Marketplace holds exactly like Git holds, including when the server
  already substituted an older eligible revision.
- Make initial acquisitions of young resources explicit, with strict as an
  option on either side.
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

For a coordinate with consumer period `P`, cutoff `C = now − P` (one `now`
per invocation), and current pin `pin` (absent for an initial acquisition):

1. `GET latest`.
   - `404` → missing resource, unchanged handling.
   - `409` with `code: marketplace.cooldown.held` → no revision is eligible
     under the server floor. Record the body's `revision`,
     `first_published_at`, `cooldown_until`, and `acknowledgeable`; a body
     that fails to parse records none of them. Go to step 3 with an empty
     eligible set.
   - `200` without `first_published_at` → D5.
   - `200` that is the **head** (no `held_head`) and
     `first_published_at ≤ C` → candidate found. This is also the `P == 0`
     path, unchanged from today, and it is how a registry revert to older
     content is followed: the head is what the registry publishes, so
     following it is an ordinary update, not a cooldown decision.
   - `200` that is a **fallback** (`held_head` present) or a head younger
     than `C` → the returned revision is a candidate only if
     `first_published_at ≤ C` and, when `pin` exists, its first publication
     is later than `pin`'s; otherwise go to step 2. Keep `held_head` for
     reporting.
2. `GET revisions`; keep revisions with `first_published_at ≤ C` and, when
   `pin` exists, first publication later than `pin`'s. Order by first
   publication descending and take the first one that passes
   `enforceMarketplaceUse`. Index-only pins use that revision's occurrence
   commit through `GetRegistryOccurrence` as today.
3. No candidate:
   - `pin` exists → **hold** (D3).
   - Initial acquisition → D4.

Selection is identical for `install`, `update`, `update --dry-run`, and the
TUI; `planMarketplaceUpdates` and `prepareLocator` share one helper. A pin
with an explicit `revision:` in configuration is exact and skips all of this.
The client compares its own `now` with server-assigned timestamps; a client
clock slightly behind the server's can cause one extra `revisions` request
when a revision has just become eligible. That request is bounded and
harmless, so no skew tolerance is applied: a tolerance would silently shorten
the consumer floor.

### D3: Holds

A held Marketplace pin is reported as
`held <endpoint> <registry>/<kind>/<name>: head <short digest> first published <date>, eligible <date> (period <P>)`.
The head and its first publication come from `held_head` or the `409` body;
the eligible date is the later of `head.first_published_at + P` and the
server's `cooldown_until`, or "unknown" when a `409` body could not be
parsed. The pin, its lock record, and installed files are untouched; `update`
exits 0 when holds are the only outcome; `--include-diff` does not diff held
candidates. `install` prints the same line when step 1 or 2 selected an older
eligible revision while the head is held. The TUI shows the line as an update
info diagnostic.

### D4: Initial acquisition and acknowledgement

No pin for the coordinate in the owning layer and no eligible revision. The
head to acknowledge is `held_head.revision` from a `200` fallback, or
`revision` from the `409` body; the acknowledgement is refused when neither
is available, because the client cannot know what it would be pinning.

- `initial_acquisition: acknowledge`, the head is known, the server reports
  `acknowledgeable: true` (or no `cooldown` object), and
  `install --acknowledge-cooldown` is given → fetch the head by exact digest
  (`GET …/revisions/{digest}`), run the ordinary `enforceMarketplaceUse` and
  artifact verification, and pin it as a normal floating Marketplace
  installation.
- `acknowledge` without the flag → refuse with
  `cooldown.acknowledgement-required` naming the coordinate, the head's first
  publication, the eligible date, and the exact command to run.
- `strict`, or `acknowledgeable: false` from the server → refuse with the
  same code and a message naming which side disabled acknowledgement.
- Head unknown (unparseable `409` body) → refuse with the same code and the
  eligible date reported as unknown.

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

Cached revision metadata gains `first_published_at` and the `cooldown`
object. Cached and cached-no-fetch resolution never advance pins, so the only
effect is that the TUI detail view can show a held head from cache.
`sync --offline` is unchanged.

### D7: TUI

- Detail view: when the catalog head's `current_availability.cooldown.held`
  is true, show "Held until <date>" beside the version information, using
  the words held/eligible only.
- Install flow: when preparation reports `cooldown.acknowledgement-required`
  and acknowledgement is possible, open a confirm modal with the coordinate,
  age, eligible date, and the sentence that installing now skips the
  cooldown; confirm proceeds as `--acknowledge-cooldown`, cancel changes
  nothing. When acknowledgement is disabled by either side, the diagnostic is
  shown with no confirm action.
- Update result: Marketplace holds as info diagnostics (D3).
- Journeys: `tools/tui-qa/tests/marketplace-install-acknowledge-cooldown.spec.ts`
  (stub serves only a young revision and a `409`; install; assert the modal
  text; confirm; assert the pin and lock record) and
  `tools/tui-qa/tests/marketplace-update-cooldown-hold.spec.ts` (installed
  pin, stub advances to a young head and serves the pin as a fallback with
  `held_head`; press `u`; assert the hold and unchanged lock). The stub gains
  `first_published_at`, the `cooldown` object, `held_head`, and the `409`
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
  - Scenario: Young head resolves to the newest eligible older revision
  - Scenario: Install reports the held head when it selects an older revision
  - Scenario: Server fallback older than the pin is held, not installed
  - Scenario: Eligible head after a revert is followed
  - Scenario: Explicit revision pins skip the cooldown
- Requirement: Initial acquisitions of young resources are acknowledged
  - Scenario: Install without acknowledgement refuses with the eligible date
  - Scenario: Acknowledged install pins the server-identified head as a floating installation
  - Scenario: Acknowledged install after a revert pins the head, not the youngest revision
  - Scenario: Strict initial acquisition refuses acknowledgement
  - Scenario: Server non-acknowledgeable hold refuses acknowledgement
  - Scenario: Unparseable hold body refuses acknowledgement and reports an unknown date
- Requirement: Missing first publication follows missing_evidence
  - Scenario: Warn proceeds and reports once per endpoint
  - Scenario: Refuse fails selection for the endpoint

`deployed-acquisition-locks`

- Requirement: Held Marketplace candidates never change lock records
  - Scenario: Writing sync refuses a held initial Marketplace acquisition and points at install
  - Scenario: Held update leaves the Marketplace lock record byte-identical

`update-output`

- Requirement: Marketplace holds are reported
  - Scenario: Held pin prints the hold line with the head's eligible date and exits 0
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
  skew, which only affects that consumer, and at worst costs one extra list
  request (D2).
- **Acknowledgement is per install, not remembered.** A later `update` applies
  the cooldown normally; this is the intended "floating pin" outcome and is
  what avoids pushing users to `revision:` pins.
- **Old servers**: with `P > 0` and no `first_published_at`, `warn` keeps
  consumers working but unprotected until the operator upgrades; the warning
  names the endpoint so the gap is visible.
- **Server `held: strict` is honored by conforming clients only.** The
  server cannot distinguish an acknowledgement fetch from a pin resolution,
  so the client is where the refusal lives (ADR 0011).

## Migration Plan

1. Client parsing of the new fields, `held_head`, and the `409` body; cache
   fields.
2. Shared selection helper with the no-downgrade rule and hold reporting in
   install and update, with preview parity.
3. `--acknowledge-cooldown`, strict and non-acknowledgeable refusals,
   writing-sync refusal, witness tests.
4. TUI detail, modal, update diagnostics, stub extensions, two journeys.
5. E2E scenarios in the public gate and `make validate-e2e`; documentation.
6. Archive in the same pull request.

Rollback is `cooldown: {period: 0s}` on the consumer side.

## Open Questions

- Whether `install` should offer the acknowledgement interactively on a TTY
  (prompt) in addition to the flag. The design keeps the CLI non-interactive
  for parity with scripts and leaves prompting to the TUI.
- Whether a hold should also be surfaced by `status` from cached metadata.
  Deferred per the design session unless cheap.
