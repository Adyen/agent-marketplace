# Design: baseline-config-layering

## Context

Consumer Marketplace configuration — Marketplace endpoint registrations, Marketplace installations, and the `allowed_marketplaces` allowlist — is currently the *Consumer Marketplace configuration* requirement of `marketplace-api`. The `marketplace-protocols` → `marketplace-api` rebaseline corrected its content and left one scenario titled "Layered config wiring" describing layered-merge rules, so the title no longer matches the content. Consumer configuration belongs to a configuration-layering capability. This baseline creates `config-layering` with the relocated consumer Marketplace configuration requirements and absorbs harvest rows 1.1 and 4.14, deleting `docs/PRECEDENCE.md` and `docs/plans/2026-09-07-unified-resource-config-design.md`.

## Goals / Non-Goals

**Goals:**
- Add `config-layering` describing how consumer Marketplace configuration is expressed in v4 and in v2/v3 roots, validated per entry, and merged across the organization, user-personal, repository-committed, and personal-local layers, with every scenario proven by an annotated passing test.
- Remove the `marketplace-api` requirement *Consumer Marketplace configuration* with a migration note pointing at `config-layering`, and drop the capability Purpose's consumer-configuration phrase.
- Absorb harvest rows 1.1 and 4.14: delete the two legacy documents, check off the rows, repoint every remaining reference, and record every non-carried claim under **Discarded claims**.
- Zero behavior changes: no runtime code or test-logic edits; test files change in comment lines only.

**Non-Goals:**
- Baselining any config-layering content beyond consumer Marketplace configuration. Include syntax, include-tree validation and trust, layer files and locations, `allowed_registries` first authority, target-harness precedence, and migration consent stay for later baseline changes and are recorded under **Discarded claims**.
- Carrying acquisition-lock or migration claims: the exact-pin lock wire shape and consent-based migration are owned by the acquisition-lock-codec and config-migration baselines. The provisional-instance validation rule is consumer-configuration validation and is carried here; only the lock record shape that carries a provisional instance stays with acquisition-lock-codec.
- Touching any other `marketplace-api` requirement.

## Decisions

### D1. Relocate by REMOVED + ADDED

OpenSpec cannot move a requirement between capabilities, so the delta REMOVES the `marketplace-api` requirement and ADDs the relocated requirements under `config-layering`. Alternative — MODIFIED requirements in place — rejected: the content does not belong in a protocol capability, and the taxonomy names `config-layering` as the owner.

### D2. Scenario carry and the retitle

The three proven scenario behaviors carry across with the same annotated tests; test files change in comment lines only. The two validation scenarios keep their titles and WHEN/THEN verbatim. *Layered config wiring* is retitled to **Layered merge retains per-layer provenance and first-set allowlist**: the scenario states merge outcomes, not wiring.

Nine scenarios are added for observable consumer-configuration behaviors already proven by existing passing tests that were never bound to a consumer-configuration scenario (the prior baseline recorded them as claims that "enter a spec when their owning consumer-configuration scenarios are baselined"). Each is proven by adding one `// spec:` annotation line; no new test logic.

Schema coexistence wording: the expression scenarios state the v4 and v2/v3 spellings as current behavior of roots that declare those `apiVersion` values. They carry no deprecation narrative or migration direction, which the spec rules forbid; both spellings load today through their own readers. The prior rebaseline removed version wording from a requirement that claimed a canonical schema had been superseded; this change names the declared `apiVersion` of the root as the observable input that selects the spelling.

### D3. Purpose edit

A delta cannot modify an existing capability's Purpose; the pinned CLI's artifact instructions state that changing an existing capability's Purpose means editing the live spec file (`openspec/specs/marketplace-api/spec.md`) directly. That direct edit drops the "consumer Marketplace configuration" phrase from the Purpose list, lands with the annotation moves, and is verified by `openspec validate --all --strict` and `make spec-check`.

### D4. Annotation slug map

Every annotation moves to `config-layering/<slug>`; the slug is the scenario title lowercased with each run of non-alphanumeric characters replaced by one hyphen.

| Old slug | New slug |
|---|---|
| `marketplace-api/marketplace-entries-are-validated` | `config-layering/marketplace-entries-are-validated` |
| `marketplace-api/marketplace-url-allowlist` | `config-layering/marketplace-url-allowlist` |
| `marketplace-api/layered-config-wiring` | `config-layering/layered-merge-retains-per-layer-provenance-and-first-set-allowlist` |

New annotations go on existing passing tests:

| New slug | Test |
|---|---|
| `v4-root-rejects-unsafe-marketplace-source-urls` | `TestV4MarketplaceURLUsesCanonicalEndpointSafety` |
| `v4-root-rejects-legacy-installation-keys` | `TestV4RootRejectsLegacyInstallationKeys`, `TestV4RejectsStrictInvalidInputs` |
| `v2-and-v3-roots-declare-marketplace-entries` | `TestMarketplaceConfigRoundTrip`, `TestV3MarketplaceRejectsDevelopmentAliasesEvenWithOtherConfigErrors` |
| `duplicate-and-reserved-marketplace-aliases-are-rejected` | `TestValidateMarketplaceEntriesRejectsDuplicateAndReservedAliases` |
| `layered-alias-accepted-for-one-endpoint-identity` | `TestValidateMarketplaceEntriesAllowsLayeredAliasForSameIdentity` |
| `provisional-instance-entry-defers-to-a-verified-layered-instance` | `TestValidateMarketplaceEntriesTreatsAProvisionalInstanceAsUnknown` |
| `unsafe-identity-and-endpoint-fields-are-rejected` | `TestValidateMarketplaceEntriesRejectsUnsafeIdentityAndEndpointFields` |
| `personal-local-duplicate-aliases-are-rejected-at-load` | `TestLocalMarketplaceValidationCoversAllEntries` |
| `allowed-marketplace-patterns-follow-include-authority-in-a-v4-root` | `TestV4IncludeMergesAuxiliaryPolicyByField` |

Binding notes: the legacy-installation-key scenario names the key family, and both bound tests exercise the same `legacy installation key` rejection rule for family members (`skills`, `registries`); no test exercises the `marketplaces` spelling specifically. The v2/v3 expression scenario binds the v2 round-trip test for retention and the v2/v3-reader test for pin rejection; no test declares `agent-marketplace/v3` together with `marketplaces` entries, so the v3 spelling is proven through the single shared v2/v3 reader that accepts both `apiVersion` sentinels identically — a v3-declared load test could strengthen the binding later without any behavior change.

### D5. Harvest absorption and link repointing

Rows 1.1 and 4.14 are checked off in `openspec/changes/harvest-legacy-docs/tasks.md`, and both documents are deleted. No surviving file outside `openspec/changes/` may link to either deleted document; the harvest ledger's checked rows and this change's own artifacts name the paths as backticked mentions, not links, and stay. The live references are:

- `README.md` (four links) — drop the link sentences or reword so they no longer point at the deleted reference.
- `AGENTS.md` — the Core Concept sentence "See [the precedence reference](./docs/PRECEDENCE.md) for include syntax, validation, migration consent, and merge order." and the File Layout row for `PRECEDENCE.md` are removed.
- `docs/adoption-guide.md` — the sentence pointing at the precedence reference for detailed precedence rules is removed or reworded.
- `docs/tui-product-contract.md` — the opening paragraph's "docs/PRECEDENCE.md" mention is dropped from the governing-sources list.
- `docs/plans/2026-09-24-pre-v4-regression-gates.md` — the precedence-reference link is dropped from the follow-list sentence.

No live file references `docs/plans/2026-09-07-unified-resource-config-design.md` outside the harvest ledger. Code and tests remain the authority for every later baseline that would have used the deleted documents as leads.

### D6. Archive mechanics

`openspec archive baseline-config-layering --yes` applies the removal and the new capability. `marketplace-api` survives with its protocol-surface requirements, so no `retire_capabilities` marker and no empty-spec cleanup is needed. The pinned OpenSpec 1.12.0 archive has a known safeguard (blackboard entry `1855191b-8e6b-46ef-9707-c9404a87fbb0`): archive refuses when the live spec holds content the merge cannot account for, including wrapped continuation lines inside scenario bullets of removed requirements. If archive refuses on the removed requirement's wrapped bullets, normalize the live requirement's wrapping (whitespace only, content unchanged) and rerun archive; every such step is followed by `openspec validate --all --strict` and `make spec-check`. That note is possibly stale; verify the behavior against this checkout before relying on it.

## Risks / Trade-offs

- [A carried scenario is weakened during the move] → the two validation scenarios carry WHEN/THEN verbatim; acceptance review compares each new scenario against the removed requirement verbatim; the slug map fixes the 1:1 carry.
- [A scenario title and its annotation slug drift apart] → `specaudit trace` fails on both dangling annotations and unproven scenarios; `make spec-check` runs before handoff and in CI.
- [Deleting the two documents removes leads later baselines expected] → code and tests remain the authority, and **Discarded claims** records every non-carried claim with the capability that owns it.
- [The archive leaves the requirement behind or flags unaccounted lines] → follow D6; verify with `openspec validate --all --strict` and `make spec-check` before handoff.
- [The live marketplace-api Purpose still claims consumer configuration] → the D3 direct edit lands with the annotation moves; acceptance review greps the live spec after archive.

## Migration Plan

One pull request: this change (proposal, deltas, design, tasks), then the annotation moves, the Purpose edit, the ledger check-offs, the document deletions, and the link repointing, then `openspec archive baseline-config-layering --yes` as the final commit. Rollback is a revert of the pull request; no runtime behavior changes at any step.

## Discarded claims

Every claim from the two absorbed documents that is not carried into `config-layering`, with its reason. "Owned by `<capability>`" means current code implements it (or it is planned) and the named capability's own baseline derives it from code, which is the authority; it is deliberately not speced here.

### `docs/PRECEDENCE.md`

- Audience and framing prose (written for contributors; the same rules apply to all five written kinds; collections are never written into harness folders): reader guidance, not observable consumer-configuration behavior.
- The **Schema requirement** section: v4 as the canonical writable schema, v2/v3 as deprecated compatibility schemas read through the legacy reader, the per-document-kind `apiVersion` rules (root must declare its version, fragments may omit it, legacy `config.local.yaml` may omit it), and the `init`-creates-v4 rule: owned by a later `config-layering` baseline addition (schema boundaries) and the `config-migration` baseline; only the v4-legacy-key rejection and the v2/v3 marketplace expression are carried here.
- **Migration consent** (interactive startup and TUI ask before migrating the user-personal root; repository and personal-local roots never offered startup migration; declined or non-interactive cases continue through the v2/v3 reader): owned by `config-migration`.
- **Config includes** (path resolution relative to the declaring file, recursion, glob and remote-URL rejection, legacy include deduplication by canonical path, v4 branch-local ancestry, cycle failure, editing a layer that has includes, the legacy registry `path` alias-versus-subdirectory interpretation): owned by later `config-layering` baseline additions; the only include behavior carried here is the `allowed_marketplaces` include authority.
- **Organization include trust** (root-owned regular files, no group/other write, trusted ancestor directories, `organization.trust.*` diagnostics): owned by a later `config-layering` baseline addition.
- **Merge order within one layer** (fragments merge below their containing file; later siblings win; field-specific rules; `exclude` beats `include` within one file; `exclude_all` stickiness; top-level selector conflict resolution): owned by later `config-layering` baseline additions and `resource-selection`; only the `allowed_marketplaces` earliest-declaration rule is carried.
- **Explicit v4 migration safety** (scope validation, adjacent private backups, journaled publication of backup, v4 replacement, and lock together, `--dry-run`, the refusal list, `init` creating v4): owned by `config-migration` and `acquisition-lock-codec`.
- **Reading v4 roots** (explicit source kinds, directory-source resolution and liveness, Marketplace sources resolving through the owning layer's lock, intentionally absent organization lock behavior, online transient evidence, `v4.marketplace-lock-required` and `v4.marketplace-lock-mismatch` fail-closed): owned by `acquisition-lock-codec`; only Marketplace source URL safety is carried here.
- **Editing v4 roots** (all selection editors on v4 roots, `config.edit.include-owned`, `v4.writer-opt-in`, the TUI harness-edit and user-override exceptions, staged-edit overlays): owned by `config-mutation`.
- **Marketplace exclusions** (an exclusion-only group acquires nothing and needs no lock; exclusions take precedence over includes; required groups immune): owned by `resource-selection` and `acquisition-lock-codec`.
- **V4 selection decision order** (include-tree order, dynamic `all`/tag candidates, exclusion suppression, re-enablement by later includes, `required`-only groups selecting zero, empty `tags` and `harnesses` semantics): owned by `resource-selection`.
- **Layer order** (the four layers, their file locations, and higher-layers-override): carried only as the merge scenario's observable outcome; the layer file locations and their management notes are owned by a later `config-layering` baseline addition.
- **Layer locks** (per-layer lock files, lock composition not changing selection precedence, lock v2 as the only write format, v1 upgrade behavior, `lock.version-deprecated`, exact Marketplace records in the lock): owned by `acquisition-lock-codec`.
- **Target harnesses** (project-first precedence `RepoCommitted` → `UserPersonal` → `Organization`, absent-or-empty lists not setting the target set, per-entry narrowing): owned by a later `config-layering` baseline addition.
- **Registry allowlist** (`allowed_registries` first-authority within and across layers, `host/path` matching form without scheme, `git@` prefix, trailing slash, or `.git` suffix, empty-versus-omitted semantics, `config.local.yaml` cannot declare it): owned by `resource-selection`; the analogous `allowed_marketplaces` first-set rule is carried here. No test proves the `config.local.yaml` restriction for `allowed_marketplaces` itself, so that specific restriction is not carried.
- **Registry-level selection rules**, **ExcludeAll**, **Exclude precedence**, **Removal scope** (scope selection, authority boundaries, `--from` semantics, collection removal), **Collection precedence** (expansion, pinned paths and refs, binding eligibility, nested definitions), **Resource prerequisites**, **Name collisions and ambiguity** (winner ordering, hook additivity, overlapping paths, remediation), and **Practical examples**: owned by `resource-selection` (and `config-mutation` for removal edits).

### `docs/plans/2026-09-07-unified-resource-config-design.md`

- Status, prerequisite, and audience preamble; **Implementation status** (implemented and remaining lists); **Integration boundaries and release conditions**: planning history, not behavior; no claim carried.
- §1 decisions table: carried only for the Marketplace rows (one named `sources` map including Marketplace endpoints; a Marketplace endpoint registration adds a source declaration only and selects no resource — carried in substance by the expression requirement); source-bound resource groups, advanced selection mapping, per-resource options, dynamic-selector registry scoping, and consent-based migration are owned by `resource-selection`, `config-mutation`, and `config-migration`.
- §2 canonical config shape: the YAML examples and the root/fragment field table are owned by a later `config-layering` baseline addition; the legacy-key rejection is carried (key family, per D4 binding note); "legacy roots reject v4 installation keys" (the v2/v3 reader refusing `sources`/`resources`) is implemented and tested but not carried here — owned by a later `config-layering` baseline addition as schema coexistence.
- §3 source declarations: source-name grammar, the one-discriminator rule, per-kind field tables, duplicate-declaration coalescing, and conflict diagnostics are owned by later `config-layering` baseline additions and `resource-selection`. The Marketplace declaration's rejection of `ref`, `path`, and `source_harness` is implemented (`v4.source-field`) but no test proves it, so it is not carried until a test binds it.
- §4 resource groups (compact and expanded forms, locator grammar, per-resource `as`/`revision` options, dynamic selection, bounded exclusion grammar): owned by `resource-selection` and `config-mutation`.
- §5 includes, scopes, and ownership (composition rules, whole-item option replacement, last-declared policy fields, self-contained source bindings, cross-layer copying rules): owned by `resource-selection`, `config-mutation`, and later `config-layering` baseline additions.
- §6 acquisition lock contract (lock v2 wire shape and required records, identity binding, missing-lock behavior, dynamic membership, deterministic updates, the exact-pin migration slice): owned by `acquisition-lock-codec` and `config-migration`.
- §7 command and editor behavior (install/remove/sync/update/list/status rows, the shared CLI/TUI editor, comment-preserving writes): owned by `config-mutation`; the Marketplace endpoint-registration row is carried in substance by the expression requirement.
- §8 migration and compatibility (version coexistence table, semantic conversion rules, legacy exact-pin conversion): owned by `config-migration`.
- §9 diagnostics and acceptance criteria (stable-code distinctions, the verification matrix): planning material; the individual diagnostics are owned by the baselines of the surfaces that emit them.
