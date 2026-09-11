# Harness Compilation Library

A Go library that converts AI-agent configuration resources from a canonical source format into vendor-specific layouts. It has seven conversion targets: Claude Code, Factory Droid, Cursor, Windsurf, Gemini CLI, Devin CLI, and Codex. Every one of them has both a compiler and an ingester, so each is valid as a `--from` source and a `--to` destination. The library never selects targets for a caller.

## Design

See [docs/plans/2026-04-23-harness-compilation-design.md](./docs/plans/2026-04-23-harness-compilation-design.md) for the full architecture and public API.

## Quick Start

```bash
make build   # compile all packages
make test    # run tests
make lint    # vet and lint
```

## How to Consume This Library

### The only import you need

```go
import hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
```

The root package owns registration of every built-in compiler and ingester, so
this single import is sufficient — no blank imports of `compile/<target>` or
`ingest/<target>` are needed or supported as part of the public contract. Call
`hcc.ValidateTargetRegistrations()` at startup if you want a fail-fast wiring
check.

### Per-resource loaders

```go
skill, err     := hcc.LoadSkill(src, "skills/my-skill")       // *hcc.Skill
command, err   := hcc.LoadCommand(src, "commands/my-cmd.md")  // *hcc.Command
subagent, err  := hcc.LoadSubagent(src, "agents/my-agent.md") // *hcc.Subagent
hooks, diags, err := hcc.LoadHooks(src, "hooks/hooks.json")   // []*hcc.Hook
servers, err   := hcc.LoadMcpServers(src, ".mcp.json")        // []*hcc.McpServer
```

`LoadHooks` also returns diagnostics because flattening `hooks.json` is lossy
for non-command hook types (`http`, `prompt`, `agent`): those produce warnings
instead of an error, and the library never drops them silently.

### Per-skill compilation (agent-marketplace pattern)

```go
// For each registry skill × enabled harness target:
skill, _ := hcc.LoadSkill(registryFS, skillDir)
result, _ := hcc.Compile(skill, hcc.TargetFactory)
report, _ := hcc.Materialize(result, "/path/to/.factory/skills",
    hcc.WithWriteMode(hcc.WriteModeSync))
```

### Bundle compilation (entire Claude-plugin directory)

```go
result, _ := hcc.CompileBundle(os.DirFS("my-plugin"), hcc.TargetCursor)
report, err := hcc.Materialize(result, "/output/dir",
    hcc.WithWriteMode(hcc.WriteModeOverwrite))
```

### Bundle error contract

Suppression is **per resource**, never bundle-wide. A resource that emits an
error diagnostic contributes no artifacts — not its own files, and not its
fragments of a shared aggregate such as `hooks.v1.json`, `mcp_config.json`, or
`plugin.json`. Every other resource is compiled and published normally, so one
unrepresentable tool restriction can never zero a whole materialization.

The run is still a failure. `Result.HasErrors()` reports it, `Result.Skipped`
names the resources that lost their output (identity and diagnostic codes only,
never resource content), and `Materialize` writes the valid artifacts while
returning a `WriteReport` with `Partial` set **and** an error wrapping
`ErrPartialResult`:

```go
report, err := hcc.Materialize(result, "/output/dir")
if report != nil {
    print(report) // what actually reached disk, partial or not
}
if errors.Is(err, hcc.ErrPartialResult) {
    return err // valid resources were written; exit non-zero
}
if err != nil {
    return err // non-partial failure; earlier renames or removals may have left partial disk changes
}
```

A partial result never prunes (`WriteReport.PruneSuppressed`): a resource skipped
this run looks exactly like a removed one to a stale-file scan, so pruning would
delete output the previous clean run wrote.

The only error codes not attributable to one resource are the bundle artifact-path
conflicts; they suppress both named producers as whole resources. See
[`diag/codes.md`](diag/codes.md#what-an-error-suppresses).

### CLI

```bash
# Install
go build -o hcc ./cmd/hcc

# Compile a plugin to Claude format
hcc --from ./my-plugin --target claude --out ./output

# Sync mode (overwrite + prune stale files)
hcc --from ./my-plugin --target factory --out ./output --sync

# Dry run
hcc --from ./my-plugin --target cursor --out ./output --dry-run
```

### Targets

| Target | Constant | Install root |
|---|---|---|
| Claude Code | `TargetClaude` | Plugin directory (sibling of `.claude-plugin/`) |
| Factory Droid | `TargetFactory` | Plugin directory (sibling of `.factory-plugin/`) |
| Cursor | `TargetCursor` | Plugin directory (sibling of `.cursor-plugin/`) |
| Windsurf | `TargetWindsurf` | Workspace root (`artifacts live under .windsurf/`) |
| Gemini CLI | `TargetGemini` | Extension directory (`gemini-extension.json` at root) |
| Devin CLI | `TargetDevin` | Project `.devin/`; user `~/.config/devin/` |
| Codex | `TargetCodex` | Partitioned: `.agents/` skills plus `.codex/` configuration; the consumer resolves scope and `CODEX_HOME` |

The authoritative declaration of every target — its identity, label, both roots,
and its compiler/ingester/conversion/UI capabilities — is
[`target/target.go`](target/target.go). Consume it rather than hardcoding a
target list; `ValidateTargetRegistrations()` checks the registry and the actual
compiler/ingester registrations against each other in both directions.

Codex, Devin, and Claude register a validated, scope-neutral topology
descriptor in [`target/native_layout.go`](target/native_layout.go): the
target's partitions, per-kind resource path patterns, and the shared
aggregate file each kind merges into per scope. A scope-variant aggregate
declares a different file per scope, like Devin's project `hooks.v1.json`
versus the `hooks` member of user `config.json`.
`ValidateTargetRegistrations()` validates every declared layout at the same
boundary, and rejects a partitioned-root target that registers none.

Devin-specific notes:

- Scope matters. `WithScope` selects project or user scope (project is the
  default). Hooks compile to `hooks.v1.json` at project scope and to the `hooks`
  member of `config.json` at user scope; `mcp_config.json` is the same relative
  path in both.
- A Devin command compiles to a skill directory (`skills/<name>/SKILL.md`)
  carrying `x-agent-marketplace-kind: command`, which the Devin ingester reads
  back as command provenance. A skill and a command with the same name therefore
  collide on one path and are reported as a bundle conflict rather than one
  silently winning.
- Ingestion reads exactly one explicit root. A workspace `.devin` directory is
  autodetected and preferred over every compatibility layout; a direct user root
  (`skills/`, `agents/`, `config.json`, `mcp_config.json` at top level) is never
  autodetected and must be selected with an explicit source harness. The
  ingester never walks ancestors or nested `.devin` directories.
- Round-trip equality is semantic, not byte-for-byte; every irreversible case is
  a declared entry in the loss matrix.
- Rules are out of scope for this target and produce a stable
  `rule.devin.unsupported` warning: no artifact, run still clean. That is a
  scope decision, not a Devin limit — 3000.3.27 does discover
  `.devin/rules/<name>.md` even though `devin rules paths` does not advertise
  it. See `diag/codes.md`.

Codex-specific notes:

- The target emits partitioned paths from one compilation result: skills and
  converted commands at `.agents/skills/<name>/SKILL.md`; subagents at
  `.codex/agents/<name>.toml`; hooks at `.codex/hooks.json`; and MCP servers as
  `[mcp_servers.<name>]` tables in `.codex/config.toml`. Commands carry
  `x-agent-marketplace-kind: command` so the ingester restores command
  provenance.
- The ingester detects a `.codex` workspace and then reads sibling
  `.agents/skills`. `.agents/skills` by itself is cross-agent and is never
  autodetected as Codex; callers may still select `TargetCodex` explicitly.
- Hook and MCP artifacts set `ExternalReconciliationRequired`: they are shared
  native files, so consumers must reconcile entries rather than overwrite the
  file. Rules and `AGENTS.md` generation are out of scope. Deprecated
  `.codex/prompts/*.md` files are ingest-only commands and are never emitted.
- The supported native schema was verified on 2026-08-19 against the official
  Codex documentation for [skills](https://developers.openai.com/codex/skills),
  [subagents](https://developers.openai.com/codex/subagents),
  [hooks](https://developers.openai.com/codex/hooks), and
  [MCP](https://developers.openai.com/codex/mcp), plus the
  [config reference](https://developers.openai.com/codex/config-file/config-reference)
  for `oauth_resource`. The exact fixture outcomes and
  losses are pinned by the Codex compiler/ingester tests and the 7×7 matrix; see
  [`field-matrix.md`](field-matrix.md) and [`diag/codes.md`](diag/codes.md).

### Materialize options

- `WithWriteMode(WriteModeFailIfExists)` — default; errors if output files already exist
- `WithWriteMode(WriteModeOverwrite)` — overwrites existing files
- `WithWriteMode(WriteModeSync)` — overwrite + prune stale files
- `WithPruneStale(true)` — remove files in outDir absent from the result
- `WithDryRun()` — report planned writes without performing I/O
- `WithPluginRoot(path)` — set the plugin root for `${CLAUDE_PLUGIN_ROOT}` expansion

### Golden-file testing

```bash
# Run golden tests
go test -run TestGolden ./...

# Refresh golden files
UPDATE_GOLDEN=1 go test -run TestGolden ./...
```

## Status

v0.x — 7×7 translation matrix and core API implemented. See design doc for deferred items.
