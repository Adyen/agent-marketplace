# Remaining security-boundary validation and fix plan

## Baseline and evidence correction

Implementation starts from `origin/main` at `96e030e`. The first hardening
wave, including bounded config reads and preventive review guidance, is now
merged (PRs 189–197). New sensitive aggregates default to `0600`, not read-only.

The initial draft overstated the completeness of validation. Its temporary
probes ran on `review/security-tui-selection-identity` at `c77bf6f`, not current
main. Direct calls to skill-link and owned-aggregate helpers demonstrated
last-target/first-entry handling of duplicate inputs, but did not demonstrate
that production resolution supplies those duplicates or loses its winner.
Those observations are not an end-to-end policy-bypass reproduction.

This document is an implementation backlog, not a completed vulnerability
ledger. Every item below requires a current-code disposition. A suspicion is
not a confirmed defect, and unsuccessful probes are not proof of safety.

## Delivery workflow

Work through R1, R2, R3, R4, and R5, one logical layer at a time. Maintain short
stacks by issue family rather than one stack containing all work.

For each item:

1. Trace the actual consumer and upstream protections on the recorded base.
2. Establish the intended contract from project guidance and existing tests.
3. Add a bounded regression with a positive control; fail on fixture/setup
   errors rather than treating them as evidence against the claim.
4. If a defect is confirmed, implement the smallest shared-boundary fix and
   verify it. Otherwise record why no code change is justified.
5. Review and commit the layer, then record its tests and disposition here.

Completion means each item has a verified fix, an evidence-backed no-change
outcome, or an explicitly identified policy decision/blocker. Do not silently
mark unresolved items complete. Do not execute resource commands, contact real
credential-bearing services, or build attack demonstrations during testing.

## Stack R1 — Selection integrity

- [x] **R1a: Collision policy through materialization.** Trace resolver output
  into native skill links and owned MCP aggregates. Test both registry orders,
  required/source precedence, same-registry disjoint paths, and separate
  machine/repository scopes. Preserve documented soft collision warnings and
  additive hooks; do not turn all name collisions into hard failures merely
  because a low-level helper accepts duplicate inputs.
- [x] **R1b: Discovery and exact snapshots.** Verify whether ad-hoc install can
  select mutable-cache content and then lock different content in one operation.
  If reproduced, bind discovery and mutation to the same exact revision while
  preserving local registries, reusable locks, and transactional rollback.
  - [x] Bind remote ad-hoc CLI discovery and publication to prepared exact pins.
  - [x] Validate and bind configured CLI install discovery, including missing-lock
    bootstrap, scope copies, and concurrent changes to existing exact inputs.
  - Live local registries and the explicitly unlocked organization's own outputs
    retain their existing mutable contracts; no universal snapshot guarantee is claimed.
- [x] **R1c: Marketplace preview/consent identity.** Trace prepared-plan commit
  and collision checks. Distinguish an interactive preview-to-commit mismatch
  from explicitly accepting the currently resolved revision in a new CLI run.
  Reuse digest-bound prepared plans; require a product decision before changing
  the meaning of `--accept-risk` or adding persistent consent state.
- [ ] **R1d: Removal-time registry policy.** Determine whether the allowlist
  exception used to remove a blocked registry also permits unrelated blocked
  resources to acquire or materialize. If so, narrow the exception without
  making blocked entries impossible to remove or altering required policy.
  - [x] Prevent new acquisition when tolerating blocked-registry errors; require
    existing cached input rather than silently omitting missing registries.
  - [x] Add a proof-backed single-entry cleanup operation for existing owned
    JSON/TOML adapters, without replacing unrelated entries or their ownership.
  - [x] Integrate named blocked-registry MCP removal with exact inputs and
    owned Devin/Codex aggregates into the config/lock publication scope.
  - [x] Extend named blocked-registry removal to complete managed-skill symlink
    outputs, with exact-target and descriptor-bound deletion safeguards.
  - [x] Extend the same source-bound link cleanup to eligible command and
    subagent outputs, retaining copied/shared/generic-cache output refusal.
  - [ ] Extend targeted removal to the remaining command paths and artifact
    types, including whole registries, collections, hooks, regular files/trees,
    shared component-cache link provenance, and shared-metadata cleanup.
- [ ] **R1e: Web URLs with slash-containing refs.** Check GitHub/GitLab URL
  interpretation and explicit `--ref`/`--path` behavior. Reject genuinely
  ambiguous input or use existing explicit selectors; do not introduce
  speculative network resolution or silently change simple-ref compatibility.
  - [x] Explicit slash-containing refs disambiguate the parsed directory.
  - [ ] Decide handling of intrinsically ambiguous URLs without explicit refs.

## Stack R2 — Repository filesystem boundaries

These are suspected scope-boundary defects, not a blanket symlink ban.

- [ ] **R2a: Config editors and migration.** Follow repo/local editor, removal,
  and migrate entry points through transaction staging and publication. Prove
  whether a project-controlled path can mutate bytes outside its authorized
  scope. Preserve supported config symlinks to valid targets and user scope.
- [ ] **R2b: Harness output roots.** Trace destination adapters and validation
  before canonicalization. Test existing root and ancestor symlinks for every
  affected target; distinguish authorized symlinked `HOME`/checkouts from escapes
  within a repository. Explicit conversion destinations have their own contract.
- [ ] **R2c: Aggregate leaves and support files.** Check whether foreign bytes
  can be read into output or written through links. Cover source acquisition,
  source containment, leaf links, and recursive support-copy paths before
  choosing the appropriate guard. Do not reject legitimate internal support
  links without establishing their existing contract.
- [ ] **R2d: Aggregate/manifest reads and deinit.** Audit regular-file checks,
  read bounds, ownership recovery, and repo-scoped deinit/portable manifests.
  Reuse existing limits where applicable; new limits need an explicit policy.
  - [x] Make shared dependency-transaction reads reject FIFOs without blocking
    during open, including final config-read assertions and journal recovery.
  - [ ] Complete remaining aggregate/manifest and deinit read-boundary validation.

## Stack R3 — Credential and terminal-output handling

- [ ] **R3a: Git authentication header scope.** Trace URL-scoped and unscoped
  extra headers through Git configuration and allowlist checks. Use dummy
  headers and local fixtures only. Preserve intentionally configured global
  authentication unless an unintended cross-origin transfer is established.
- [ ] **R3b: Persisted remote credentials.** Verify what acquisition writes to
  cached `.git/config`. Remove unintended persistence without breaking later
  authenticated fetches or changing the meaning of repository URLs.
- [ ] **R3c: Credential redaction.** Cover default-branch lookup, malformed URLs,
  blocked-registry errors, query parameters, and userinfo. Centralize redaction
  at shared error/log boundaries; do not inspect real credentials.
- [ ] **R3d: Terminal output.** Trace control bytes through names, refs, list,
  status, warnings, and telemetry failure summaries. Reuse existing quoting at
  human-readable sinks without corrupting machine-readable JSON output.

## Stack R4 — Conversion fidelity and restricted authority

Each item requires a native-semantics check and a real ingest/compile path.
Warnings about an intentional unsupported conversion are not automatically
security defects; restrictions must not silently become broader authority.

- [ ] **R4a: Explicit tool restrictions.** Verify Factory behavior for partially
  or wholly unmappable allowlists, and Codex command `allowed-tools` handling.
  Suppress unsafe conversion with a stable diagnostic when an explicit
  restriction cannot be represented.
- [ ] **R4b: Hook lifecycle mapping.** Verify `SubagentStop` to Windsurf response
  hooks and suppress mappings that cannot preserve the original scope.
- [ ] **R4c: Target execution-control extras.** Check cross-harness workflow
  `auto_execution_mode` and other native controls preserved as opaque metadata.
  Preserve legitimate native round trips while preventing metadata from gaining
  authority merely by switching targets.
- [ ] **R4d: Literal versus interpolated values.** Verify Codex header literals
  and Windsurf/Gemini token conversion without substituting real environment
  values. Keep literal strings distinct from declared references.
- [ ] **R4e: Project versus plugin roots.** Determine where Gemini project and
  extension tokens are collapsed, then preserve distinct meanings through
  ingestion, canonical IR, compilation, and native installation.
- [ ] **R4f: Other restriction/lifecycle losses.** Check Windsurf write/edit
  coverage, Devin restricted skills reclassified as commands, and any missing-
  file detection based on error prose instead of typed errors.
- [ ] **R4g: Managed markers.** Check marker recognition against each target's
  format and ownership model. Do not replace durable ownership with markers or
  adopt handwritten artifacts based on body substrings.
- [ ] **R4h: Compatibility regressions.** Add independently authored cross-
  harness expectations alongside each confirmed mapping/token fix, including
  valid native round trips and explicitly unsupported conversions.

## Stack R5 — Bounded processing and algorithmic work

These claims remain candidates until reachability and cost are measured.

- [ ] **R5a: Read boundaries.** Audit YAML frontmatter, JSON/JSONC, support files,
  aggregate inputs, and Marketplace collection/blob reads. Enforce applicable
  existing limits consistently; define new budgets before implementation.
- [ ] **R5b: Algorithmic complexity.** Measure duplicate-key path construction,
  hook equality/merge work, parser depth/member counts, wildcard filters, and
  collection DAG expansion. Optimize only established costs while preserving
  branch-local cycle detection, ordering, and diagnostics.
- [ ] **R5c: Acquisition and search.** Check clone/tag work, federated search
  buffering before the global limit, page serialization/cache limits, revision
  listing, and manifest ownership loops. Bound app-level amplification even
  when deployment rate limits exist; operator-facing ceilings need a documented
  policy and compatibility controls.

## Store/worker and policy-dependent follow-up

- [ ] Trace redirect policy during acquisition.
- [ ] Verify stale-assessment publication and compare-and-set behavior.
- [ ] Review lease renewal/override semantics and telemetry authority rules.
- [ ] Reassess image decoding/resource limits and API error exposure against
  actual trust boundaries; authenticated publishers and upstream rate limiting
  alone do not establish that an application-level resource risk is harmless.

## Prior controls and exclusions to preserve

- Standard `PATH` lookup, supported SSH environment configuration, and an explicit
  local log-path override are not independently proven privilege boundaries.
  Change them only when a lower-trust input crosses an actual authority boundary.
- Current additive hook behavior and incomplete-compilation pruning suppression
  are documented safety contracts, not bugs to remove.
- Earlier nested-bundle and required-hook controls were narrow examples on an
  older branch, not exhaustive proofs covering every layout or resource kind.
- Known-field projection in one MCP translator does not by itself establish the
  safety of all extension, alias, and nested-MCP paths.

## Verification and tracking

Use regressions through real consumers with valid controls, stable diagnostic
codes or typed errors, unchanged external bytes, and no staging residue where
applicable. Isolate `HOME`/`XDG_*`/organization config and potentially blocking tests.
Measure changed hot paths before and after per `CODING_STANDARDS.md`; do not
weaken safety to obtain faster benchmarks. Native harness and real-terminal TUI
checks remain CI gates; never bypass pinned tooling or repository policy.

### Progress

- Plan corrected and rebased to the merged hardening baseline `96e030e`.
- R1a confirmed through `resolver.Resolve` into `materialize.Run`, not by
  injecting duplicates into a leaf helper. Same-URL disjoint paths retained for
  visibility could install the wrong skill or Devin MCP value; Gemini and Codex
  could reject the duplicated desired entries instead of installing the winner.
- R1a fixed by a resolver-owned materialization projection using the existing
  precedence comparator per kind/name, target, and machine/project scope.
  Visibility and soft warnings remain unchanged; additive hooks remain additive.
- R1a verification: full resolver/materializer package tests passed, then final
  projection/consumer regressions and `make ci-lint LINT_BASE=origin/main` passed.
  Tests cover both source orders, required/source/file precedence, config-order
  ties, target overlap, separate scopes, nil resources, and unresolved metadata.
- R1a benchmark (101 inputs, including existing filesystem inspection):
  190.4 to 208.3 microseconds/op (+9.44%), 194.4 to 242.4 KiB/op (+24.72%),
  and 938 to 942 allocations/op. The added policy check has a measured cost.
- R1c: existing prepare/commit flow already retains the approved Marketplace
  plan and exact pins. A new CLI invocation with `--accept-risk` accepts the
  currently resolved revision; no persistent consent redesign was made.
- R1c: collision checks now use read-only exact Git inventories when locks are
  present, instead of the mutable cache. Only absent locks may use the existing
  warm-cache fallback. Malformed locks and unavailable snapshots fail closed.
  Regression tests demonstrate both a missed locked collision and a false
  mutable-cache collision before the fix, and assert no config/lock writes or
  Git clone/pull operations. Existing Marketplace pins may still refresh over
  HTTP during preflight. Focused pipeline tests, Marketplace E2E, and lint pass.
- R1c warm-registry benchmark: 855.2 to 867.6 microseconds/op (no significant
  timing difference), zero refreshes/op, 310.6 to 314.3 KiB/op and 2988 to 3031
  allocations/op. This does not measure a cold exact-snapshot acquisition.
- R1b remote ad-hoc slice, based on merged `a371a5d`: a real CLI regression
  confirmed that `install new --from ...` reported success for a resource only
  in mutable commit B while the owning lock still contained commit A.
- Remote `--from` installs now prepare exact lock documents and the effective
  discovery config without publishing dependency files. Discovery consumes that
  exact view; final reconciliation reuses the prepared documents offline and
  validates frozen config/include/absent-layer/lock generations before publication.
  The exact-cache lease spans preparation, selection, publication, and sync.
- Regressions cover warm-lock refusal and valid installation, cold named/bundle/
  inferred/user installs with a ref move after discovery, changed included config
  before reconciliation and immediately before commit, newly created local config,
  changed locks and final requirements, and missing-selection preservation with
  released-plan refusal. GC cannot cross the prepared lease and can run after a
  failed selection. A built-binary E2E case verifies the warm locked consumer.
  Install-focused command/pipeline tests, race-enabled ad-hoc regressions, focused
  E2E, full `make test`, build, and CI lint passed before publication.
- Adversarial review checked pin reuse, generation checks, pre-publication errors,
  cache-lease ownership, scope boundaries, and the actual consumer/test paths.
  Discovery also uses the prepared effective config rather than an earlier load.
  An alleged extra lease acquisition was rejected after checking the actual code;
  the acquired lease is transferred once and released by the plan. No remaining
  blocker was identified within the remote ad-hoc contract.
- Repeated warm remote `--from` install benchmark (local Git/disk included, no
  Internet): 166.5 to 146.9 ms/op (-11.80%), 1.910 to 2.684 MiB/op (+40.50%),
  and 13.25k to 18.86k allocations/op (+42.38%). Five samples per revision;
  benchstat reports p=0.008 but cannot estimate a 95% confidence interval with
  this sample count. This is not a cold-install or general-sync benchmark.
- At the ad-hoc checkpoint, configured-install discovery remained open. Local
  registries and explicitly unlocked organization output are intentionally mutable;
  neither slice promises immutable local input or global filesystem atomicity.
  A post-publication sync failure retains committed dependency state under the
  existing error contract.
- R1b configured slice, based on merged `99ba279`: consumer regressions reproduced
  revision drift after discovery both for missing-lock bootstrap and when copying
  a user registry into repo scope. Configured CLI installs now use the shared
  preparation path while preserving the existing refusal of invalid/stale locks.
- Scope copies bind the actual selected provider's prepared URL/ref/root/commit
  into the destination layer. Roots are projected from the verified manifest,
  not acquired again. Conflicting commits or manifests for one destination
  identity fail before publication rather than silently choosing another pin.
  Lock generations, including absent destination locks, are captured up front.
- Named, all, tag, collection, repo/user/local copy, and nested-bundle controls
  retain the discovered commit when the ref moves. Real lock upgrades, include
  changes before reconciliation and immediately before commit, new local config,
  and changed final requirements refuse without overwriting the foreign input.
  An E2E command copies a warm user pin into repo scope after the origin advances.
- Organization discovery may capture an ephemeral pin for a writable scope copy;
  its own unlocked materialization remains mutable and no organization lock is
  created. A GC control preserves installed organization output. Missing-selection
  and released-handle controls cover cleanup without dependency publication.
- Adversarial review checked copied-provider identity, scope isolation, root
  projection, incompatible pins, generation timing, lease lifetime, and refusal
  side effects. Benchmark verification exposed an absent nonstandard organization
  filename with unset source metadata; explicit layer roles now determine the
  adjacent lock path, with a failing-then-passing regression. No remaining blocker
  was identified within the CLI Git-backed binding contract. Focused install and
  E2E checks, race-enabled regressions, full Go tests, build, and CI lint passed.
- Repeated warm configured install benchmark, with local Git and disk I/O and no
  network: 136.4 to 202.8 ms/op (+48.69%), 2.288 to 3.361 MiB/op (+46.89%), and
  14.71k to 21.58k allocations/op (+46.67%). Five samples per revision; benchstat
  reports p=0.008 but cannot estimate a 95% confidence interval with this sample
  count. This measured validation overhead remains an optimization follow-up;
  no safety checks or processing budgets were weakened.
- R1e partial fix: an explicit `--ref release/v1` now removes the full matched
  ref from a GitHub/GitLab web URL before deriving the directory. Tree/blob,
  root, encoded slash, mismatching ref, component-boundary, and explicit-path
  controls pass. Legacy unqualified URL interpretation remains unchanged.
  URL benchmark: 242.8 to 266.1 ns/op, 240 to 264 B/op, 3 to 4 allocations/op.
- R1d decision approved: cleanup-only removal. Remove the requested selection,
  preserve unrelated installed resources, and neither fetch nor rewrite blocked
  content. Fail clearly before mutation if existing state cannot prove safe
  cleanup. Do not replace this with a blanket refusal while blocked entries
  remain.
- R1d acquisition guard implemented: when the removal exception actually
  suppresses registry-policy errors, dependency reconciliation must reuse
  existing exact state without acquisition. Catalog resolution uses the
  existing strict read-only `Inspect` mode; missing cache is an error, not an
  empty desired set. Marketplace resolution follows the existing offline mode.
  Ordinary allowed-policy removals retain their normal behavior.
- R1d regressions: the previous CLI acquired newly configured blocked content
  while removing another registry. It now refuses before publishing the config
  and lock in that case, with zero snapshot acquisitions and byte-identical
  existing output. Warm locked removal succeeds; missing exact snapshots fail
  closed; allowed-policy acquisition remains a positive control. Both default
  and custom pipeline loaders reject absent cache without calling clone/pull.
  Focused command/pipeline tests, lint, the full Go suite, and build pass.
- R1d normal warm-lock benchmark: 78.90 to 79.77 ms/op (+1.11%); memory and
  allocations have no statistically significant difference. This workload
  includes local disk and normal command processing, not Internet access.
- R1d backend foundation added on merged baseline `83ac043`:
  `RepairFile.RemoveOwned` removes one explicitly confirmed clean owned entry
  using the existing JSON/TOML adapters, root write session, mandatory backup,
  journal, and generation/digest CAS. It re-reads current file/proof and retains
  the durable metadata of every sibling, including drifted siblings.
- Pre-push adversarial review found a destination-binding defect in the first
  implementation: mutable preview paths could redirect a write inside the
  bound root while changing the original file's ownership record. The operation
  now checks its canonical root against the store binding and uses that root
  plus the validated relative path for publication. Retargeting regressions and
  a legitimate root-alias control cover the correction before first push.
- Backend tests cover keyed JSON/TOML preservation, exact retained spans and
  ordering, handwritten and drifted siblings, custom/private modes, absent or
  corrupt proof, present unmanaged targets, pending journals, confirmation,
  cancellation, foreign CAS mutation, and sequential/duplicate hooks. Codex raw
  hook digests have distinct controls: exact raw duplicates are handled without
  removing both, while reserialized changed values are refused.
- The desired-selection guard is relative to the prepared view, NOT a fresh
  configuration read. Future CLI integration must bind authorization to the
  applicable config/lock generations and refuse stale requests. Ownership
  proof alone does not authorize deleting a newly selected resource.
- Scoped clean 100-entry MCP comparison (disk/journal cost included; setup and
  preview excluded): full materialization deletion 62.26 ms/op versus targeted
  deletion 61.77 ms/op, with no significant timing difference. Memory is
  3.232 versus 1.346 MiB/op; allocations are 25528 versus 9005/op. This compares
  two different removal operations, not whole-CLI performance.
- R1d named-MCP CLI integration: `remove <name> --from <registry>` now uses
  targeted cleanup when registry policy is blocked and the selected resource is
  an MCP server. Kind inference uses the same path. Every selected target must
  have an existing owned-aggregate adapter; unsupported target mixtures fail
  before publication rather than narrowing targets or running broad sync.
- Preparation freezes config/include/absent-layer and adjacent-lock reads before
  opening the exact inventory, retains its cache lease, and verifies the selected
  identity again. Live local registries and unlocked input are unsupported in
  this narrowly exact-backed path. Staged config resolves against the same exact
  inventory without acquisition or Marketplace refresh.
- A dependency publication scope holds destination/read coordination locks and
  ownership root sessions through all-action preflight, config/lock publication,
  and targeted deletions. Post-publication checks require the NEW generation,
  not the old-or-new condition used during journal progress. Reverting to the
  original selected config after publication therefore refuses cleanup.
- Consumer regressions cover Devin repo and Codex user/repo output, explicit and
  inferred kind, last-selected MCP, scoped copies, unowned/corrupt/drifted input,
  pending journals, unsupported mixtures, missing exact snapshots, included-file
  changes, new local config, changed locks, and post-publication reselection.
  Refusals preserve bytes, modes, mtimes, ownership, backups, and staging state;
  coordination locks are separate. Codex repo mutations retain the trust-review
  diagnostic without modifying trust state. A declarative CLI E2E case covers
  edited and handwritten sibling preservation.
- Normal warm exact-lock sync benchmark: 74.00 to 79.92 ms/op (+8.00%); memory
  and allocation differences are not significant. This is the existing normal
  one-remote sync workload with local disk, not a cleanup-lifecycle measurement.
- Late I/O/CAS failures remain distinguishable as committed-scope errors; no
  rollback of published configuration is claimed. Aggregate journals remain
  per-file, not a global filesystem transaction. Whole-registry, collection,
  hook, ordinary-file, non-`--from`, and unblocked removal paths are unchanged
  by this slice and remain follow-up work. Existing adapter filesystem behavior
  remains a separate review surface; no general filesystem sandbox is claimed.
- R1d managed-skill link extension on merged baseline `508b257e`: named blocked
  `remove --from` now reuses the exact-backed publication scope for skills whose
  complete compiled output consists of managed symlinks. Factory, Cursor,
  Gemini, Windsurf, Devin, and Codex are covered; selection is shape-based, not
  a new target allowlist. Claude's shared plugin fragment, file mode, missing
  or changed links, and unsupported mixtures refuse before publication.
- The CLI regression first reproduced an unrelated installed sibling's edited
  cached `SKILL.md` being overwritten by broad sync. Targeted removal leaves
  compiled caches, siblings, handwritten content, manifests, and aggregates
  untouched. The original cache target survives; only the requested link is
  removed. All selected targets are preflighted, and remaining physical output
  paths protect fallbacks, cross-kind collisions, and aliased scopes.
- The link primitive binds the scope root, each directory ancestor, the link
  inode, and its exact compiled-cache target. Directory-descriptor operations
  move the entry into a private quarantine, verify it, then unlink only the
  matching symlink. A raced foreign replacement is restored without overwriting
  a new original-path entry; a restore conflict retains both replacements and
  reports the quarantine location. Cache targets are never traversed for deletion.
- Pre-push adversarial review caught missing scope-root comparison, descriptor
  cleanup, post-rename restoration, and tests that checked the wrong residue
  directory in the draft. These were corrected, including empty-quarantine
  cleanup guarded by its captured inode. Foreign or nonempty replacements are
  preserved rather than recursively deleted. No remaining blocker was identified
  within the declared named-skill symlink contract.
- Compatibility boundary: unresolved/already-unselected names in blocked cleanup
  now return `ErrOwnedCleanupUnsupported` instead of invoking broad sync. The
  old named-removal test had no enabling selector and conditionally checked
  installed output; its fixture now actually enables both skills and asserts
  the intended removal/preservation unconditionally. A separate unselected-name
  refusal verifies that edited siblings remain unchanged. Allowed-policy removal
  retains its previous behavior.
- Verification covers explicit/inferred kind, repo and user paths, supported
  multi-target success, unsupported mixtures, file-mode controls, missing/drifted
  links, root/parent/leaf substitutions, FIFOs, foreign descriptor refusal,
  before/after-move faults, restoration conflicts, and retained-selector aliases.
  Full-fixture CLI file/symlink snapshots include bytes, modes, mtimes, caches,
  ownership, backup and staging files, excluding coordination locks; primitive
  tests also inspect quarantine directories. Include/lock mutation and
  post-publication original-selection restoration refuse deletion. Focused
  built-binary E2E, race-enabled regressions, Go tests, build, and CI lint passed.
- Warm last-selected-skill removal benchmark (local Git and disk I/O included,
  no network; fixture setup and per-iteration config/link reset excluded):
  171.2 to 238.9 ms/op (+39.53%), 2.375 to 3.335 MiB/op (+40.42%), and 15.71k
  to 21.90k allocations/op (+39.40%), six samples per revision, p=0.002. This is
  a removal-lifecycle measurement, not a general sync/install benchmark; repeated
  exact validation remains an explicit optimization follow-up without relaxing
  generation or ownership checks.
- This link extension does not add a durable filesystem recovery journal or
  multi-file rollback. Late failures may follow committed dependency changes;
  process crashes may leave quarantine residue. It does not claim a general
  filesystem sandbox. Regular-file/tree cleanup, shared skill metadata, other
  kinds, whole registries, collections and non-`--from` paths remain open.
- R1d command/subagent extension on merged baseline `84d73c22`: the existing
  planner is now `PrepareManagedLinkRemoval`, with typed-kind agreement and
  per-kind copy policy. Pipeline eligibility for skills, commands, subagents,
  and MCP is centralized; MCP retains its separate aggregate planner. No changes
  to the filesystem deletion primitive, normal sync, or cache routing are made.
- Real Devin command and subagent regressions reproduced broad-sync rewrites of
  edited sibling cache contents before the fix. Complete source-bound command
  links are covered for Claude, Cursor, Gemini, Windsurf, Devin, and Codex;
  complete subagent links for Factory, Cursor, and Devin. Devin/Codex command
  user scope and Devin subagent user scope are covered, as is kind inference.
- Factory command copies, Claude subagent shared metadata, explicit file mode,
  and mixed unsupported targets refuse without publishing changes. Codex and
  Gemini subagents also refuse: their standalone artifacts use generic shared
  component-cache routing rather than this planner's source-specific link
  groups. They are not claimed to emit extra shared compiler fragments. Adding
  metadata solely to make the guard pass would change normal cache routing;
  source provenance for those generic links remains a separate follow-up.
- Full file/symlink fixture snapshots retain sibling bytes, modes, mtimes,
  cache contents and targets, unrelated configs, locks and state. Missing and
  retargeted file links refuse. An absolute external `CODEX_HOME` is explicitly
  covered by the Codex subagent refusal. Cross-kind tests refuse command cleanup
  when a skill still needs the same slot, and permit subagent cleanup while a
  same-named skill occupies a different slot. Config/include/lock changes and
  post-publication reselection tests cover all three link-resource kinds.
- Six-sample warm last-resource removal measurements with local Git/disk and no
  network, excluding fixture setup and per-iteration config/link reset:
  command 177.9 to 233.2 ms/op (+31.09%), 2.319 to 3.164 MiB/op (+36.45%),
  16.12k to 21.77k allocations/op (+35.06%); subagent 172.8 to 232.3 ms/op
  (+34.47%), 2.355 to 3.217 MiB/op (+36.62%), 16.14k to 21.76k allocations/op
  (+34.83%). All comparisons report p=0.002. These are Devin removal workloads,
  not a universal performance claim. The validation-cost follow-up stays open.
- Adversarial review of the resource-link extension checked route reachability,
  per-kind output shape/copy policy, remaining-slot protection, exact source
  binding, scope mapping, no cache writes, and regression validity. No remaining
  blocker was identified in the declared scope. Full Go tests, focused race
  regressions, the command/subagent/skill/MCP built-binary E2E cases, build and
  CI lint passed. Existing late-failure, quarantine, and unsupported-input
  limitations remain unchanged; no broader cleanup
  completion, v4 resource-editor support, or filesystem atomicity is claimed.
- R2 dependency read boundary on merged baseline `d13f9923`: the shared
  `dependencytxn.readRegularFile` opened a FIFO in blocking mode before it could
  check the descriptor's type. A bounded real CLI removal regression replaces
  an included config with a FIFO immediately before dependency commit; the old
  reader hangs in the final authorization path instead of refusing it. Direct
  snapshot/generation readers and journal recovery also hang. The separate
  mode-reading API already refuses a static FIFO through its earlier check.
- Exact old-code controls reached the CLI injection marker and timed out at the
  30-second subprocess bound; snapshot/generation/recovery children reached their
  10-second bounds. Restoring the fix makes these tests pass with typed unsafe-
  path/generation-conflict/recovery-blocked errors as applicable. The tests use
  no FIFO writer; they cannot accidentally unblock the faulty open.
- The fix adds `O_NONBLOCK` to the existing no-follow/close-on-exec open, validates
  the same opened descriptor and existing size limit, then restores blocking
  mode only for regular-file content reads. Existing 4 MiB bounds, growth checks,
  descriptor cleanup, symlink policy, and transaction semantics remain unchanged.
  The CLI test preserves the injected FIFO inode/type and all other fixture
  files/symlinks after staging cleanup. Direct reader refusals return no snapshot
  data and leave no new state entries.
- Review corrected two initial evidence gaps: a purported CLI test only called
  the reader directly, and a noncanonical recovery fixture could fail before
  reaching the journal. Current tests exercise actual commit/recovery consumers,
  require precise nested error identities, and fail on preservation mismatches
  rather than merely adding another error to an expected failure.
- Six-sample local-disk measurements (setup excluded, no network): 4 KiB snapshot
  read median 18.67 to 20.94 us/op, not statistically significant (p=0.065);
  unchanged one-file commit 79.80 to 84.50 us/op (+5.89%, p=0.041). Byte allocation
  and allocation counts are unchanged (10944 B/16 allocations for snapshot;
  14592 B/139 allocations for unchanged commit). Timing variability is substantial;
  these are narrow boundary measurements, not whole-command latency claims.
- Full Go tests, race-enabled transaction and CLI regressions, focused built-
  binary cleanup E2E, build and CI lint passed. Pre-push adversarial review found
  no remaining blocker within the FIFO-open contract. This is not a general I/O
  deadline, hardlink-policy change, inode/mode
  pairing redesign, repository-containment fix, or completion of R2d. Aggregate,
  manifest and deinit validation and the remaining R1 cleanup paths stay open.
- Combined checkpoint at `74e1564`: `make test` and `make build` passed. The
  planning and first three implementation layers are published as PRs 210–213
  in stack 214. Native compatibility and real-terminal TUI checks remain CI
  gates; publication does not mark the rest of the backlog complete.
- All other unchecked items remain open.
