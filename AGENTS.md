# Agent Marketplace — agent-marketplace CLI

This project builds the **agent-marketplace** CLI: a Go binary that syncs shared AI agent resources from Git-based or local registries and materializes them into native harness layouts. Resources include skills, commands, subagents, hooks, and MCP servers, following the [agentskills.io spec](https://agentskills.io/specification).

**Code changes:** Read [CODING_STANDARDS.md](./CODING_STANDARDS.md) before modifying code. It defines platform, design, testing (including E2E), observability, logging, and performance requirements.

## Core Concept

- **Registry repos** contain agent resources (skill directories with `SKILL.md`, commands, subagents, hooks, MCP server definitions). Content is ingested through `harness-compilation` so resources can be authored in any harness layout and compiled into any target harness.
- The CLI reads **layered configs**: external organization (`/etc/agent-marketplace/config.yaml`), user-personal (`~/.agents/config.yaml`), repo committed (`.agents/config.yaml`), and personal-local (`.agents/config.local.yaml`, gitignored). Every layer supports recursive local-file `include`; included fragments merge below their containing file, while CLI edits remain rooted in the selected config file. V4 is the canonical source/resource schema; v2 remains the legacy schema and v3 remains an accepted compatibility alias. Regular legacy roots reject v1 configs (no `apiVersion`), included fragments and local roots may omit it, and `agent-marketplace migrate` targets v4 by default. Proactive CLI/TUI migration is delayed until resource editors support v4; legacy roots continue through the v2/v3 reader unless users explicitly run `agent-marketplace migrate`. Exact legacy Marketplace pins migrate only with matching verified cache evidence; otherwise the legacy read continues unchanged. `init` creates editable v2 user configs. See [the precedence reference](./docs/PRECEDENCE.md) for include syntax, validation, migration consent, and merge order.
- Target harnesses are resolved from the project config when it declares top-level `harnesses:`, then from the user-personal config, with organization as fallback. Registry entries may further narrow their targets.
- Each registry entry can declare `harnesses:` to narrow its targets; the entry's list is intersected with the effective target set.
- Catalog-only commands (`list`) work without a configured target set and warn; target-dependent commands (`sync`) require a configured set and exit with an error if missing.
- Resources are **materialized** into native harness layouts (e.g. `.factory/skills/`, `.claude/skills/`, `.cursor/rules/`, `.gemini/skills/`, `.windsurf/`, `.devin/skills/`, `.agents/skills/`, `.codex/agents/`) via `harness-compilation`. In the default mode, skills and target-declared skill-directory kinds use symlinks to the registry cache; file mode writes regular files/trees for every target through the generic Agent Marketplace materializer. Other artifacts are compiled per target. Config is the source of truth; generated entries are ephemeral.
- **Claude and Gemini native aggregate installation is distinct from plugin compilation.** Claude MCP servers are installed at `<repo>/.mcp.json` or `~/.claude.json#mcpServers`; Gemini MCP servers and hooks share the scope's `.gemini/settings.json`. Compiler plugin formats remain compatible, and previously generated plugin-style destinations are left untouched rather than moved or pruned implicitly. Extra native roots expose only explicitly mapped aggregate files, never a broad HOME/repository prune walk. Native merges preserve unrelated settings, comments, permissions, and handwritten entries; unmanaged MCP collisions and malformed configurations fail closed. Gemini MCP entries use durable ownership proof instead of in-file markers, which its native schema rejects; hook markers retain their existing policy, and MCP/hook updates share one journaled mutation. Unproven legacy MCP markers are preserved and reported, never automatically adopted or stripped. Gemini hooks use native matcher groups, with legacy flat input retained for ingestion. Incomplete compilation retains stale managed entries while still allowing valid siblings to materialize. Machine backups cover the external Claude configuration through an explicit archive mapping, and new archives are owner-readable only.
- Remote Git requirements are resolved into generated, **layer-owned lockfiles**: organization `/etc/agent-marketplace/lock.yaml`, user `~/.agents/lock.yaml`, repository `.agents/lock.yaml`, and personal-local `.agents/lock.local.yaml`. The repository lock is committed; the personal-local lock is ignored. An absent organization lock intentionally leaves that external layer mutable for consumer inspection or refresh without creating the lock; a present organization lock is exact and fail-closed. Plain local-directory registries remain live and unlocked.
- The lockfile schema is versioned: v1 remains Git-only, while v2 adds exact Marketplace endpoint/registry/resource/selection records used for offline legacy migration and a read-only runtime adapter that hydrates explicit v4 Marketplace requests. Broader hosted acquisition, update, dynamic-selector, and editor integration remains absent.
- **Devin is a harness target.** Roots are `<repo>/.devin` and `~/.config/devin`. Skills and converted commands are directory symlinks at `skills/<name>/`, each containing `SKILL.md` (a command carries `x-agent-marketplace-kind: command`), subagents are file symlinks at `agents/<name>.md`, hooks go to `hooks.v1.json` (project) or the `hooks` member of `config.json` (user), and MCP servers go to `mcp_config.json` in both scopes. Hook event arrays contain native matcher groups with nested `hooks` arrays; matchers belong to the group and payload/type/timeout fields to its inner hook. Legacy flat input remains ingestible, and sync upgrades only demonstrably owned legacy entries while preserving handwritten groups and unrelated settings. Rules are out of scope for this target (a scope decision, not a Devin limit): nothing is emitted and a `rule.devin.unsupported` warning is reported without failing the run. These symlinks are the default; `materialization.mode: file` instead writes regular skill/command trees and subagent files through the generic materializer. Installation mode is distribution policy, not a compiler capability, and does not change aggregate ownership reconciliation. Select it through the TUI harness modal (`H`) or by adding `devin` to the applicable `harnesses:` list. Devin is also a conversion target (`convert --from/--to devin`, `source_harness: devin`), and autodetection prefers a `.devin` workspace marker over compatibility layouts.
- **Devin's shared aggregate files are ownership-reconciled, not marker-merged.** No `_managed_by` marker is ever written into a Devin file. Ownership lives in durable journaled state under `$XDG_STATE_HOME/agent-marketplace` (default `~/.local/state/agent-marketplace`), outside the repo and outside the cache, so deleting the cache has no ownership effect. Missing or corrupt ownership state fails closed: every existing entry becomes unmanaged, so sync preserves everything, refuses collisions, and prunes nothing. Handwritten entries, comments, key order, and hook order survive; handwritten hooks precede generated ones. Mandatory raw-byte safety backups are taken even when configured backups are disabled, retaining the five newest per file. Aggregate updates are atomic per file: a malformed `mcp_config.json` blocks only that file.
- **Codex is a partitioned-root harness target.** Repo destinations are `<repo>/.agents` for skills/converted commands and `<repo>/.codex` for subagents, hooks, and MCP servers. Machine destinations are `~/.agents` plus `$CODEX_HOME` (which must be absolute) or `~/.codex`; `CODEX_HOME` never relocates skills. In default symlink mode, skills and converted commands are directory symlinks at `skills/<name>/`; file mode writes regular directory trees there. Each contains `SKILL.md`, with commands marked `x-agent-marketplace-kind: command`; subagents are TOML files at `agents/<name>.toml`; hooks and MCP servers are owned entries in `hooks.json` and `config.toml`. Select it through `H` or `harnesses: [codex]`. It is bidirectional (`convert --from/--to codex`, `source_harness: codex`), but `convert --to codex` requires a destination without pre-existing `.codex/hooks.json` or `.codex/config.toml`. Autodetection requires `.codex` rather than claiming the cross-agent `.agents/skills` layout. Agent Marketplace never installs, requires, or probes a Codex executable.
- **Codex shared configuration is ownership-reconciled and trust-neutral.** `.codex/hooks.json` and `.codex/config.toml` preserve handwritten entries and formatting, use the same durable ownership authority and mandatory backup policy as Devin, and fail closed on missing/corrupt state or unpatchable native forms. An actual repo aggregate mutation emits `materialize.codex.trust-review`; check-only drift emits `materialize.codex.trust-review-pending`. The application never edits Codex trust state or adds Codex output paths to `.gitignore`. Rules, `AGENTS.md` generation, and emitted legacy `.codex/prompts` are out of scope.

## Key Design Decisions

| Decision | Why |
|---|---|
| CLI sync, not git submodules | No submodule friction; marketplace UX for browsing/installing |
| Harness-compilation materialization | Native per-harness outputs instead of one-size-fits-all symlinks |
| v2/v3 organization config with project target harnesses | `/etc/agent-marketplace/config.yaml` is the managed external org layer; v2 is canonical and v3 is a compatibility alias; project `harnesses` wins, then user, then org; per-entry `harnesses` narrows the set |
| Multi-resource kinds (skill/command/subagent/hook/mcp) | Full IR fidelity; same selection/exclude UX across all kinds |
| Tags in `metadata.tags` | Uses the spec's extension point; no deviation |
| Attribute inheritance (specific wins) | Per-registry-entry overrides top-level defaults |
| Name collisions across registries resolve deterministically with a warning | Keeps one materializable winner visible; required policy, config precedence, normalized URL, and stable config order determine the winner; labels include `@ref` when URL/path alone is ambiguous |
| Same-URL overlapping paths merge deterministically | Same-URL+ref paths that are component-wise parent/child describe one physical source; required policy wins first, then the deepest path wins equal-policy overlaps, while different scopes remain complementary |
| Same-URL different-path collisions emit warning | Non-overlapping paths remain visible; keep all occurrences for visibility but warn that only one materializes per scope |
| Nested collections preserve branch-local ancestry | Shared DAG includes are valid and expand per branch; only an ancestor repeated on the same include path is a cycle |
| `required:true` registries immune to all excludes | Enforces org-wide security standards |
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

## CLI Commands

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
- `agent-marketplace migrate [--to v4] [--scope user|repo|local|all] [--dry-run]` — convert representable writable legacy configs to v4 transactionally; `--to v2 --harnesses <targets>` remains available for the legacy structural-remediation flow. Git/directory-only conversion leaves locks unchanged; exact Marketplace pin migration (backed by verified local cache evidence) creates or upgrades the owning layer lock to v2 and publishes config+lock in one journaled transaction. Migration never edits organization config and never adds `devin` or `codex` implicitly
- `agent-marketplace convert --from <harness> --to <harness> --out <dir> <path>` — one-shot layout conversion; `claude`, `factory`, `cursor`, `windsurf`, `gemini`, `devin`, and `codex` are valid in both directions
- `agent-marketplace doctor` — show local harness/root diagnostics and non-blocking Devin CLI compatibility guidance
- `agent-marketplace doctor --repair` — preview or interactively repair Devin and Codex shared-aggregate ownership state
- `agent-marketplace doctor --repair --yes` — adopt exact current desired matches without prompting; destructive orphan removal and conflict replacement still require interactive confirmation
- `agent-marketplace tui` — launch interactive browser (default on bare TTY invocation); `H` opens the harness modal. Normative TUI behavior is in [`docs/tui-product-contract.md`](./docs/tui-product-contract.md)

## Tech Stack

- **Language:** Go (static binary, no runtime deps)
- **Distribution:** package managers and GitHub releases
- **Build version:** retain `-ldflags "-X main.version=<release>"` compatibility for MSC packaging. CLI startup resolves that stamp before falling back to Go main-module metadata or `dev`; pass the resolved value to telemetry and preview rather than independently reading build metadata. `e2e/version_test.go` checks the linker stamp reaches telemetry preview.
- **Cache:** `~/.cache/agent-marketplace/<registry-hash>/<ref>/` — disposable; deleting it has no effect on aggregate ownership
- **Durable state:** `$XDG_STATE_HOME/agent-marketplace` (default `~/.local/state/agent-marketplace`) — journaled Devin/Codex aggregate-ownership records and mandatory safety backups (`internal/ownership`)
- **Materialization:** `harness-compilation` for multi-harness compilation (Factory, Claude, Cursor, Windsurf, Gemini, Devin, and Codex). All seven are bidirectional compile/ingest/convert targets; the authoritative registry is `pkg/harness-compilation/target`
- **Workspace:** the committed `go.work` couples the root module and the `e2e` module, so bumping dependencies in one module never requires `go mod tidy` in the other for CI to pass. `e2e/go.mod` pins are hygiene: refresh every module with `make tidy` (module-mode tidy of each), or realign them to the workspace with `go work sync`.

## Known Gaps

- No resource dependency mechanism (tags and collections are partial workarounds)
- Locking covers Git registries plus the exact v2 Marketplace slice (migration-generated endpoint/registry/resource/selection records and the read-only lock-backed adapter); hosted Marketplace lock acquisition, update, rebind, dynamic-selector snapshots, editor writes, and GC integration remain unimplemented

Run direct real-terminal test typechecking with `make tui-qa-check` and all eight deterministic user journeys with `make tui-qa`. TUI QA requires no organization-specific infrastructure; do not place machine-specific values in tracked commands, reports, or fixtures.

## Command test isolation

Command tests in `cmd/agent-marketplace` are ordinary top-level `Test<Name>(t *testing.T)` functions, each run in its own sandboxed child process. Every such test opens with exactly `if isolateCommandTest(t) { return }` as its first statement; in the parent that call marks the test parallel, spawns the child, relays its result, and returns true. In the child it returns false and the body runs. Matrices stay together in one child. `TestCommandIsolationGuard` parses the package's test sources and fails on any top-level test that does not start with the guard, except tests in the runner/pure-unit files (`inProcessTestFiles`) and the explicit `inProcessTests` allowlist in `command_integration_test.go`.

Select a test with `go test ./cmd/agent-marketplace -run '^TestBackupCreatedOnInstall$' -count=1`; append `/pattern` for nested subtests. `-run` alternations, `-skip`, `-count`, and `-shuffle` behave as for any Go test; the parent translates the filter into an exact child selector. The default child slot count is `min(max(GOMAXPROCS, 4), 12)`; `-parallel=1` serializes children. Use `-args -command-tests-in-process` for the serial baseline. Coverage builds automatically use that path so counters reach the report.

Child HOME, XDG roots, CODEX_HOME, cwd, organization config, and Git environment are sandboxed. Fixtures that replace HOME and expect Codex beneath it must also set CODEX_HOME. Diagnostic-grouping fixtures use local directories when asserting registry-relative source locations; exact Git snapshots deliberately preserve absolute source paths. Runner and guard checks are `go test ./cmd/agent-marketplace -run '^TestCommand(Process|IsolationGuard)' -count=1`; include `-race` and `-cover` when changing the runner.

## E2E Test Suite

The `e2e/` directory contains E2E tests that validate end-to-end CLI behavior by running the built binary in isolated temp-directory sandboxes. They run directly in CI through `make marketplace-gate`; local runs can optionally use a Docker container for host isolation.

There are **two types** of E2E tests:

1. **Declarative (table-driven)** — defined as `TestCase` structs in `e2e/table_test.go`. These are the preferred format for tests that follow a standard setup/run/assert pattern: write config, run a command, check output and filesystem. Use these when the test fits the `Steps` model (actions + assertions on a single workspace).
2. **Standalone** — individual `func TestXxx(t *testing.T)` functions in separate files (e.g. `e2e/install_test.go`). Use these when the test requires non-standard setup that the declarative model doesn't support, such as running commands from a non-default working directory or skipping git repo initialization.

### Running

```bash
make e2e                  # runs inside Docker (isolated from host env)
E2E_SKIP_DOCKER=1 make e2e  # for CI runners that cannot spawn Docker
E2E_PARALLEL=8 make e2e  # control concurrency (default 4, set to 1 for serial)
```

### Architecture

- **Process-based sandbox**: each test gets its own temp root with `home/`, `workspace/`, `repos/`, `etc/` directories. The binary runs as a subprocess with `HOME` overridden so it never touches the host filesystem.
- **Path mapping**: test table uses container-style paths (`/root/...`, `/workspace/...`) which are automatically resolved to the temp sandbox by `Env.resolvePath()`.
- **Declarative test cases**: defined as `TestCase` structs in `e2e/table_test.go` with `Registries`, config layers (`GlobalCfg`/`RepoCfg`/`LocalCfg`/`OrgCfg`), and `Steps` (actions + assertions). `{{REG:name}}` placeholders are resolved to `file://` URLs of seeded bare git repos.

### When to add a test

Add a new `TestCase` to `testTable` in `e2e/table_test.go` when:
- Adding a new CLI command or flag that should be validated end-to-end
- Fixing a bug that involves config merging, materialization, precedence, or collision handling
- Changing resource selection/exclusion semantics (`required`, `default`, `tags`, `exclude`)

Add a standalone test function in a separate file when:
- The test needs non-standard setup (e.g. no git repo, custom working directory)
- The test uses `RunAMFrom` or other framework features not exposed through the `Steps` model

Use the structured fixture builders in `e2e/harness_test.go` (`Skill`, `Command`, `Subagent`, `MCPServer`) to define registry contents. Keep `Desc` fields as Given/When/Then descriptions.

## Performance & Benchmarking Policy

This project treats performance as **data-driven, not narrative-driven**. Optimizations and
refactors that touch hot paths must be accompanied by benchmarks; "it feels faster" is
not acceptable evidence.

**During every implementation cycle:**

1. **Spot a hot path → write a benchmark.** Any code that runs on a user-visible latency
   path (each keystroke, every `sync`, every `status`, every `Resolve`, every `DiscoverSkills`
   invocation) earns a `func Benchmark…(b *testing.B)` in the same package the first time
   it is modified. No silent hot paths.
2. **Measure before you change.** Capture a baseline with
   `go test -run '^$' -bench '^BenchmarkFoo$' -benchtime 2s -count 5 -benchmem | tee before.txt`
   *before* the optimization lands. Without a baseline the change is unverifiable.
3. **Measure after.** Re-run the same command into `after.txt` and diff with
   `benchstat before.txt after.txt`. Post the delta (ns/op, B/op, allocs/op) in the PR
   description or commit message; no delta ⇒ not merged.
4. **Regression guard.** The benchmark stays in the tree. Anyone who later regresses the
   hot path will see it in CI or when running `make bench` locally.
5. **Micro-benchmarks live next to the code.** Name the file `*_bench_test.go` so it is
   discoverable and can be excluded from `go test` wall-clock budgets when needed.

**Review the `efficient-go` skill** (`~/.factory/skills/efficient-go/SKILL.md`) for the
Three Rs method (Reduce / Reuse / Recycle allocations), pre-allocation patterns, and
`sync.Pool` guidance when proposing a performance change.

## Logging Policy

The CLI configures a process-wide `slog` logger before any subcommand runs
(see `cmd/agent-marketplace/main.go`). **Instrument new features and changes
with structured logs** via the standard `slog` default logger (`slog.Info`,
`slog.Debug`, `slog.Error`, ...) — at minimum log meaningful start/end,
decisions, and error paths.

- **Defaults live in code.** The `Default*` constants in
  `internal/config/config.go` are the single source of truth for the logging
  runtime (`internal/logging`). Keep them there.
- **Do not seed a `logging:` block into the generated config skeleton.** The
  skeleton carries only required global fields (`apiVersion`, `harnesses`);
  logging is opt-in override only. Users override via `~/.agents/config.yaml`
  (`logging:`) or `AGENT_MARKETPLACE_*` env vars. Precedence is
  **env var > config file > code default**.
- Full strategy and overridable keys: [`docs/logging.md`](./docs/logging.md).

## File Layout

```
agent-marketplace/
├── AGENTS.md          ← project context and agent workflow
├── CODING_STANDARDS.md ← code-change requirements
├── go.mod
├── docs/              ← internal design notes (not published)
│   ├── adoption-guide.md         ← v3 onboarding guide
│   ├── PRECEDENCE.md             ← selection & precedence reference
│   └── plans/                    ← phased implementation plans
├── site/              ← VitePress landing page (/) and public docs (site/docs → /docs/)
├── web/               ← Nuxt Marketplace UI (/marketplace/)
├── e2e/              ← declarative E2E test suite (required Ubuntu CI; Docker locally)
├── internal/
│   ├── config/        ← config model, loader, merge, editor, migration
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

## Landing the Plane (Session Completion)

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

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds

## Additive hook selection

Hook support materialization uses `catalog.Entry.SourceHarness` to exclude the primary definition and native aggregate siblings using target metadata; legacy Windsurf `hooks/` JSON definitions are not support files. `catalog.HookSupportFiles` remains unchanged for source-revision hashing, so supported lock manifests retain their historical superset of adjacent files. Current Windsurf ingestion and selection preserve source-array order independently of selector names. Fresh output follows that order, while shared-file reconciliation preserves surviving installed slots and comments; upgrading a legacy lock does not forcibly reorder an installed handler.

Hooks retain registry-local names but resolve additively across independent registry sources; repeated same-source selections deduplicate and competing refs at one location/scope retain precedence. The policy lives in `internal/resolver`, including reconciliation after collection expansion, and install-all consumes `HasAdditiveSources`. Non-hook collisions remain winner-based. Windsurf uses native `hooks.json` event arrays; old `hooks/` directories remain untouched and new support files use `hook-support/`. Source revision v3 enables corrected Windsurf ingestion, while existing v1/v2 locks retain their old inventory until explicit upgrade. `materialize.hook.support-conflict` refuses conflicting support files before aggregate mutation; blocked hooks retain installed entries while valid siblings can still materialize. See `docs/plans/2026-09-16-additive-hooks.md` for the design and verification. Focused CLI E2E: `go test ./e2e -run 'TestE2E/hooks/additive' -count=1`. Run E2E-based gates sequentially: `make e2e`, `make harness-compat-check`, and `make harness-compat-config` share the `e2e/agent-marketplace-e2e` binary, which each test process removes on exit.
