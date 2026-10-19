# Agent Marketplace — agent-marketplace CLI

This repository builds the **agent-marketplace** CLI, a Go binary that syncs shared AI agent resources (skills, commands, subagents, hooks, and MCP servers) from Git-based or local registries and materializes them into native harness layouts, plus the Marketplace service (`agent-marketplace serve`), its Nuxt web UI (`web/`), and the public documentation site (`site/`). Resources follow the [agentskills.io spec](https://agentskills.io/specification). The embedded `pkg/harness-compilation` library performs harness translation; see its own `AGENTS.md`.

## Specs are the authority

- `openspec/specs/` is the only normative description of user-observable behavior. When code, tests, docs, or this file disagree with a spec, the spec wins; when no spec covers a behavior yet, current code and its tests define it.
- Every behavior change is an OpenSpec change under `openspec/changes/<name>/` carrying a spec delta. Create it with `openspec new change <name>` (or the `openspec-propose` skill), implement it, and run `openspec archive <name> --yes` as the final commit of the same pull request.
- Every scenario is proven by a test file line `// spec: <capability-path>/<scenario-slug>`. `go run ./tools/specaudit trace` fails on unbound scenarios and dangling annotations.
- A pull request that changes non-test files under `cmd/`, `internal/`, `pkg/`, `api/`, or `web/` needs a spec delta, or a commit trailer `Spec-Impact: none — <reason>` when behavior is unchanged.
- The domain glossary and artifact rules live in `openspec/config.yaml`; use its terms.
- Legacy documents (`docs/`, the appendix below) are being absorbed by `baseline-<capability>` changes tracked in the `harvest-legacy-docs` change. Treat them as leads only.

## Consumer configuration contract

- Every existing organization, user-personal, repository-committed, and personal-local root requires exact `apiVersion: agent-marketplace/v4`. Missing files keep their creation/absence rules; an existing empty or versionless root is not absent. Included fragments under a validated root may omit the field or declare exact v4; a root cannot inherit its version.
- Missing root versions refuse with `v4.api-version-required`; explicit unsupported/invalid versions refuse with `v4.api-version`, naming the authored file and `apiVersion` field with line context when available. Validate all required roots/include trees before new acquisition, config/lock staging or publication, or harness writes/prunes. Refusal preserves existing bytes/metadata and creates no missing writable root or lock. Ordinary logs and established safe recovery of a pre-existing dependency journal remain separate: read-only modes return `dependency.recovery-required` without replay; recovery-capable commands may complete already-admitted writes before fresh validation.
- The shipped CLI has no `migrate`, startup conversion prompt, TUI migration modal, or legacy consumer decoder/editor. Users convert writable roots and include trees with an older released migrator before upgrading; organization configuration is converted/deployed externally. Includes, unrepresentable input, and missing verified Marketplace evidence can cause conversion refusal. Never advise merely changing a sentinel or promise unconditional conversion.
- Keep the runtime requirement model/v4 adapter and supported v4 edits, including `update --bump`. Acquisition-lock v1 reads/v2 writes, historical source-revision compatibility, operator `agent-marketplace.io/marketplace/v1`, API/artifact/native hook schemas, ownership state, and journal recovery are independent contracts, not removed consumer formats.

## Build, test, and verification

- `make build` builds the binary; `make spec-check` runs pinned strict OpenSpec validation plus `specaudit trace` and `specaudit archived`.
- During iteration, run the affected package/test first: `make test TEST_PACKAGES=./internal/resolver TEST_FLAGS="-run TestResolve -count=1"`. Select packages and consumer regressions explicitly; a scoped result is not full-suite validation.
- `make test` selects changed root-module packages since the merge base with `origin/main` and runs short-mode tests. Root Markdown, known `openspec/`, `.github/`, `.gitlab/`, and `deploy/` metadata, and exact root metadata files (such as `.gitignore`) alone select no Go tests; shared or unknown paths still select all root packages. An explicit `TEST_PACKAGES` overrides selection; `make test-full` runs all root tests without short mode. `make test-fast` aliases `make test`.
- `make validate` and its `validate-fast` alias run selected short-mode tests, non-mutating CI lint, and the build concurrently. `TEST_PACKAGES`, `TEST_FLAGS`, `TEST_SHORT`, and `TEST_LOCAL_BASE` apply to the test target; `make validate-full` runs the full gate. Use `-count=1` when a fresh execution is needed, not reflexively on every unchanged package.
- `make validate-full` runs all root tests freshly (`-count=1`) at full stress counts, lint, build, the combined host E2E/configuration-compatibility gate, shellcheck, and TUI QA. It resets scoped test flags/packages so a prior narrow selection cannot silently weaken the full gate. It does not run vendor-native probes or frontend/container checks; those remain separate domain-specific/CI gates.
- `make validate-e2e` runs E2E and all configuration-compatibility scenarios in one process, avoiding repeated boundary tests and builds. Do not also run `harness-compat-check` or `harness-compat-config` after that successful combined gate unless debugging those gates themselves.
- `make marketplace-proxy-check` executes all three supplied reverse-proxy profiles in isolated Nginx containers and requires the regression witness to run and pass. It needs Docker; `NGINX_IMAGE` selects the pinned runtime or its registry-mirror equivalent. CI runs it for deployment configuration and proxy-test changes. Ordinary root tests skip this Docker-only check rather than counting it as covered.
- The pre-v4 success `upgrade-gate` and its CI job are retired. Launch acceptance witnesses unsupported/versionless roots in every consumer layer and explicit non-v4 included fragments in the normal Marketplace E2E gate. Required witnesses must execute and pass: skips, failures, or nonexecution fail the gate. Keep independent exact-lock/source-revision, handwritten aggregate/owned-entry, local/relative-directory scope, multiple-ref, and collision regressions with supported v4 consumer fixtures; no pre-v4 executable is required.
- `AM_E2E_RELEASED_BINARY=/path/to/verified-v4.1.4-linux make released-client-gate` retains checksum-verified stable v4.1.4 interoperability in isolated containers: teammates with separate HOMEs/checkouts share one v4-configured Git-locked repository, the candidate runs `sync`, `install`, `update`, and `remove`, and the pinned release must still pass `sync --locked` and `sync --check` after each step. Healthy committed lock bytes stay identical through plain candidate sync and released locked/check reads. Native-hook source-layout verification and owned hook upgrade/deselection remain covered. Keep the existing architecture checksums in `e2e/released_client_compat_test.go` and CI for this launch. Change a shared config or lock format by shipping read support before switching writes. Use `AM_E2E_RELEASED_IMAGE` with an already-cached image containing git when Docker Hub is unavailable. Required scenarios run through `tools/witness-check -require`; skipped, failed, or unexecuted scenarios fail. The shared `e2e/historical_binary_test.go` runner remains; released binaries use fixed `/etc` organization paths, so never run them directly on the host. This dedicated gate is separate from `make validate-e2e`.
- `e2e`-tagged builds honour `AGENT_MARKETPLACE_E2E_CRASH_AT=<stage>:<index>` (for example `after-publish:0`), which kills the process at that `dependencytxn` fault point the way SIGKILL would; production builds have no hook. Crash-recovery tests interrupt supported v4 journaled mutations and prove read-only commands refuse pending work without replay while recovery-capable commands complete its already-admitted write set. Recovery then validates fresh roots/includes before any new work, including a subsequent unsupported-format refusal.
- The CI `E2E Gates` job remains the single aggregate status for Marketplace E2E and released-client gates, like `TUI QA Driver` for TUI QA. After successful planning, both jobs must succeed when selected, or both intentionally skip when unselected. Failed/cancelled planning, unexpected skips, missing executions, or failed witnesses fail the aggregate; no retired upgrade job is required. Require `E2E Gates` on `main` rather than the individual gate jobs.
- `CGO_ENABLED=0 go -C e2e test -run '^TestMarketplacePublicGate$' -count=1 .` exercises the actual Marketplace `serve` process with a v4 client, every materialized resource kind, a collection, and offline reuse after shutdown, alongside in-process v4 consumer and revocation controls.
- `VALIDATE_JOBS` bounds concurrent make tasks (default 2). `E2E_PARALLEL` bounds E2E parallel tests (default 4); an explicit Go `-parallel` overrides the runner default, while the declarative table retains its own `E2E_PARALLEL` cap. Tests that mutate process-global environment/cwd remain serial; do not blanket-add `t.Parallel()`.
- Run focused checks after edits. Before handoff, run `make validate` once plus the domain gates the change touches (`make spec-check`, `make validate-e2e`, `make tui-qa`); CI's witnessed Test job owns the full root suite. Reserve local `make validate-full` for changes to those gates, the command-test runner, or shared test infrastructure, or for an explicit request: heavy command matrices can exceed the fixed 2-minute child-process deadline on a loaded host, so rerun a timed-out scenario alone before treating it as a regression. Do not run mutating `make lint` concurrently with readers; validation uses `ci-lint` instead. `make lint-full` runs golangci-lint over the whole root module without the `LINT_BASE` diff filter; the `Full lint` workflow runs it weekly and on dispatch, so fix or justify findings it reports rather than relying on the diff-filtered `ci-lint`. TUI package-manager preflight resolves pnpm from `tools/tui-qa`, where its exact version is pinned.

### CodeQL triage

Inspect the SARIF source-to-sink trace before changing an identity digest to address a sensitive-hashing alert. A static diagnostic such as `URL passwords are forbidden` can be inferred as a credential source without carrying a password value. SHA-256 cache keys, file generations, and ownership roots are compatibility identities, not password-verification records; retain their formats and record evidence when dismissing a verified false positive.

### Command tests

Command tests in `cmd/agent-marketplace` are ordinary top-level `Test<Name>(t *testing.T)` functions, each run in its own sandboxed child process. Every such test opens with exactly `if isolateCommandTest(t) { return }` as its first statement; in the parent that call marks the test parallel, spawns the child, relays its result, and returns true. In the child it returns false and the body runs. Matrices stay together in one child. `TestCommandIsolationGuard` parses the package's test sources and fails on any top-level test that does not start with the guard, except tests in the runner/pure-unit files (`inProcessTestFiles`) and the explicit `inProcessTests` allowlist in `command_integration_test.go`.

Select a test with `go test ./cmd/agent-marketplace -run '^TestBackupCreatedOnInstall$' -count=1`; append `/pattern` for nested subtests. `-run` alternations, `-skip`, `-count`, and `-shuffle` behave as for any Go test; the parent translates the filter into an exact child selector. The default child slot count is `min(max(GOMAXPROCS, 4), 12)`; `-parallel=1` serializes children. Use `-args -command-tests-in-process` for the serial baseline. Coverage builds automatically use that path so counters reach the report.

Child HOME, XDG roots, CODEX_HOME, cwd, organization config, and Git environment are sandboxed. Fixtures that replace HOME and expect Codex beneath it must also set CODEX_HOME. Diagnostic-grouping fixtures use local directories when asserting registry-relative source locations; exact Git snapshots deliberately preserve absolute source paths. Runner and guard checks are `go test ./cmd/agent-marketplace -run '^TestCommand(Process|IsolationGuard)' -count=1`; include `-race` and `-cover` when changing the runner. `TestPublicCommandPathsHaveBehaviorTests` compares every public Cobra command and flag (including completion shells) to compile-checked behavioral test witnesses, and fails when a new surface lacks one; the complete root and E2E suites must execute the referenced tests rather than substituting help-only smoke checks. The guard logs each witness, and `make test-witnessed` (the CI Test job and `validate-full`) pipes `go test -json` through `tools/witness-check`, which fails when any witness skipped, never ran, or failed, or when any package failed. A mapping alone is not coverage.

### E2E tests

```bash
make e2e                  # runs inside Docker (isolated from host env)
E2E_SKIP_DOCKER=1 make e2e  # for CI runners that cannot spawn Docker
E2E_PARALLEL=8 make e2e  # control concurrency (default 4, set to 1 for serial)
```

### TUI QA

Run direct real-terminal test typechecking with `make tui-qa-check` and all deterministic user journeys with `make tui-qa`. TUI QA requires no organization-specific infrastructure; do not place machine-specific values in tracked commands, reports, or fixtures.

A failing journey keeps its GIF and `tui-test` trace under `tools/tui-qa/test-results/<journey>/` (or `AM_TUI_QA_ARTIFACTS`); in CI the `TUI QA recordings comment` job links each failed journey's GIF from one sticky pull request comment. Start each journey from the settled drift state: input sent while the TUI still shows `verifying resources...` can be discarded. The Marketplace stub serves the placeholder-form archive route with archives compressed by Node's zlib, which no Go toolchain reproduces, so the Marketplace journeys pass under any Go version only while clients verify archives by the bytes they receive; keep it that way rather than pinning a toolchain. Never regenerate `tools/tui-qa/pnpm-lock.yaml` through a registry mirror that records its own tarball URLs.

### Local toolchain

Local toolchain notes: on macOS hosts where the system linker rejects the SDK, run Go tests with `CGO_ENABLED=0` (as the Makefile does). In `web/`, `pnpm run` may refuse to start when `node_modules` is stale; `node scripts/generate-api.mjs` and `./node_modules/.bin/{vitest,eslint,nuxt}` run the same checks directly. Tests under `tools/homebrew` require a `ruby` executable on PATH and fail rather than skip when it is absent.

Temporary worktrees should use sibling paths rather than paths nested in the repository: `specaudit trace` scans ignored directories, so nested worktrees can contribute unrelated scenario annotations. Remove only task-created worktrees before final gates; leave existing worktrees untouched.

Crash fixtures must enumerate every journaled participant, not just config roots. A finalized journal does not imply materialized outputs are already in sync: read-only commands may report ordinary drift until the next sync, but must not demand recovery after finalization. Test interrupted recovery separately from the initial publication; recovery fault indexes follow journal positions and skip already-published entries. Assert no staging residue.

Docker Desktop bind mounts can retain stale directory entries after host-side fixture renames. Move or hide historical-world fixture paths inside the container filesystem and verify source absence there before invoking the consumer; do not add sleeps or retries to mask an incoherent precondition.

For Linux checks in a cached Go container, set command-scoped `GOMODCACHE` to the container path of the mounted module cache; do not assume the image's default GOPATH matches that mount. Populate pinned modules through the host's configured proxy when necessary, without changing proxy/authentication policy. A dependency-download failure is not a test result.

### Session completion

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**Pre-push adversarial review:** Before every GitHub push, run an adversarial review of the intended changes, including consumer-path reachability, failure behavior, unintended writes, ownership/scope boundaries, and regression-test validity. Validate reported findings against the code; fix actionable issues and rerun affected checks before pushing. Record the reviewed revision or diff and the outcome. Do not treat a passing test suite or a clean review as proof that no defects remain.

**Lint cache verification:** If golangci-lint reports cached source paths from missing or unrelated worktrees, rerun with `GOLANGCI_LINT_CACHE="$(mktemp -d)" make ci-lint`. A cached `0 issues` result with source-mapping warnings can hide real findings; verify with a fresh cache rather than changing lint rules or CI policy.

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds

## Engineering standards

Read [CODING_STANDARDS.md](./CODING_STANDARDS.md) before modifying code. It defines platform, design, security-boundary, testing (including E2E), observability, logging, and performance requirements.

## Unharvested leads (non-normative)

The sections below are historical, non-normative leads pending harvest, not current usage authority. Live consumer-format and gate guidance has been corrected for v4-only launch; other statements may still be stale, and specs win. Each `baseline-<capability>` change deletes the paragraphs it absorbs. Historical design logs under `docs/plans/` do not advertise supported launch behavior.

### Core Concept

- **Registry repos** contain agent resources (skill directories with `SKILL.md`, commands, subagents, hooks, MCP server definitions). Content is ingested through `harness-compilation` so resources can be authored in any harness layout and compiled into any target harness.
- The CLI reads **layered configs**: external organization (`/etc/agent-marketplace/config.yaml`), user-personal (`~/.agents/config.yaml`), repo committed (`.agents/config.yaml`), and personal-local (`.agents/config.local.yaml`, gitignored). Every existing root requires exact v4; every layer supports recursive local-file `include` whose fragments may omit the version or declare exact v4. Fragments merge below their containing file, while CLI edits remain rooted in the selected config file. Unsupported roots/includes refuse new operations, with no legacy fallback or in-process conversion. See the Consumer configuration contract above for refusal, recovery, and pre-upgrade guidance. `init` creates v4 user configs directly.
- Target harnesses resolve per selecting layer (`resource-selection`). Repository and personal-local selections use the project `harnesses:`, then user, then organization. User and organization selections use user, then organization, falling back to the project list only when neither declares one, so a repository never narrows what machine layers install. Registry entries may further narrow their targets.
- Each registry entry can declare `harnesses:` to narrow its targets; the entry's list is intersected with the effective target set.
- Catalog-only commands (`list`) work without a configured target set and warn; target-dependent commands (`sync`) require a configured set and exit with an error if missing.
- Resources are **materialized** into native harness layouts (e.g. `.factory/skills/`, `.claude/skills/`, `.cursor/rules/`, `.gemini/skills/`, `.windsurf/`, `.devin/skills/`, `.agents/skills/`, `.codex/agents/`) via `harness-compilation`. In the default mode, skills and target-declared skill-directory kinds use symlinks to the registry cache; file mode writes regular files/trees for every target through the generic Agent Marketplace materializer. Other artifacts are compiled per target. Config is the source of truth; generated entries are ephemeral.
- **Claude and Gemini native aggregate installation is distinct from plugin compilation.** Claude MCP servers are installed at `<repo>/.mcp.json` or `~/.claude.json#mcpServers`; Gemini MCP servers and hooks share the scope's `.gemini/settings.json`. Compiler plugin formats remain compatible, and previously generated plugin-style destinations are left untouched rather than moved or pruned implicitly. Extra native roots expose only explicitly mapped aggregate files, never a broad HOME/repository prune walk. Native merges preserve unrelated settings, comments, permissions, and handwritten entries; unmanaged MCP collisions and malformed configurations fail closed. Gemini MCP entries use durable ownership proof instead of in-file markers, which its native schema rejects; hook markers retain their existing policy, and MCP/hook updates share one journaled mutation. Unproven legacy MCP markers are preserved and reported, never automatically adopted or stripped. Gemini hooks use native matcher groups, with legacy flat input retained for ingestion. Incomplete compilation retains stale managed entries while still allowing valid siblings to materialize. Machine backups cover the external Claude configuration through an explicit archive mapping, and new archives are owner-readable only.
- Remote Git requirements are resolved into generated, **layer-owned lockfiles**: organization `/etc/agent-marketplace/lock.yaml`, user `~/.agents/lock.yaml`, repository `.agents/lock.yaml`, and personal-local `.agents/lock.local.yaml`. The repository lock is committed; the personal-local lock is ignored. Organization-lock deployment is optional. Some deployments intentionally never distribute it, and consumer behavior supports permanent absence. An absent organization lock intentionally leaves that external layer mutable for consumer inspection or refresh without creating the lock; online consumer runs, including `sync --check`, resolve explicit organization Marketplace requests into a verified in-memory lock and revalidate every policy input before use. A present organization lock is exact and fail-closed. Plain local-directory registries remain live and unlocked. Configuration-only reads (`config.ReadIntent`) parse and validate without acquiring any lock, and GC performs normal collection work without an organization lock.
- The lockfile schema is versioned: v2 is the sole write format for every writable layer, including Git-only layers, and carries Git resolutions plus exact Marketplace endpoint/registry/resource/selection records used for install/remove, update, the read-only runtime adapter, and transient acquisition when an organization lock is intentionally absent. A writable v1 lock upgrades to v2 on the next reconciliation; a read-only organization v1 lock is still accepted with a `lock.version-deprecated` warning. Offline transient acquisition, hosted rebind, and dynamic-selector snapshots remain absent. Source-revision v4 is current; v1-v3 remain byte-compatible and intentionally retain their historical bare-bundle MCP behavior. Unconsumed-config audits remain warnings but do not own discovery subtrees: diagnostic-only containers retain warnings while nested resources are discovered; actual resource/load diagnostics still bound traversal.
- **Devin is a harness target.** Roots are `<repo>/.devin` and `~/.config/devin`. Skills and converted commands are directory symlinks at `skills/<name>/`, each containing `SKILL.md` (a command carries `x-agent-marketplace-kind: command`), subagents are file symlinks at `agents/<name>.md`, hooks go to `hooks.v1.json` (project) or the `hooks` member of `config.json` (user), and MCP servers go to `mcp_config.json` in both scopes. Hook event arrays contain native matcher groups with nested `hooks` arrays; matchers belong to the group and payload/type/timeout fields to its inner hook. Legacy flat input remains ingestible, and sync upgrades only demonstrably owned legacy entries while preserving handwritten groups and unrelated settings. Rules are out of scope for this target (a scope decision, not a Devin limit): nothing is emitted and a `rule.devin.unsupported` warning is reported without failing the run. These symlinks are the default; `materialization.mode: file` instead writes regular skill/command trees and subagent files through the generic materializer. Installation mode is distribution policy, not a compiler capability, and does not change aggregate ownership reconciliation. Select it through the TUI harness modal (`H`) or by adding `devin` to the applicable `harnesses:` list. Devin is also a valid `source_harness: devin`, and autodetection prefers a `.devin` workspace marker over compatibility layouts.
- **Devin's shared aggregate files are ownership-reconciled, not marker-merged.** No `_managed_by` marker is ever written into a Devin file. Ownership lives in durable journaled state under `$XDG_STATE_HOME/agent-marketplace` (default `~/.local/state/agent-marketplace`), outside the repo and outside the cache, so deleting the cache has no ownership effect. Missing or corrupt ownership state fails closed: every existing entry becomes unmanaged, so sync preserves everything, refuses collisions, and prunes nothing. Handwritten entries, comments, key order, and hook order survive; handwritten hooks precede generated ones. Mandatory raw-byte safety backups are taken even when configured backups are disabled, retaining the five newest per file. Aggregate updates are atomic per file: a malformed `mcp_config.json` blocks only that file.
- **Codex is a partitioned-root harness target.** Repo destinations are `<repo>/.agents` for skills/converted commands and `<repo>/.codex` for subagents, hooks, and MCP servers. Machine destinations are `~/.agents` plus `$CODEX_HOME` (which must be absolute) or `~/.codex`; `CODEX_HOME` never relocates skills. In default symlink mode, skills and converted commands are directory symlinks at `skills/<name>/`; file mode writes regular directory trees there. Each contains `SKILL.md`, with commands marked `x-agent-marketplace-kind: command`; subagents are TOML files at `agents/<name>.toml`; hooks and MCP servers are owned entries in `hooks.json` and `config.toml`. Select it through `H` or `harnesses: [codex]`. It is also a valid `source_harness: codex`. Autodetection requires `.codex` rather than claiming the cross-agent `.agents/skills` layout. Agent Marketplace never installs, requires, or probes a Codex executable.
- **Codex shared configuration is ownership-reconciled and trust-neutral.** `.codex/hooks.json` and `.codex/config.toml` preserve handwritten entries and formatting, use the same durable ownership authority and mandatory backup policy as Devin, and fail closed on missing/corrupt state or unpatchable native forms. An actual repo aggregate mutation emits `materialize.codex.trust-review`; check-only drift emits `materialize.codex.trust-review-pending`. The application never edits Codex trust state or adds Codex output paths to `.gitignore`. Rules, `AGENTS.md` generation, and emitted legacy `.codex/prompts` are out of scope.

### Key Design Decisions

| Decision | Why |
|---|---|
| CLI sync, not git submodules | No submodule friction; marketplace UX for browsing/installing |
| Harness-compilation materialization | Native per-harness outputs instead of one-size-fits-all symlinks |
| Exact-v4 organization config with per-layer target harnesses | `/etc/agent-marketplace/config.yaml` is the externally managed consumer org layer and requires exact v4; repository and personal-local selections use project, then user, then org `harnesses`; user and organization selections use user, then org, falling back to project only when neither declares one; source-bound group `harnesses` narrows the applicable set |
| Multi-resource kinds (skill/command/subagent/hook/mcp) | Full IR fidelity; same selection/exclude UX across all kinds |
| Tags in `metadata.tags` | Uses the spec's extension point; no deviation |
| Attribute inheritance (specific wins) | Per-registry-entry overrides top-level defaults |
| Name collisions across registries resolve deterministically with a warning | Keeps one materializable winner visible; required policy, config precedence, normalized URL, and stable config order determine the winner; labels include `@ref` when URL/path alone is ambiguous |
| Same-URL overlapping paths merge deterministically | Same-URL+ref paths that are component-wise parent/child describe one physical source; required policy wins first, then the deepest path wins equal-policy overlaps, while different scopes remain complementary |
| Same-URL different-path collisions emit warning | Non-overlapping paths remain visible; keep all occurrences for visibility but warn that only one materializes per scope |
| Nested collections preserve branch-local ancestry | Shared DAG includes are valid and expand per branch; only an ancestor repeated on the same include path is a cycle |
| `required:true` groups protect selected resources from excludes/removal | Enforces org-wide security standards without selecting anything by itself; Git/directory `all:true` explicitly selects all resources |
| `--from` ad-hoc registry operations on install/remove/list | Zero-config onboarding: browse, install, and remove resources from any registry URL without editing config files first |
| `.gitignored` generated harness output | Config is committed; generated files are ephemeral on disk |
| Adding Devin did not change default harness lists | Existing configurations keep their selected targets; select Devin through the TUI or a `harnesses:` edit |
| Adding Codex did not change default harness lists | Existing configurations keep their selected targets; select Codex through the TUI or a `harnesses:` edit |
| Codex uses resolved partitioned roots | Codex itself discovers skills under `.agents`, but keeps subagents and shared configuration under `.codex`; an absolute `CODEX_HOME` moves only the machine `.codex` partition |
| Devin ownership in durable XDG state, not an in-file marker | An in-file `_managed_by` key can be written by any tool, so it cannot prove ownership of a file the user also edits; state outside the repo and cache can, and fails closed when absent |
| Codex trust remains Codex-owned | Sync reports when a repo hooks/MCP mutation may need trust review, but never changes trust or repository policy on the user's behalf |
| Devin aggregate updates are serialized per canonical root by advisory `flock(2)`, with compare-and-swap as a degraded-filesystem backstop | Two real processes cannot patch the same Devin aggregate concurrently; `flock` is owned by the open file description so it survives crashes without stale-lock heuristics, and `Store.Apply` refuses with `ownership.generation-conflict` rather than corrupting when advisory locking is unavailable |
| `doctor --repair` never removes or overwrites an entry without interactive confirmation | Adoption is byte-neutral and safe to batch with `--yes`; deleting a user-visible entry, or replacing its value, is not — so both need per-entry consent plus a fresh backup |
| `doctor --repair` offers three classes: adopt an exact match, remove an evidence-backed orphan, or replace an unmanaged entry whose key matches but whose value differs | A collision needs only key equality, adoption needs value equality, and orphan nomination needs evidence only a completed mutation produces — so such an entry was reported by every sync and repairable by nothing |

### CLI Commands

- `agent-marketplace list` — browse available resources from configured registries
- `agent-marketplace list --kind command` — browse one resource kind (skill, command, subagent, hook, mcp, collection)
- `agent-marketplace list --kind collection` — list available collections from configured registries
- `agent-marketplace list --from <url> [--ref <ref>] [--path <path>] [--kind <kind>]` — browse resources at an ad-hoc URL without pre-configuring it
- `agent-marketplace search [query]` — search configured Marketplace endpoints
- `agent-marketplace search [query] --marketplace <alias|url|all>` — search one or all Marketplace endpoints
- `agent-marketplace search [query] --output json` — structured search output with per-endpoint errors
- `agent-marketplace install <resource>` / `install --tag <tag>` / `install --scope {user,repo,local}` — add resource to target config layer and sync
- `agent-marketplace install <marketplace-endpoint>` — register a Marketplace endpoint; defaults to user scope, honors an explicit scope, and is a no-op when the same identity exists in that scope or a broader one
- `agent-marketplace install --kind command X` — disambiguate or select a non-skill resource
- `agent-marketplace install --all [--kind mcp] [--registry URL]` — install every discovered resource
- `agent-marketplace install <name> --from <url> [--ref <ref>] [--path <path>] [--kind <kind>] [--scope <scope>]` — install from an ad-hoc registry without pre-configuring it; without `<name>` installs all resources (bundle mode)
- `agent-marketplace remove <resource>` / `remove --scope {user,repo,local}` — polymorphic remove: deletes explicit selections, appends exclude for auto-selected/default resources; explicit scope selects the machine/repo/local occurrence and preserves other scopes; refuses required resources and machine removal from repo/local scope
- `agent-marketplace remove <name> --from <url> [--ref <ref>] [--path <path>] [--scope <scope>]` — remove a resource from a specific ad-hoc registry entry; without `<name>` removes the entire registry entry; `--path` narrows URL matching to an entry/subtree and `--scope` defaults to repo
- `agent-marketplace lock [--scope user|repo|local|all|organization]` — create or minimally reconcile generated Git locks; `--check` reports required changes without writing and `--upgrade` refreshes all or targeted Git requirements
- `agent-marketplace sync` — materialize from exact Git snapshots, minimally reconciling writable stale locks
- `agent-marketplace sync --locked` — require fresh locks and forbid config or lock writes
- `agent-marketplace sync --check` — check lock and materialization drift without reconciling; exits 1 if out of sync
- `agent-marketplace update` / `update --bump` — advance Git locks, optionally rewriting eligible refs first
- `agent-marketplace status` — show exact Git locks, resources, harnesses, targets, refs, cache freshness, source layer, drift, and errors. Includes `EXACT REGISTRY LOCKS` and `HARNESS TARGETS` tables
- `agent-marketplace doctor` — show local harness/root diagnostics and non-blocking Devin CLI compatibility guidance
- `agent-marketplace doctor --repair` — preview or interactively repair Devin and Codex shared-aggregate ownership state
- `agent-marketplace doctor --repair --yes` — adopt exact current desired matches without prompting; destructive orphan removal and conflict replacement still require interactive confirmation
- `agent-marketplace tui` — launch interactive browser (default on bare TTY invocation); `H` opens the harness modal. Normative TUI behavior is in [`docs/tui-product-contract.md`](./docs/tui-product-contract.md)

### Tech Stack

- **Language:** Go (static binary, no runtime deps)
- **Distribution:** package managers and GitHub releases
- **Build version:** retain `-ldflags "-X main.version=<release>"` compatibility for MSC packaging. CLI startup resolves that stamp before falling back to Go main-module metadata or `dev`; pass the resolved value to telemetry and preview rather than independently reading build metadata. `e2e/version_test.go` checks the linker stamp reaches telemetry preview.
- **Cache:** `~/.cache/agent-marketplace/<registry-hash>/<ref>/` — disposable; deleting it has no effect on aggregate ownership
- **Durable state:** `$XDG_STATE_HOME/agent-marketplace` (default `~/.local/state/agent-marketplace`) — journaled Devin/Codex aggregate-ownership records and mandatory safety backups (`internal/ownership`)
- **Materialization:** `harness-compilation` for multi-harness compilation (Factory, Claude, Cursor, Windsurf, Gemini, Devin, and Codex). All seven are bidirectional compile/ingest/convert targets; the authoritative registry is `pkg/harness-compilation/target`
- **Workspace:** the committed `go.work` couples the root module and the `e2e` module, so bumping dependencies in one module never requires `go mod tidy` in the other for CI to pass. `e2e/go.mod` pins are hygiene: refresh every module with `make tidy` (module-mode tidy of each), or realign them to the workspace with `go work sync`.

### Known Gaps

- No resource dependency mechanism (tags and collections are partial workarounds)
- Locking covers Git registries plus exact v2 Marketplace records, writable-layer generation and update, online transient acquisition for an intentionally absent organization lock, and GC from local reachability without organization-lock acquisition or writes; offline transient acquisition, hosted rebind, and dynamic-selector snapshots remain unimplemented

### Local validation workflow

- During iteration, run the affected package/test first: `make test TEST_PACKAGES=./internal/resolver TEST_FLAGS="-run TestResolve -count=1"`. Select packages and consumer regressions explicitly; a scoped result is not full-suite validation.
- `make test` with no `TEST_PACKAGES` runs short-mode (`-short`) tests for the root-module packages changed since the merge base with `origin/main`, covering committed, staged, unstaged, and untracked work via `tools/test-local`. An explicit `TEST_PACKAGES` (including `./...`) overrides selection, `TEST_FLAGS` still apply, and `TEST_LOCAL_BASE` changes the merge-base ref. A rename selects both locations; root Markdown, `docs/**/*.md`, recognized OpenSpec/CI/deployment metadata, and select root metadata outside Go packages are ignored for Go test selection, while a deleted package, shared build/module file, or any other path unmappable to a root-module Go package falls back to `./...`; e2e-module changes are never selected. Reverse dependencies are not computed; `make test-full`, `make test-witnessed`, or CI's witnessed run covers them. When nothing changed, `make test` reports an empty selection and runs nothing.
- `make test-fast` is an alias of `make test`; `make test-full` preserves the previous full behavior (`./...`, no `-short`). `make validate-fast` is an alias of `make validate`: short-mode selected tests, non-mutating CI lint, and the build with two concurrent jobs. Short mode reduces existing stress repetitions and skips the existing cross-compilation test; it is not the full gate.
- `make validate` runs the same short-mode selected tests, non-mutating CI lint, and the build concurrently, retaining normal Go test caching. `TEST_PACKAGES`, `TEST_FLAGS`, `TEST_SHORT`, and `TEST_LOCAL_BASE` apply to the test target. Use `-count=1` when a fresh execution is needed, not reflexively on every unchanged package.
- `make validate-full` runs all root tests freshly (`-count=1`) at full stress counts, lint, build, the combined host E2E/configuration-compatibility gate, shellcheck, and TUI QA. It resets scoped test flags/packages so a prior narrow selection cannot silently weaken the full gate. It does not run vendor-native probes or frontend/container checks; those remain separate domain-specific/CI gates.
- `make validate-e2e` runs E2E and all configuration-compatibility scenarios in one process, avoiding repeated boundary tests and builds. Do not also run `harness-compat-check` or `harness-compat-config` after that successful combined gate unless debugging those gates themselves.
- The pre-v4 success `upgrade-gate` and its CI job are retired. Launch acceptance witnesses unsupported/versionless roots in every consumer layer and explicit non-v4 included fragments in the normal Marketplace E2E gate. Required witnesses must execute and pass: skips, failures, or nonexecution fail the gate. Keep independent exact-lock/source-revision, handwritten aggregate/owned-entry, local/relative-directory scope, multiple-ref, and collision regressions with supported v4 consumer fixtures; no pre-v4 executable is required.
- `AM_E2E_RELEASED_BINARY=/path/to/verified-v4.1.4-linux make released-client-gate` retains checksum-verified stable v4.1.4 interoperability in isolated containers: teammates with separate HOMEs/checkouts share one v4-configured Git-locked repository, the candidate runs `sync`, `install`, `update`, and `remove`, and the pinned release must still pass `sync --locked` and `sync --check` after each step. Healthy committed lock bytes stay identical through plain candidate sync and released locked/check reads. Native-hook source-layout verification and owned hook upgrade/deselection remain covered. Keep the existing architecture checksums in `e2e/released_client_compat_test.go` and CI for this launch. Change a shared config or lock format by shipping read support before switching writes. Use `AM_E2E_RELEASED_IMAGE` with an already-cached image containing git when Docker Hub is unavailable. Required scenarios run through `tools/witness-check -require`; skipped, failed, or unexecuted scenarios fail. The shared `e2e/historical_binary_test.go` runner remains; released binaries use fixed `/etc` organization paths, so never run them directly on the host. This dedicated gate is separate from `make validate-e2e`.
- `e2e`-tagged builds honour `AGENT_MARKETPLACE_E2E_CRASH_AT=<stage>:<index>` (for example `after-publish:0`), which kills the process at that `dependencytxn` fault point the way SIGKILL would; production builds have no hook. Crash-recovery tests interrupt supported v4 journaled mutations and prove read-only commands refuse pending work without replay while recovery-capable commands complete its already-admitted write set. Recovery then validates fresh roots/includes before any new work, including a subsequent unsupported-format refusal.
- The CI `E2E Gates` job remains the single aggregate status for Marketplace E2E and released-client gates, like `TUI QA Driver` for TUI QA. After successful planning, both jobs must succeed when selected, or both intentionally skip when unselected. Failed/cancelled planning, unexpected skips, missing executions, or failed witnesses fail the aggregate; no retired upgrade job is required. Require `E2E Gates` on `main` rather than the individual gate jobs.
- `CGO_ENABLED=0 go -C e2e test -run '^TestMarketplacePublicGate$' -count=1 .` exercises the actual Marketplace `serve` process with a v4 client, every materialized resource kind, a collection, and offline reuse after shutdown, alongside in-process v4 consumer and revocation controls.
- `VALIDATE_JOBS` bounds concurrent make tasks (default 2). `E2E_PARALLEL` bounds E2E parallel tests (default 4); an explicit Go `-parallel` overrides the runner default, while the declarative table retains its own `E2E_PARALLEL` cap. Tests that mutate process-global environment/cwd remain serial; do not blanket-add `t.Parallel()`.
- Run focused checks after edits and `make validate` plus the touched domain gates once before handoff; CI owns the full root suite (see Build, test, and verification above). Do not run mutating `make lint` concurrently with readers; validation uses `ci-lint` instead. TUI package-manager preflight resolves pnpm from `tools/tui-qa`, where its exact version is pinned.

### Effective config loading

Production consumers use `pipeline.LoadEffective` with an explicit `ConfigLoadOnline`, `ConfigLoadStrict`, or `ConfigLoadCached` mode. Strict ignores transient evidence; cached reuses verified invocation evidence without acquiring it. `ResolveOptions.ConfigLoadMode` defaults to online, but offline and cached-only catalog resolution force strict loading. Cobra initializes the shared cache in its root pre-run; standalone TUI lifecycle entry points retain their own initialization. `TestRawEffectiveLoaderBoundary` confines the raw config loader to `internal/config` and the pipeline loader implementation. Telemetry/init retain the offline organization policy reader.

### File Layout

```
agent-marketplace/
├── AGENTS.md          ← project context and agent workflow
├── CODING_STANDARDS.md ← code-change requirements
├── go.mod
├── docs/              ← internal design notes (not published)
│   └── plans/                    ← phased implementation plans
├── site/              ← VitePress landing page (/) and public docs (site/docs → /docs/)
├── web/               ← Nuxt Marketplace UI (self-hosted at /)
├── e2e/              ← declarative E2E test suite (required Ubuntu CI; Docker locally)
├── internal/
│   ├── config/        ← v4 consumer model, loader, merge, editor, runtime adapter
│   ├── catalog/       ← wraps harness-compilation IngestBundle
│   ├── resolver/      ← resource selection logic
│   ├── materialize/   ← harness-compilation materialization engine
│   ├── ownership/     ← durable, journaled ownership of shared aggregate files
│   ├── harness/       ← install roots, per-target aggregate files and capabilities
│   ├── devincli/      ← non-blocking Devin CLI version detection for doctor
│   ├── collection/    ← collection discovery and expansion
│   ├── tui/           ← Bubble Tea interactive UI
│   └── cache/         ← registry clone/pull cache management
├── cmd/agent-marketplace/  ← Cobra CLI subcommands
└── skills/            ← self-hosted skills for this repo
```

Full design rationale is in `docs/plans/2026-04-24-harness-compilation-integration.md` (harness-compilation integration design).

### Admin discovery preview

The admin panel can preview acquisition and discovery for a registry draft before it is applied: `POST /api/v1/admin/previews` records a `managed_previews` row plus a `managed-preview` job in one transaction, and `managedWorkerLoop` runs the same `acquire.Clone` + `ingest.BuildRegistryRevision` path as a refresh without ever publishing, advancing heads, or recording attempts. Results are per-root resource lists, a diff against `CurrentRegistryPublication` for existing registries, and `ingest.Revision.Diagnostics`; failures store a stable code plus a sanitized message (never git stderr). Previews are rate limited in `adminGuard` and pruned after 24h. The UI panel (`web/app/components/AdminPreviewPanel.vue`) binds a result to the exact draft JSON it was requested for; the Review step warns but never blocks Apply. Design context: `docs/plans/2026-09-18-marketplace-admin-interface-design.md` §4 and §8.

### v4 TUI selection workflow

V4 TUI mutations use the v4 layer editors, including staged config writes with the original declaring path so includes and relative directories resolve against the real root. Unsupported roots/includes gate configuration-dependent actions without a migration modal, conversion offer, or legacy editor fallback. The real-terminal QA journeys `v4-directory-global-toggle.spec.ts` and `v4-directory-error-visibility.spec.ts` cover the directory-source global toggle and narrow error recovery flow.
