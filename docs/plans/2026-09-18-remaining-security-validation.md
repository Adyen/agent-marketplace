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
- [ ] **R1b: Discovery and exact snapshots.** Verify whether ad-hoc install can
  select mutable-cache content and then lock different content in one operation.
  If reproduced, bind discovery and mutation to the same exact revision while
  preserving local registries, reusable locks, and transactional rollback.
- [ ] **R1c: Marketplace preview/consent identity.** Trace prepared-plan commit
  and collision checks. Distinguish an interactive preview-to-commit mismatch
  from explicitly accepting the currently resolved revision in a new CLI run.
  Reuse digest-bound prepared plans; require a product decision before changing
  the meaning of `--accept-risk` or adding persistent consent state.
- [ ] **R1d: Removal-time registry policy.** Determine whether the allowlist
  exception used to remove a blocked registry also permits unrelated blocked
  resources to acquire or materialize. If so, narrow the exception without
  making blocked entries impossible to remove or altering required policy.
- [ ] **R1e: Web URLs with slash-containing refs.** Check GitHub/GitLab URL
  interpretation and explicit `--ref`/`--path` behavior. Reject genuinely
  ambiguous input or use existing explicit selectors; do not introduce
  speculative network resolution or silently change simple-ref compatibility.

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
- All other unchecked items remain open.
