# Pre-v4 to v4 Regression Gates

**Date:** 2026-09-24
**Status:** Approved design; implementation not started
**Worktree branch:** `docs/pre-v4-regression-gates`
**Implementation base:** `67b9eb87c4d739b75e2b3d4551c9bbce78392c20` (main, including PR #302)

## 1. Decisions and objective

Prevent another pre-v4 to v4 rollout that preserves syntactically valid configuration but breaks existing users' workflows. The approved compatibility rule is **full workflow parity**, not merely successful parsing or preservation of installed files.

- Protect the **pre-v4 installation to v4 candidate** boundary. Do not expand this work into an every-historical-release compatibility project.
- Pin the last published pre-v4 stable release, **v3.1.1**, source commit `ebae4a05e589df085b54da332c0be86c88421672`, as the historical installation producer. Configuration schema v2/v3 and executable release versions are different concepts.
- Cover the complete public `agent-marketplace` command tree, including server and maintenance subcommands, with behavioral tests and coverage enforcement.
- Extend the existing Go E2E, Go TUI, and real-terminal QA infrastructure. Do not introduce another test framework, TUI scenario inventory, classifier, retry layer, or result authority.
- Test real Marketplace integration and deterministic delayed/failing responses. Neither substitutes for the other.
- Make required contracts block merges and releases. Repeating a narrow suite nightly does not replace missing deterministic scenarios.

If a supported workflow succeeds before migration and becomes unavailable after accepted migration, the compatibility test fails. Fix the v4 path or defer migration of that state. Do not turn the regression into an expected unsupported-operation diagnostic, skip, or weaker assertion. Existing security boundaries remain authoritative; parity does not require preserving unsafe behavior intentionally prohibited by those contracts.

This document saves the approved plan only. It does not authorize production changes, repository-settings changes, commits, or pushes in the planning session.

## 2. Findings from the audit

The initial audit used main at `e31280ca82741dc173f8e2a0c8e13b32d2acb4b4`. PR [#302](https://github.com/Adyen/agent-marketplace/pull/302) merged while the plan was being prepared; the worktree was created from the updated main above.

| Existing evidence | Gap to close |
|---|---|
| [Public Marketplace gate](../../e2e/public_gate_test.go) indexes a real registry, uses production server code, and installs every materialized resource kind | Its consumer round-trip starts with v2 configuration; this is not a complete migrated-v4 lifecycle gate |
| [Marketplace lifecycle tests](../../e2e/marketplace_lifecycle_test.go) cover exact pins, offline use, failures, identity, policy, revocation, and collections | Most share a legacy consumer fixture; extend rather than duplicate their real-server machinery |
| [Migration E2E](../../e2e/migrate_v4_test.go), [v4 runtime E2E](../../e2e/v4_runtime_test.go), and extensive package tests exist | No discovered historical-executable upgrade gate proves the complete before/after workflow contract |
| [Organization Marketplace PTY journey](../../tools/tui-qa/tests/org-marketplace-lock-failure.spec.ts) counts acquisition requests across startup, sync, and update | It does not edit a selection in that session, which is the cache-invalidating transition behind #302 |
| [PTY fixture](../../tools/tui-qa/tests/fixture.ts) has two local skills; the [runner](../../tools/tui-qa/tui-test.config.ts) permits five-second ordinary assertions | No overflowing-list fixture for the scroll regression; a three-second toggle can still pass |
| [CI](../../.github/workflows/ci.yml) compares a fixed benchmark selection | Results are explicitly informational; #302's new acquisition-after-edit benchmark is not in that selection |
| Release depends on E2E and other checks in the same workflow | It does not depend on TUI QA; better tests alone would not close that release boundary |
| [Documentation contract test](../../cmd/agent-marketplace/docs_contract_test.go) walks Cobra commands and flags | Reuse this discovery pattern to detect missing behavioral coverage, not just missing documentation |

#302 attributed slow toggles to revalidating all configuration reads in the session acquisition cache. A selection-layer edit therefore reacquired organization Marketplace evidence. The PR reports roughly 2.3–3.7 seconds for that network acquisition. It also fixes a Status-tab viewport jump. Its description identifies staged mutation coverage as a remaining gap and reports a possible generation conflict when toggling again during reload; neither is assumed solved by faster execution.

These findings are based on source and PR inspection. No test suite or timing baseline was executed during planning.

## 3. Ownership and implementation boundaries

Follow [CODING_STANDARDS.md](../../CODING_STANDARDS.md), [the precedence contract](../PRECEDENCE.md), and [the TUI product contract](../tui-product-contract.md).

| Layer | Responsibility |
|---|---|
| `cmd/agent-marketplace` tests | Cobra surface discovery, argument/flag contracts, startup behavior, consumer-path regressions |
| `internal/config`, `internal/pipeline`, transaction and ownership tests | Configuration semantics, acquisition policy, invalidation, transaction recovery, fail-closed boundaries |
| `internal/tui` tests | Request ordering, operation serialization, model/render invariants, deterministic concurrency and benchmarks |
| `e2e` | Built-binary lifecycles, historical upgrade scenarios, filesystem and configuration matrices, real-server integration |
| `tools/tui-qa` | Small canonical real-terminal journeys with causal visible assertions, interaction timing, and relevant filesystem outcomes |
| Existing harness compatibility suite | Native output and target matrices; do not reproduce every target combination in every CLI or PTY scenario |
| Existing CI planner and workflows | Select and enforce the owning suites, preserve diagnostics, and prevent publication without required results |

Use declarative `TestCase` scenarios for ordinary setup/run/assert workflows. Use standalone E2E tests for historical processes, server lifecycles, concurrency, and setup that the existing table cannot express. Introduce only the fixture capabilities required by the next vertical slice.

The earlier [deterministic TUI QA design](2026-09-04-deterministic-tui-qa-journeys.md) remains the basis for causal assertions, zero retries, one PTY worker, and ownership boundaries. This plan expands its two-resource fixture only for specific journeys and proposes CI log/timing artifact retention and release dependencies. It does not revive the removed custom tracing/result framework. Update affected normative documentation with the implementation so these scope changes are explicit.

## 4. CLI contract coverage

### Surface and behavior

At the audited base, the 18 registered top-level commands are `init`, `lock`, `sync`, `list`, `search`, `install`, `remove`, `migrate`, `convert`, `doctor`, `gc`, `update`, `status`, `tui`, `backup`, `deinit`, `serve`, and `telemetry`.

Discover the current tree from Cobra instead of maintaining this list as an executable authority. Include nested commands, public aliases, root help/version, bare TTY/non-TTY behavior, and generated completion behavior where exposed. Do not inherit the documentation walker's help/completion exclusions without classifying and testing those entrypoints deliberately.

For each public command/flag, associate meaningful behavioral contracts with actual tests. Important branches include:

- Writable scope selection and externally managed organization policy.
- Resource kinds, ambiguity, tags/all selections, exclusions, required resources, and collections.
- Configured sources versus `--from`, including ref/path identity and command forms with and without a resource name.
- Check, locked, offline, dry-run, upgrade/bump, and output modes where supported.
- Backup create/list/restore, deinit preservation and purge, doctor repair/consent, and GC retention of referenced resources.
- Telemetry consent and controls against local synthetic endpoints; server startup, roles, reindex, and adoption using isolated stores.
- Valid inputs, invalid combinations, no-op behavior, cancellation/confirmation, and externally relevant negative side effects.

Every public executable command needs built-binary behavioral evidence. Package-level tests supplement detailed parsing and policy matrices. Help output, constructor coverage, or an invocation with no behavioral assertion does not satisfy a functional contract.

### Enforcement

1. Compare discovered commands/flags with explicit test assignments and flag semantics; fail on an unclassified addition or stale assignment.
2. Associate assignments with real test identifiers and the relevant invocations, not arbitrary prose claiming coverage.
3. Use Go test events and the existing runners' results to verify that required tests actually ran and passed. Missing or skipped required tests fail the gate.
4. Keep expected outcomes independent of production discovery/migration/resolution helpers. Discovering what exists is not computing what should happen.
5. Test the coverage guard itself: an added command/flag and a missing assigned test must make it fail.

The existing E2E JSON report covers the declarative table, not all standalone tests. It cannot alone prove complete execution. Extend reporting only as needed around standard Go test output; do not create another pass/fail authority.

Cover every supported flag behavior, not the infinite Cartesian product of all argument values. Explicitly test high-risk interactions and representative equivalence classes. Schema-independent commands do not need artificial multiplication across all configuration states.

## 5. Brownfield fixture and upgrade contract

### Historical producer and isolation

Acquire the pinned v3.1.1 executable through a reproducible setup step, record the release/source identity and verified artifact checksum, and cache it by that identity. No floating `latest` lookup or silent fallback to the candidate is permitted. Select and record the actual artifact checksum during implementation; it was not fetched during planning.

Run historical executables in disposable Linux container sandboxes. An old executable may not honor current test-only organization-path overrides, so HOME/XDG overrides alone are insufficient. Do not mount the developer's home, organization configuration, credentials, or host root. Registry and service access during test execution stays local; artifact/dependency acquisition is a separate setup concern. Missing required isolation or historical artifacts is a failed/blocked setup, not a skipped successful gate.

Have the historical executable create representative populated Git/local installations. Preserve the same sandbox paths when switching to the candidate so existing symlinks are genuine upgrade inputs rather than accidentally broken fixture copies. Repeat the historical setup for independent branches or clone the complete filesystem while retaining its container-visible paths.

Do not require pre-v4 executables to implement Marketplace capabilities introduced later. Cover Marketplace adoption as a new environment condition applied to the populated legacy installation, and cover legacy Marketplace-shaped configuration with independently authored fixtures where needed.

### Fixture contents and assertions

Each fixture declares initial config layers, resources and kinds, source identities, target harnesses, locks, cache state, installed output, ownership evidence, and handwritten sentinels. Add synthetic reproductions of actual rollout incidents as their precise inputs become available; real customer/home contents and organization identifiers do not belong in fixtures.

Assertions cover selected resource identities, scopes, required/excluded semantics, exact revisions, installed names and bytes, native output paths, and diagnostics. Protected config/lock/handwritten state uses byte, permission, and relevant metadata checks. Normalize only inherently variable fixture paths or timestamps in semantic comparisons; never normalize away scope, revision, ownership, or content differences.

Run three branches from equivalent starting state:

1. Candidate reads and operates on legacy configuration without migration.
2. Candidate accepts migration, then performs the same supported workflows and restarts.
3. Migration is declined or safely refused; protected state remains unchanged and subsequent legacy workflows still work.

A refusal test must prove a useful legacy operation still succeeds. A preservation test must also prove the intended update happened. Fresh-v4 fixtures provide an additional control against upgrade-only assumptions.

### Required scenarios

| ID | Scenario | Contract |
|---|---|---|
| B01 | Replace pre-v4 executable; retain valid legacy roots | Supported read and mutation workflows remain usable; no forced migration |
| B02 | Non-interactive legacy invocation | No prompt hang or implicit conversion; documented warning/legacy continuation |
| B03 | Accept CLI or TUI startup migration | Only user-personal root is offered; repo/local/org roots remain untouched |
| B04 | Migrate user root; retain populated legacy repo/local roots | Mixed-layer precedence, editing, scope, and selected resources are preserved |
| B05 | Explicit repo/local/all migration | Representable states retain full workflow parity; organization policy is never migrated |
| B06 | Decline, cancel, dry-run, or refuse unsupported conversion | No unapproved config/lock/backup/output mutation; legacy behavior remains usable |
| B07 | Legacy includes, explicit empty lists, source-free local selectors, relative directories, duplicate names and refs | Semantics preserved or conversion refused without loss; no lossy flattening |
| B08 | Existing exact Git locks, populated cache, and handwritten shared aggregates | Migration does not advance pins, discard ownership, overwrite handwritten data, or widen harness targets |
| B09 | Verified versus missing/corrupt Marketplace pin evidence | Valid evidence preserves identity and exact content; unproven conversion refuses safely |
| B10 | Add v4 organization Marketplace source without organization lock | Online acquisition works transiently; no organization writes; strict/offline contracts remain intact |
| B11 | Present valid, malformed, mismatched, or untrusted organization lock | Valid lock consumed exactly; invalid present lock fails closed without transient downgrade |
| B12 | Missing personal config/cache with legacy repository state retained | Init/recovery produces a usable workspace; no delete-everything prerequisite |
| B13 | Concurrent edits or interrupted migration publication | Transaction rejects stale inputs or recovers coherently; no silently partial conversion or unexplained residue |
| B14 | Repeat migration, sync, and restart | Idempotent intended state; no unnecessary protected-file rewrites |

Execute applicable lifecycle actions: `list`/`status`, install/remove, lock/sync/check, update, and TUI selection workflows. Use fresh branches for actions that intentionally alter the expected starting state; do not let an early destructive action make later assertions vacuous.

Exercise every writable scope and all supported resource kinds at their owning test layer. Include a representative native-output target in broad command journeys and retain the existing all-target compatibility suite for native layout policy. Add focused shared-aggregate fixtures where ownership preservation is the contract.

## 6. Marketplace service coverage

Reuse `newMarketplaceBehaviorFixture` and the public gate's registry/indexing helpers. Retain production-handler integration for broad deterministic matrices, and add a bounded executable-level round-trip using the actual candidate `serve` process, readiness endpoints, and graceful termination. Existing process startup checks alone do not prove a client lifecycle against that process.

Use temporary SQLite metadata, filesystem blobs, and local Git origins with all materialized kinds plus a collection. Production PostgreSQL/S3 infrastructure is not a prerequisite for the consumer regression gate.

Required lifecycles:

- Endpoint registration and discovery, search, exact acquisition/install, inspection/status, synchronization, removal, and restart.
- Fresh v4, legacy continuation, migrated configuration, and mixed legacy/v4 organization state.
- Mirror and index-only acquisition where supported, exact retention on sync, and supported update behavior without rebinding unexpectedly.
- Direct and collection-derived selection, exclusions, optional versus required organization resources, collisions, and cross-scope preservation.
- Warm offline success and cold offline failure; unavailable/partial endpoints; identity mismatch; digest corruption; blocked or revoked resources.
- Protected config/lock/installed bytes remain coherent when an acquisition or publication fails.

Tests must distinguish an intentionally unsupported new feature from a supported workflow lost during accepted migration. Existing v4 hosted-edit/update gaps are not automatically acceptable parity exceptions: classify the actual workflow, and fix or defer migration when parity requires it.

## 7. TUI interaction and responsiveness

### First vertical slice: PR #302

Use the existing organization-without-lock fixture, extended only as needed:

1. Launch cold and prove successful initial organization acquisition and visible resources.
2. Wait for the authoritative baseline, then toggle a local resource through the real scope modal and staged config-write path.
3. Assert the new persisted selection and authoritative visible state, plus **zero additional organization acquisition-data requests** after the selection-only edit.
4. Repeat the appropriate edit variants in Go consumer tests for user/repo/local scopes. Do not multiply the whole PTY suite into a scope matrix.
5. Assert organization bytes unchanged and continued absence of its lock.
6. Use a separate overflowing Status-list journey: disable a row, require the next intended row selected, retain visible section context, and prevent an unjustified viewport jump.

The initial request count must be positive and serve verified content. Distinguish organization reacquisition from legitimate discovery/search or unrelated acquisition; a blanket zero-network assertion would be incorrect.

Positive controls must prove that organization/include changes, lock appearance, effective target changes, and registry/Marketplace allowlist changes are revalidated. Reacquire when policy permits; reject safely when it does not. Changed inputs during in-flight acquisition must not publish or cache stale evidence. Preserve the nil-versus-empty policy distinction.

Run the new interaction tests against pre-fix main `e31280ca82741dc173f8e2a0c8e13b32d2acb4b4` and fixed main `67b9eb87c4d739b75e2b3d4551c9bbce78392c20` in disposable comparison worktrees/builds. Record the specific red assertion and green result. Do not revert a shared branch to obtain the negative control.

### Controlled asynchronous behavior

Go tests own the interleaving matrix. Use bounded request-start/release handshakes rather than fixed sleeps to hold work in flight and release completions in chosen orders. Add a small PTY journey for user-visible responsiveness under delayed service work.

Cover a second toggle while reload is active, superseded searches, late snapshots, policy changes during acquisition, and quit/error recovery during pending work. A second mutation must follow the serialization contract; a generation conflict is not acceptable merely because it becomes less likely on a fast runner. Do not weaken compare-and-swap or policy validation to fix interaction races.

Every ordinary state-changing input is followed by a causal assertion that was false before the input. The explicit overlap test deliberately sends input while a proven operation is pending and asserts the documented blocked behavior. Never use old terminal scrollback or a pre-existing `in sync` label as proof of a new operation's completion.

### Timing contracts

Measure separately:

| Measurement | Required distinction |
|---|---|
| Input to visible acknowledgement/navigation | Local responsiveness must not wait for injected network delay |
| Selection edit to authoritative settled state | Includes real config write, lock handling, reload, and drift calculation |
| Cold startup/acquisition | Network-inclusive; not compared with a warm toggle budget |
| Request count per warm selection edit | Deterministic guard against unnecessary organization reacquisition |

Use monotonic time, fixed fixture workload and terminal dimensions, and explicit cold/warm states. Measure repeated baselines on Linux/macOS CI runners, select and commit numeric per-operation ceilings with documented headroom, and enforce them before calling the timing gate complete. No measurements were made in planning, so this plan does not invent millisecond thresholds.

Keep bounded hang timeouts separate from latency ceilings. Keep zero retries and identical local/CI semantics; report failures rather than hiding them behind retries or CI-only timeout multipliers. Use stage logs and request histories to distinguish service delay from local work. Preserve neighboring benchmarks, explicitly register the new benchmark in CI selection, and retain benchstat evidence; an informational benchmark comment is not the blocking interaction test.

## 8. CI and release enforcement

| Execution tier | Required coverage |
|---|---|
| Relevant pull requests | CLI contract guard; Linux E2E including pinned pre-v4 upgrade and real Marketplace lifecycles; Go async/consumer regressions; Linux/macOS real-terminal journeys |
| Main and release candidate | Full relevant suites, without an inherited narrow test filter; production executable upgrade smoke in the container sandbox |
| Release publication | Explicit dependencies on successful required gates, including TUI QA; verify tested source/artifact provenance before publication |
| Nightly/manual stress | Repeated latency/interleaving cases and broader combinations; not the sole owner of a deterministic incident regression |

Extend `tools/ci-plan` tests for config, migration, pipeline, TUI, Marketplace, command, fixture, dependency, and workflow changes. Preserve conservative fallback behavior; optimization must not silently omit a required consumer test.

Use a stable required-check aggregation pattern that fails on unexpected skipped, cancelled, missing, or failed required jobs. Prose-only skips may remain explicit on PRs; release checks must not depend on PR-only change selection. Verify required-check settings in the repository with the owner before changing them. This plan does not itself change repository policy or bypass existing controls.

Failure evidence should include standard test output, scenario and executable identities, command arguments/exit status, synthetic state differences, server logs/request paths and counts, current terminal output, and measured operation durations. Use standard runner results and CI artifact uploads, not a custom TUI report engine. Retain evidence before fixture cleanup; include only synthetic fixture data and redact credential-shaped values. Both standalone and table tests must be represented in execution evidence.

## 9. Implementation sequence and acceptance

Implement vertical slices: one meaningful failing consumer test, the necessary fixture/support change, then green verification. Production defects exposed by the new gate receive separately reviewable fixes; do not bulk-write imagined tests or build a general-purpose framework first.

### Slice 1: known-incident regression proof

- [ ] Add the #302 real staged-toggle/request-count regression and overflow viewport journey.
- [ ] Add deterministic mutation-during-reload coverage using the existing operation contract.
- [ ] Demonstrate #302 assertions fail on pre-fix main and pass on the fixed base.
- [ ] Record exact revisions, failing assertions, and applicable focused test commands.
- [ ] Convert additional reported v4 incidents into synthetic reproductions with positive controls.

Acceptance: the added tests reject the actual regression; faster execution alone cannot mask a missing ordering invariant.

### Slice 2: historical installation and migration lifecycle

- [ ] Pin and verify v3.1.1 executable provenance and artifact checksum.
- [ ] Add container-isolated historical setup and candidate switching without host policy access.
- [ ] Implement B01–B14 at their owning layers, prioritizing populated mixed-layer and organization-Marketplace transitions.
- [ ] Exercise legacy continuation, accepted migration, and refusal/decline branches.
- [ ] Demonstrate representative migration tests fail against the affected v4 revision, selected per incident rather than assuming all bugs began in v4.0.0.

Acceptance: supported workflows retain parity after conversion, and refusal leaves a demonstrably usable legacy installation. Any discovered parity gap remains a blocker with an explicit fix/defer decision.

### Slice 3: complete CLI coverage and its guard

- [ ] Reuse Cobra surface traversal and map command/flag behavior to existing tests before adding missing cases.
- [ ] Add missing functional cases, including maintenance/server subcommands and public root entrypoints.
- [ ] Verify required tests executed, including standalone cases; reject stale, missing, or skipped assignments.
- [ ] Demonstrate the guard fails for an untested command/flag addition.

Acceptance: every public command and supported flag behavior has actual behavioral evidence, without counting help-only smoke or arbitrary expected failures as success coverage.

### Slice 4: real Marketplace lifecycle and timing budgets

- [ ] Extend existing production-server fixtures across fresh-v4 and upgraded/mixed clients.
- [ ] Add the executable `serve` plus client round-trip with bounded startup/shutdown.
- [ ] Add controlled failure/delay interleavings without replacing real-server integration with mocks.
- [ ] Measure and commit per-operation timing budgets, request-count assertions, and benchmark selection.
- [ ] Verify local navigation, truthful completion, and error/quit behavior under held responses.

Acceptance: real integration and deterministic fault scenarios both pass; timing contracts are measured and enforced rather than merely reported.

### Slice 5: blocking merge/release wiring and diagnostics

- [ ] Wire the suites into existing Make/CI entrypoints without duplicating full runs unnecessarily.
- [ ] Test change selection and required-result aggregation, including unexpected skips/failures.
- [ ] Make release depend explicitly on TUI and upgrade/CLI/Marketplace gates.
- [ ] Add safe failure artifacts using existing runner output and standard CI uploads.
- [ ] Verify required-check settings with the repository owner; obtain separate approval for any settings change.
- [ ] Update owning product/test documentation and local verification instructions where behavior or scope changed.

Acceptance: a missing or failing required contract cannot yield an eligible release; nightly stress is supplemental, not a replacement.

## 10. Verification and completion

Use affected-package tests and one focused scenario during iteration. Follow the repository's `make validate-e2e`, `make tui-qa-check`, `make tui-qa`, and full validation guidance at the appropriate slice boundary. The historical container gate and actual release-binary smoke need explicit local/CI entrypoints when implemented; existing `make validate-full` must not be described as covering them before it is wired accordingly.

For async/pipeline changes, run applicable race checks and ordering controls; for hot paths, retain before/after benchmark evidence. For every incident regression, record a valid control plus a negative control that exercises the actual consumer path. Do not infer coverage from filenames, aggregate line coverage, or a successful test command that selected no relevant tests.

The work is complete only when all of the following hold:

1. All supported public CLI command/flag contracts have executed behavioral coverage.
2. Pre-v4 installations retain full supported workflow parity after accepted migration, with safe and usable legacy continuation otherwise.
3. Real Marketplace lifecycles and delayed real-terminal interactions are blocking gates.
4. Known broken revisions demonstrably fail their corresponding new regressions.
5. Numeric timing contracts and deterministic request/ordering invariants are enforced.
6. Release publication depends on the required successful gates; unexpected missing/skipped results cannot pass.
7. No test depends on production Marketplace infrastructure, real user state, or hidden retries.

## 11. Explicit non-goals and remaining implementation inputs

Non-goals: every historical release, Windows, a new test DSL/runner, exhaustive command-by-harness Cartesian products, production Marketplace uptime testing, a broad UI redesign, or declaring existing unsupported hosted features implemented by adding tests.

Inputs still to establish during implementation:

- Verified historical release artifact checksum and reproducible acquisition/cache setup.
- Minimal synthetic configurations for each concrete migration incident and its affected revision.
- Measured numeric timing ceilings and any necessary latency-stage instrumentation.
- The exact tests already satisfying each command/flag contract and remaining uncovered behaviors.
- Repository-required check configuration and owner approval for changes outside workflow code.

These are tracked implementation inputs, not permission to silently omit the corresponding gate. Additional capability loss found by parity tests must be reported and resolved or explicitly defer migration, not disappear into a coverage exemption.
