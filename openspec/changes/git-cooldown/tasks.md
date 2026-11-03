## 0. Preconditions

- [ ] 0.1 Remove `skip_specs: true` from `.openspec.yaml` and add the spec
  deltas outlined in `design.md` (`consumer-config-format`, `config-layering`,
  `deployed-acquisition-locks`, `update-output`, `update-preview`,
  `tui-error-recovery`, `tui-feedback`) in the same pull request as the tests
  that annotate every scenario. Verify
  `npx --yes @fission-ai/openspec@1.12.0 validate git-cooldown --strict` and
  `go run ./tools/specaudit trace` pass

## 1. Consumer schema and layering (internal/config)

- [ ] 1.1 Decode the `cooldown` mapping and per-source `cooldown.period`
  (design D1) with strict validation, including the bare-`0` rule. Red tests
  first: valid mapping in every layer, bare zero, invalid period, unknown
  mode, directory source rejection, per-source key restriction. Annotate the
  six `consumer-config-format/*` scenarios
- [ ] 1.2 Compute the effective floor per source and per shared lock identity
  (design D2), clamp with `cooldown.relaxation-ignored`, resolve
  `initial_acquisition`, and expose the values on the effective configuration.
  Annotate the six `config-layering/*` scenarios
- [ ] 1.3 Update `site/docs/reference/configuration.md` and
  `configuration-layers.md` (field table, layer permissions, floor rule,
  shared identities, diagnostics, `0s` spelling)

## 2. History reads (internal/cache)

- [ ] 2.1 Add a first-parent chain reader over the exact object store that
  parses `rev-list --first-parent --format` output including its header
  lines, returns commit, committer date, and parents, reads the tagger date of
  an annotated `refs/agent-marketplace/resolved`, and provides `IsAncestor`
  and "still reachable from any ref" helpers that translate missing-object
  failures (exit 128) into a not-an-ancestor / unreachable result while
  leaving transport failures intact. Fixture repositories: fresh tip,
  backdated tip, dormant branch with a backdated push, local-commit-pushed-late,
  side-branch merge then fast-forward with a recent side commit, rewritten
  branch, unreachable locked commit, annotated tag on an old commit
- [ ] 2.2 Add effective-commit-date computation over the whole first-parent
  chain with anchor restriction and seed (design D3 steps 3–6) as a pure
  function with table tests; add a benchmark over a long synthetic
  first-parent history
- [ ] 2.3 Add tag effective dates from the mutable ref cache (tagger date and
  merge-base-restricted candidates) for `--bump` (design D6)

## 3. Candidate selection and rewrite handling (cmd/agent-marketplace, internal/pipeline)

- [ ] 3.1 Replace tip resolution in `acquireLockedRegistry` with candidate
  selection; keep `P == 0` byte-for-byte identical to today including a
  rewritten tip. Annotate
  `deployed-acquisition-locks/fresh-tip-is-held-and-the-lock-is-retained`,
  `older-eligible-descendant-is-selected-instead-of-the-tip`,
  `backdated-tip-is-measured-by-its-parent-and-held`,
  `backdated-push-onto-a-dormant-branch-is-accepted`,
  `annotated-tag-ref-uses-the-later-of-tagger-and-commit-dates`,
  `full-commit-ref-is-exempt`,
  `zero-period-resolves-the-tip-without-a-rewrite-check`
- [ ] 3.2 Initial acquisition selection, `acknowledge` fallback warning, and
  `strict` refusal (design D5). Annotate the three initial-acquisition
  scenarios
- [ ] 3.3 History-rewrite detection with the evidence table, refusal
  `lock.history-rewritten` before any write, `--accept-history-rewrite` on
  `update`, `lock --upgrade`, and `update --dry-run` with optional
  `--registry`/`--ref` targeting, whole-chain selection after acceptance, and
  the held-rewrite outcome that keeps the existing lock. Annotate the six
  history-rewrite scenarios
- [ ] 3.4 `--bump` eligibility filter and held-tag reporting. Annotate the two
  tag scenarios
- [ ] 3.5 Hold lines, rewrite report, and diff exclusion in `UpdateResult`
  and `writeUpdateOutput`; preview parity in `update --dry-run`, including
  the accepted-rewrite preview with `--include-diff`. Annotate the three
  `update-output/*` and three `update-preview/*` scenarios
- [ ] 3.6 Extend `TestPublicCommandPathsHaveBehaviorTests` witnesses for the
  new flag on `update` and `lock`

## 4. TUI

- [ ] 4.1 Show holds, including held accepted rewrites, in the update result
  as info diagnostics (design D8)
- [ ] 4.2 Add the history-rewrite recovery action and confirm modal with
  accept-all and per-row acceptance, re-running the update with the accepted
  identities; cancel changes nothing
- [ ] 4.3 Add `tools/tui-qa/tests/update-cooldown-hold.spec.ts` and
  `tools/tui-qa/tests/update-history-rewrite-accept.spec.ts`, list them in
  `tools/tui-qa/README.md`, confirm each fails without the change, and
  annotate `tui-feedback/held-source-appears-in-the-update-result-with-its-eligible-date`,
  `tui-error-recovery/recovery-action-shows-evidence-and-accepting-advances-the-lock`,
  `tui-error-recovery/cancelling-the-acceptance-modal-changes-nothing`

## 5. E2E and documentation

- [ ] 5.1 Add hold, initial-acquisition, rewrite, held-accepted-rewrite, and
  shared-identity scenarios to the declarative E2E table with spec tags; run
  `make validate-e2e`
- [ ] 5.2 Extend the released-client gate: lock bytes and
  `sync --locked`/`sync --check` unchanged after a held `update`, and the
  pinned stable client refusing a shared configuration that declares
  `cooldown` with `v4.unknown-field`
- [ ] 5.3 Document holds, rewrites, acceptance, the dormant-branch limit, the
  client upgrade order, and the recommended 24-hour organization floor in
  `site/docs/reference/lockfiles.md` and a how-to under `site/docs/how-to/`

## 6. Validate and archive

- [ ] 6.1 Run `make validate`, `make spec-check`, `make tui-qa`, and
  `make validate-e2e`
- [ ] 6.2 `openspec archive git-cooldown --yes` in the same pull request
