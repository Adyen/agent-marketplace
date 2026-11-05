## Decisions

Use the existing selected-layer registration inventory and `errors.Join`.
Return one typed `lock.change-required` diagnostic per affected scope in
layer order, preserving each scope's lock path and endpoint list. The change
count counts affected scopes. Locked operations retain their first-refusal
behavior, and configuration, policy, lock-validation, and pending-recovery
errors retain their existing precedence.

The command regression covers absent and partially populated acquisition
locks in user, repository, and personal-local scopes, both together and
individually. Endpoint counters and complete fixture snapshots prove that
checks acquire nothing and write nothing; healthy locks are positive controls.

## Adoption guide disposition

The historical `docs/adoption-guide.md` is not retained as another source of
current usage instructions. Its surviving behavioral claims remain governed
by the existing capabilities and annotated tests:

- Exact-v4 roots, accepted includes, and unsupported-format refusal:
  `consumer-config-format` and `authored-configuration-reads`.
- Marketplace sources, selections, and allowlist authority:
  `config-layering` and `configuration-intent`.
- Selection, required resources, collisions, and collection ownership:
  `resource-selection`, `resource-installation`, and `config-mutation`.
- Layer-owned locks, exact snapshots, check/locked modes, and transactional
  edits: `deployed-acquisition-locks` and `acquisition-lock-codec`.
- Live directories, ad-hoc browsing, native output, ownership, backups,
  and status: `directory-source-discovery`, `ad-hoc-registry-browsing`,
  `materialization`, `owned-entry-reconciliation`, `backup-recovery`,
  and `status-reporting`.

The current repository adoption guide and public configuration, resource,
and harness references retain procedural advice; no public usage page is
removed. Logging and telemetry claims are covered by the companion baselines.

## Discarded claims

- v2/v3 consumer readers, legacy exclusions, implicit selections, the
  `migrate` command, startup migration prompts, structural remediation,
  conversion backups, and `config.legacy-deprecated` contradict the exact-v4
  launch contract and are discarded rather than converted into requirements.
- Claims that legacy consumer conversion always succeeds are discarded;
  the public pre-upgrade guide describes conditional conversion with an older
  released migrator instead.
- Minimum historical release versions and was/now migration instructions
  are history, not current compatibility requirements.
- Shell setup recipes, example repository addresses and resource names,
  manual Git commits, ignore-file recommendations, and lists of daily
  commands are tutorial examples, not additional normative behavior.
- Implementation package names and the internal sync stage checklist are
  explanatory architecture, not user-observable contracts.

## Validation

Run focused command regressions, the selected validation gate, strict spec
and traceability checks, and the combined E2E gate. Review the final diff for
consumer reachability, diagnostic identity, scope boundaries, and no-write
claims before publication.
