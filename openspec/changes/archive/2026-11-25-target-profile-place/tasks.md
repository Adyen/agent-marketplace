# Tasks

Test-first for each requirement. Annotate every scenario's witness with `// spec: target-profile-place/<scenario-slug>`. Use test-only profiles; ship no host profile.

## 1. Profile identity, selection and binding
- [x] 1.1 Exact profile identity, digest and scoped locator in selection and acquisition locks; refuse built-in/external ambiguity, unsupported IDs, malformed paths and invalid scope before acquisition.
- [x] 1.2 Package assessment binds package identity, mapping and full profile digest, online and offline.
- [x] 1.3 `target enable|disable` change only the target ID for a selected, known, policy-admitted profile; write `harnesses:` when no list is authored.
- [x] 1.4 Selection outcomes: direct refusal, collection skip reporting, headless pending-consent skip, `sync --check` read-only, malformed or repository-scope errors, no pruning on consent, availability or environment change.

## 2. Approval and retirement
- [x] 2.1 Interactive approval receipts with exact OS, target, profile digest, canonical root, state store, command roles and executable digest; recheck the executable before execution.
- [x] 2.2 Organization preapproval with exact canonical roots only; mismatches produce no receipt.
- [x] 2.3 Root, alias, environment or XDG changes require new approval and never relocate an installation; retirement by installation ID, TTY only, keeping edited and unowned bytes.

## 3. Place engine
- [x] 3.1 `packagegen` place generations, `BuildKey` and `GarbageCollect` retention through ownership refs and pending journals.
- [x] 3.2 `dependencytxn` place authority branch: early durable intent, agent-owned stage outside the target root, topological publish order, journal size refusal, no `AuthorityLegacy`.
- [x] 3.3 Whole-set conflict planning with a normalized path trie; unowned, new/new, installed/new and prune-reservation refusals.
- [x] 3.4 Root binding: pinned identity, re-stat `SameObject`, canonical spelling only, XDG state unavailable.
- [x] 3.5 Device check against the nearest existing ancestor; `EXDEV` handling with no fallback.
- [x] 3.6 Journaled directory creation with recorded identity; prune of empty `SameObject` directories only.
- [x] 3.7 Ownership participant commit after publication; marked prune phase in reverse dependency order.

## 4. Recovery
- [x] 4.1 Read-only `dependency.recovery-required` without replay; other commands refuse while a place journal is pending.
- [x] 4.2 Recovery state machine per Decision 14's table; every stop names the journal and the path; e2e crash points added to `fault_e2e.go` for the stage, directory, publish, ownership and prune boundaries.

## 5. Drift and verification
- [x] 5.1 Edited and deleted files keep the projection, report drift, refuse upgrade as a unit, and cede on deselection.
- [x] 5.2 Initial-load check after commit and prune; per-generation result record; retry; failure keeps files.
- [x] 5.3 `target verify <locator> --installation <id>`: named check only; refuse drift without writes.

## 6. Delivery
- [x] 6.1 CLI reference and how-to docs for `target enable|disable|verify` and approval or retirement; public command witnesses in `TestPublicCommandPathsHaveBehaviorTests`.
- [ ] 6.2 `make spec-check`, `make validate`, `make validate-e2e`; archive this change in the final commit of the PR.
  Final-candidate verification is recorded in the Ship-It ledger after the archive commit.
