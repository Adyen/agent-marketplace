## Context

Consumer loading currently branches on the version sentinel: v4 goes through the explicit source/resource parser and a runtime adapter, while older syntax reaches a compatibility decoder and legacy editor paths. `internal/config/config.go` also holds the runtime requirement model used by resolution; its historical shape does not make that model an on-disk compatibility contract. CLI startup and the TUI can initiate conversion, and several gates currently require successful legacy operations or candidate-run migration.

The canonical `config-layering`, `regression-gates`, `tui-interactions`, and `config-mutation` requirements must change together. The existing acquisition-lock codec and source-revision compatibility contracts must not change with them. This design deliberately breaks old consumer roots, not every format whose name includes v1, v2, v3, legacy, or migration.

## Goals / Non-Goals

**Goals**

- Ship one consumer reader/editor contract for every layer and entry point, with exact root version validation and v4-only included fragments.
- Refuse unsupported input before beginning new dependency, acquisition, configuration, or harness work, with stable file/field diagnostics and non-vacuous preservation tests.
- Delete consumer conversion and compatibility implementations, not merely hide their commands or keep fallbacks unreachable by convention.
- Retain all v4 selection, policy, update, materialization, ownership, and recovery behavior, including relevant behavior currently tested with legacy fixtures.
- Make CI exercise the new launch contract while preserving released v4 interoperability.

**Non-goals**

- Replacing the internal requirement model, rewriting resolution, changing source/resource scope or ordered decisions, or reimplementing v4 `update --bump`.
- Changing acquisition-lock v1 compatibility/v2 publication, source-revision v1-v3 hashing, Marketplace operator `agent-marketplace.io/marketplace/v1`, Marketplace API/protocol versions, resource artifacts, source layouts, native harness compatibility, ownership state, or journal formats.
- Adding a replacement converter, dependency, hidden migration escape hatch, or automatic organization-config edit.
- Suppressing ordinary logs or preventing completion of previously admitted journaled writes.

## Decisions

### 1. A single version gate, not fallback-by-peeking

Existing consumer roots at `/etc/agent-marketplace/config.yaml`, `~/.agents/config.yaml`, `.agents/config.yaml`, and `.agents/config.local.yaml` must contain a string scalar exactly equal to `agent-marketplace/v4`. No trimming, case folding, aliases, or schema guessing may turn another value into v4. An absent file retains its existing missing-layer/creation behavior; an existing empty or versionless file is not absent. Existing YAML syntax, ambiguity, bounded-read, containment, and captured-provenance checks remain in force.

Use the existing v4 diagnostic identities consistently: `v4.api-version-required` for an omitted root field and `v4.api-version` for an explicit unsupported or invalid value, including empty/null/non-string values. Both must carry the offending authored file path and field context `apiVersion`, plus line context when available. Check the sentinel before legacy field semantics so an old root with `registries` or `marketplaces` reports the format problem instead of being accepted, ignored, or incidentally rejected as a v4 unknown field. A root labeled v4 but containing old installation keys still fails with the existing `v4.legacy-key` diagnostic; it is not automatically converted.

All public consumers must reach this policy: full and configuration-only loads, online/strict/cached pipeline loads, organization policy/settings readers when they interpret consumer configuration, direct layer editors, staged overlays/reloads, initialization of an existing file, ad-hoc `--from` commands, CLI and standalone TUI entry points. Auxiliary logging initialization may fall back to defaults and log a warning; it must not become a successful alternative decoder for a consumer operation. Commands whose purpose is independent of consumer configuration must not acquire a consumer-config dependency merely to apply this gate.

### 2. Include acceptance does not change authority

A fragment is an explicitly included local document under a validated v4 root, not a versionless root discovered by another route. It may omit the field or explicitly declare exact v4; explicit non-v4/invalid fields fail with `v4.api-version` naming that fragment. Validate every required include tree before binding remote resources or starting a new edit. The root cannot inherit its version from a fragment.

Keep include bounds, cycle detection, captured identity, relative-directory bases, source-name binding, fragment/root precedence, and ordered include/exclude decisions. Keep layer-specific field restrictions: exact v4 does not make personal-local policy/harness fields valid. Edits still target the owning selected layer root, not an included file, and acquisition locks stay with the logical root even when approved config links point elsewhere.

### 3. Refusal concerns new work, not recovery or logs

After existing command-specific pending-transaction handling, parse/validate all roots and includes the operation needs before new config/lock staging or publication, remote Git/Marketplace requests, or harness write/prune plans are applied. A rejected layer cannot be skipped so another layer's edit succeeds; ad-hoc acquisition cannot bypass a malformed authored policy layer. Configuration-only reads retain their no-acquisition contract.

Do not move format validation ahead of established safe journal recovery. Read-only modes continue to return `dependency.recovery-required` for pending dependency work without replay. Recovery-capable commands may finish the recorded, already-admitted write set before loading configuration. They then revalidate recovered roots and refuse unsupported input without starting new work. Malformed journals, foreign/replaced generations, and blocked recovery remain fail-closed with evidence retained. Replay of captured transaction bytes is not a legacy config decoder and must not depend on removed migration code. Ordinary log creation is outside the config/lock/harness preservation assertions.

Refusal tests need a remote request counter or failing acquisition sentinel, seeded valid resources and owned output that would otherwise be pruned, existing config/lock bytes and metadata, and missing writable destinations that would otherwise be created. Pending-journal cases assert recovery effects separately from absence of new work; they must not demand that recovery leaves already-recorded destinations unchanged.

### 4. Delete conversion while keeping the runtime bridge

Remove Cobra registration/implementation/help/completion witnesses for `migrate` and both migration targets, startup prompting and their terminal helpers when unused, TUI migration state/modal/actions, conversion-only evidence preparation, legacy consumer unmarshalling, and legacy layer-editor branches. Remove migration-specific public test requirements and stale scenario bindings. Do not leave hidden flags, success-by-warning, or a dormant decoder as an upgrade mechanism.

Retain the v4 parser/editor and the runtime requirement model used by catalog, resolver, and materialization. `ToLegacyConfig`/`ToLegacyConfigWithLock` may become `ToRuntimeConfig`/`ToRuntimeConfigWithLock` to expose their purpose; this is an internal rename, not another serializer. Audit legacy writers individually: remove old-syntax writes, but retain or port shared selection/ref-edit logic that v4 consumers actually use. In particular, keep already-implemented v4 `update --bump` and its owning-root/include behavior. Avoid a mechanical deletion based on historical names in lock, source-revision, harness, ownership, or dependency transaction code.

### 5. Port behavior evidence; retire only the obsolete promise

Classify each legacy-syntax test before editing it:

| Existing evidence | Replacement |
| --- | --- |
| Decoder accepts v1/v2/v3, versionless-root compatibility, candidate migration, prompt/modal conversion | Explicit per-layer/per-fragment refusal and absent-surface tests |
| Bulk/tag/ad-hoc install/remove, required/default/exclude/collection policy, collisions, include authority, ref updates | Equivalent v4 sources/resources fixtures with original positive and preservation assertions |
| Lock byte stability, historical source-revision verification, aggregate ownership/handwritten data, transaction interruption | Keep coverage; use v4 consumer fixtures or admitted journal fixtures where necessary |
| Schema-independent conversion/server/help/completion behavior | Keep successful invocations without requiring a consumer root |

Do not satisfy the command coverage guard by relabeling a legacy-success record as v4, by using help-only invocations, or by adding allowlisted gaps. Negative refusals cannot count as successful public-command witnesses. Keep sandbox/process isolation and compile-checked public-surface witnesses. Add or update `// spec: <capability-path>/<scenario-slug>` lines for every new/modified scenario; remove bindings only for retired or renamed scenarios, not for preserved behavior. Explicitly remove and replace the Marketplace-expression and migrated-workflow requirements rather than dropping their old-format scenarios from a MODIFIED block; carry their still-applicable scenarios/assertions into the v4 replacements.

### 6. Replace the pre-v4 executable gate; keep the stable v4 gate

Chosen approach: remove the `upgrade-gate` Make target, `UPGRADE_GATE_TESTS`, pre-v4-only executable download/checksum/job wiring, and tests whose contract is successful candidate execution/migration of pre-v4 roots. No pre-v4 binary is required for launch acceptance. Port their schema-independent assertions into v4 tests: exact locked revision and byte stability until explicit update; handwritten native aggregate preservation and existing owned-entry removal; personal-local/relative-directory scopes; same-origin multiple refs and collision winners. Retain historical output/source-revision/ownership fixtures where they prove supported independent formats. Keep the shared isolated historical-binary runner used by released-client tests.

Witness the new unsupported-root/include refusal matrix in the normal Marketplace E2E gate, listing required scenario tests through the existing witness mechanism so skip/rename/nonexecution fails. Keep `E2E Gates` as the aggregate status, depending on planner success plus the Marketplace and released-client jobs; selected jobs must both succeed, and only planner-unselected jobs may both skip. Remove the retired job from all `needs`, result comparisons, release eligibility dependencies, planner assumptions, artifact labels, and developer instructions, rather than leaving an expected skip that masks missing acceptance.

Keep `released-client-gate` pinned to v4.1.4 and its existing architecture checksums and isolated, separate HOME/checkout actors. Change the main repository fixture in `e2e/released_client_compat_test.go` from v2 registries to v4 sources/resources. Preserve candidate sync/install/update/remove, released `sync --locked`/`sync --check`, byte-identical healthy locks, native-hook source-layout verification, and owned hook upgrade/deselection checks. Successful consumer-command/flag inventory becomes v4-only, covering every present consumer layer, including organization; schema-independent surfaces retain successful configuration-free invocations.

## Rollout / Pre-upgrade conversion

Publish the breaking format requirement in existing site/reference/upgrade pages and generated command documentation. Users keep or obtain an older released binary that provides the migrator, convert all writable roots and their include trees before upgrading, and verify that the complete installation is valid v4. The older migrator can refuse unrepresentable input or exact Marketplace pins lacking verified evidence; do not describe conversion as unconditional. Consumer organization configuration is externally managed and must be converted/deployed by its administrator. Merely changing the version string is insufficient.

The launch binary offers only a path-specific refusal and guidance to perform conversion before upgrading; it neither calls nor downloads the older migrator. Rollback means using a compatible earlier binary with backed-up configuration, not introducing old-format writes in the launch release. Shared committed v4 config/lock output remains compatible with the pinned v4 stable client.

## Implementation batching and verification

Delegate bounded implementation batches with non-overlapping ownership: config reader/editor policy; CLI/pipeline surfaces; TUI; E2E/CI/test infrastructure; existing documentation/guidance. Consumers wait for the config boundary instead of inventing local gates. Each batch runs focused positive and negative checks, an ordinary review, an adversarial consumer-path/side-effect/ownership/test-validity review, and `open-code-review-delegate` (including required security-review routing). Record reviewed diffs and verify findings before moving on.

Because command coverage and CI/test infrastructure change, final implementation validation includes `make validate`, `make validate-full`, `make spec-check`, the dedicated checksum-verified released-client gate, and `make site-build`. The combined E2E/configuration-compatibility and TUI QA gates inside full validation need not be duplicated after success. Run domain-specific checks separately when not included, and rerun a timed-out scenario alone before calling it a regression. Use the pinned OpenSpec CLI to archive as the final task in the implementation pull request, not during proposal work.

## Risks / Open Questions

- The primary risk is a hidden alternate consumer decoder/editor or an acquisition before a later invalid layer is read. Inventory and direct-entry-point tests are required, not just parser tests.
- Bulk fixture conversion can weaken independent selection/safety coverage. Track behavioral assertions and preserved scenario bindings, and review each deletion.
- Journal recovery may legitimately change recorded destinations before a format refusal. Tests and release guidance must distinguish those effects from newly attempted operations.
- Removing the pre-v4 job affects release dependencies and required aggregate status wiring; verify selected/unselected/failure/skip outcomes explicitly.
- No product-contract decision is left open: refusal codes, pre-upgrade conversion, gate replacement, and retained compatibility boundaries are chosen above. Exact legacy-only helper removal and final test names/bindings are implementation inventory outcomes, not permission to retain compatibility.
