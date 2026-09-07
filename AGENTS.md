# Agent Marketplace — agent-marketplace CLI

This project builds the **agent-marketplace** CLI: a Go binary that syncs shared AI agent resources from Git-based or local registries and materializes them into native harness layouts. Resources include skills, commands, subagents, hooks, and MCP servers, following the [agentskills.io spec](https://agentskills.io/specification).

**Code changes:** Read [CODING_STANDARDS.md](./CODING_STANDARDS.md) before modifying code. It defines platform, design, testing (including E2E), observability, logging, and performance requirements.

## Core Concept

- **Registry repos** contain agent resources (skill directories with `SKILL.md`, commands, subagents, hooks, MCP server definitions). Content is ingested through `harness-compilation` so resources can be authored in any harness layout and compiled into any target harness.
- The CLI reads **layered configs**: external organization (`/etc/agent-marketplace/config.yaml`), user-personal (`~/.agents/config.yaml`), repo committed (`.agents/config.yaml`), and personal-local (`.agents/config.local.yaml`, gitignored). Every layer supports recursive local-file `include`; included fragments merge below their containing file, while CLI edits remain rooted in the selected config file. The canonical schema is v2; v3 remains an accepted compatibility alias. Regular roots reject v1 configs (no `apiVersion`), included fragments and local roots may omit it, and `agent-marketplace migrate` performs structural remediation without forcing a schema rewrite. See [the precedence reference](./docs/PRECEDENCE.md) for include syntax, validation, and merge order.
- Target harnesses are resolved from the project config when it declares top-level `harnesses:`, then from the user-personal config, with organization as fallback. Registry entries may further narrow their targets.
- Each registry entry can declare `harnesses:` to narrow its targets; the entry's list is intersected with the effective target set.
- Catalog-only commands (`list`) work without a configured target set and warn; target-dependent commands (`sync`) require a configured set and exit with an error if missing.
- Resources are **materialized** into native harness layouts (e.g. `.factory/skills/`, `.claude/skills/`, `.cursor/rules/`, `.gemini/skills/`, `.windsurf/`, `.devin/skills/`, `.agents/skills/`, `.codex/agents/`) via `harness-compilation`. In the default mode, skills and target-declared skill-directory kinds use symlinks to the registry cache; file mode writes regular files/trees for targets that support it. Other artifacts are compiled per target. Config is the source of truth; generated entries are ephemeral.
- **Devin is a harness target.** Roots are `<repo>/.devin` and `~/.config/devin`. Skills and converted commands are directory symlinks at `skills/<name>/`, each containing `SKILL.md` (a command carries `x-agent-marketplace-kind: command`), subagents are file symlinks at `agents/<name>.md`, hooks go to `hooks.v1.json` (project) or the `hooks` member of `config.json` (user), and MCP servers go to `mcp_config.json` in both scopes. Rules are out of scope for this target (a scope decision, not a Devin limit): nothing is emitted and a `rule.devin.unsupported` warning is reported without failing the run. Devin is symlink-only: `materialization.mode: file` is refused (`materialize.devin.symlink-required`) rather than degraded. Select it through the TUI harness modal (`H`) or by adding `devin` to the applicable `harnesses:` list. Devin is also a conversion target (`convert --from/--to devin`, `source_harness: devin`), and autodetection prefers a `.devin` workspace marker over compatibility layouts.
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
- `agent-marketplace install <resource>` / `install --tag <tag>` / `install --scope {user,repo,local}` — add resource to target config layer and sync
- `agent-marketplace install --kind command X` — disambiguate or select a non-skill resource
- `agent-marketplace install --all [--kind mcp] [--registry URL]` — install every discovered resource
- `agent-marketplace install <name> --from <url> [--ref <ref>] [--path <path>] [--kind <kind>] [--scope <scope>]` — install from an ad-hoc registry without pre-configuring it; without `<name>` installs all resources (bundle mode)
- `agent-marketplace remove <resource>` / `remove --scope {user,repo,local}` — polymorphic remove: deletes explicit selections, appends exclude for auto-selected/default resources; explicit scope selects the machine/repo/local occurrence and preserves other scopes; refuses required resources and machine removal from repo/local scope
- `agent-marketplace remove <name> --from <url> [--ref <ref>] [--path <path>] [--scope <scope>]` — remove a resource from a specific ad-hoc registry entry; without `<name>` removes the entire registry entry; `--path` narrows URL matching to an entry/subtree and `--scope` defaults to repo
- `agent-marketplace sync` — deterministic materialization based on merged config
- `agent-marketplace sync --check` — check drift without reconciling; exits 1 if out of sync
- `agent-marketplace update` / `update --bump` — pull latest for pinned refs
- `agent-marketplace status` — show resources, harnesses, targets, refs, cache freshness, source layer, drift, errors. Includes a `HARNESS TARGETS` table with capabilities and repo/machine roots
- `agent-marketplace migrate --harnesses <targets>` — remediate structural config issues while preserving v2 (v1 configs are rejected). Migration never adds `devin` or `codex` implicitly
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

## Known Gaps

- No resource dependency mechanism (tags and collections are partial workarounds)

Run direct real-terminal test typechecking with `make tui-qa-check` and all eight deterministic user journeys with `make tui-qa`. TUI QA requires no organization-specific infrastructure; do not place machine-specific values in tracked commands, reports, or fixtures.

## E2E Test Suite

The `e2e/` directory contains E2E tests that validate end-to-end CLI behavior by running the built binary in isolated temp-directory sandboxes. Registry fixtures are local bare Git repositories reached through `file://` URLs, so the full suite runs directly on Ubuntu in CI. Local runs use Docker by default for additional host isolation.

There are **two types** of E2E tests:

1. **Declarative (table-driven)** — defined as `TestCase` structs in `e2e/table_test.go`. These are the preferred format for tests that follow a standard setup/run/assert pattern: write config, run a command, check output and filesystem. Use these when the test fits the `Steps` model (actions + assertions on a single workspace).
2. **Standalone** — individual `func TestXxx(t *testing.T)` functions in separate files (e.g. `e2e/install_test.go`). Use these when the test requires non-standard setup that the declarative model doesn't support, such as running commands from a non-default working directory or skipping git repo initialization.

### Running

```bash
make e2e                    # run locally inside Docker
E2E_SKIP_DOCKER=1 make e2e  # run directly on the host, as CI does
E2E_PARALLEL=8 make e2e     # control concurrency (default 4, set to 1 for serial)
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
├── docs/
│   ├── adoption-guide.md         ← v3 onboarding guide
│   ├── PRECEDENCE.md             ← selection & precedence reference
│   └── plans/                    ← phased implementation plans
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

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
