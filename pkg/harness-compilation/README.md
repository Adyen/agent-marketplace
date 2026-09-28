# Harness compilation library

This Go library converts AI-agent configuration resources from a canonical source format (the Claude plugin layout) into the layout each harness expects. It has seven conversion targets: Claude Code, Factory Droid, Cursor, Windsurf, Gemini CLI, Devin CLI, and Codex. Each target registers both a compiler and an ingester, so each one works as a `--from` source and as a `--to` destination. The library never decides which targets a caller uses.

## Design

The full architecture and public API are described in [docs/plans/2026-04-23-harness-compilation-design.md](./docs/plans/2026-04-23-harness-compilation-design.md).

## Quick start

Run these from this directory:

```bash
make build   # compile all packages
make test    # run tests
make lint    # vet and lint
```

`make lint` also runs `go mod tidy` and `go fmt ./...`, so it can rewrite files. This directory has no `go.mod` of its own, so `go mod tidy` acts on the repository's root module. The target runs `golangci-lint` only when it is installed.

## How to consume this library

### The only import you need

```go
import hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
```

The root package registers every built-in compiler and ingester, so this one
import is enough. Blank imports of `compile/<target>` or `ingest/<target>` are
not needed and are not part of the public contract. To check the wiring at
startup and fail fast, call `hcc.ValidateTargetRegistrations()`.

### Per-resource loaders

```go
skill, err     := hcc.LoadSkill(src, "skills/my-skill")       // *hcc.Skill
command, err   := hcc.LoadCommand(src, "commands/my-cmd.md")  // *hcc.Command
subagent, err  := hcc.LoadSubagent(src, "agents/my-agent.md") // *hcc.Subagent
hooks, diags, err := hcc.LoadHooks(src, "hooks/hooks.json")   // []*hcc.Hook
servers, err   := hcc.LoadMcpServers(src, ".mcp.json")        // []*hcc.McpServer
```

`LoadHooks` also returns diagnostics. Flattening `hooks.json` loses information
for hook types other than `command` (`http`, `prompt`, `agent`), and each of
those produces a warning instead of an error. The library does not drop them
without reporting it.

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

Errors suppress output one resource at a time, never for the whole bundle. A
resource that emits an error diagnostic contributes no artifacts: none of its
own files, and none of its fragments of a shared aggregate file such as
`hooks.v1.json`, `mcp_config.json`, or `plugin.json`. Every other resource is
compiled and published as usual, so one tool restriction that a target cannot
represent does not empty the whole output.

The run still counts as a failure. `Result.HasErrors()` reports it, and
`Result.Skipped` names the resources that lost their output (identity and
diagnostic codes only, never resource content). `Materialize` writes the valid
artifacts and returns both a `WriteReport` with `Partial` set and an error
wrapping `ErrPartialResult`:

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

A partial result never prunes, and `WriteReport.PruneSuppressed` records that.
To a stale-file scan, a resource skipped in this run looks the same as a removed
one, so pruning would delete output that the previous clean run wrote.

Bundle artifact-path conflicts are the only error codes that cannot be traced to
one resource. They suppress both named producers as whole resources. See
[`diag/codes.md`](diag/codes.md#what-an-error-suppresses).

### CLI

`cmd/hcc` is a small command for local testing. It has two subcommands:
`compile` reads a Claude plugin directory and writes one target layout, and
`convert` reads any supported harness layout and writes another.

```bash
# Install
go build -o hcc ./cmd/hcc

# Compile a plugin to Claude format
hcc compile --from ./my-plugin --target claude --out ./output

# Sync mode (overwrite + prune stale files)
hcc compile --from ./my-plugin --target factory --out ./output --sync

# Dry run
hcc compile --from ./my-plugin --target cursor --out ./output --dry-run

# Convert a Cursor layout to Codex; --out defaults to <dir>-<target>
hcc convert --from cursor --to codex ./my-plugin

# Detect the source harness instead of passing --from
hcc convert --autodetect --to factory ./my-plugin
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

[`target/target.go`](target/target.go) is the authoritative declaration of
every target: its identity, label, both roots, and its compiler, ingester,
conversion, and UI capabilities. Read targets from it instead of hardcoding a
list. `ValidateTargetRegistrations()` checks the registry and the actual
compiler and ingester registrations against each other in both directions.

Codex, Devin, and Claude register a validated topology descriptor in
[`target/native_layout.go`](target/native_layout.go). The descriptor does not
depend on scope. It lists the target's partitions, the resource path pattern for
each kind, and the shared aggregate file each kind merges into at each scope.
An aggregate that varies by scope declares a different file per scope, such as
Devin's project `hooks.v1.json` and the `hooks` member of its user
`config.json`. `ValidateTargetRegistrations()` validates every declared layout
at the same point, and rejects a partitioned-root target that registers no
layout.

Devin-specific notes:

- Scope matters. `WithScope` selects project or user scope, and project is the
  default. Hooks compile to `hooks.v1.json` at project scope and to the `hooks`
  member of `config.json` at user scope. `mcp_config.json` has the same relative
  path in both scopes.
- A Devin command compiles to a skill directory (`skills/<name>/SKILL.md`)
  marked `x-agent-marketplace-kind: command`, and the Devin ingester reads that
  marker back to restore the resource as a command. A skill and a command with
  the same name therefore collide on one path. The library reports this as a
  bundle conflict instead of letting one of them win.
- Ingestion reads exactly one explicit root. A workspace `.devin` directory is
  autodetected and preferred over every compatibility layout. A direct user root
  (`skills/`, `agents/`, `config.json`, `mcp_config.json` at top level) is never
  autodetected; select it with an explicit source harness. The ingester never
  walks ancestors or nested `.devin` directories.
- A round trip preserves meaning, not bytes. Every case that cannot be reversed
  is declared in the loss matrix.
- Rules are out of scope for this target. They produce a stable
  `rule.devin.unsupported` warning, emit no artifact, and leave the run clean.
  This is a scope decision and not a Devin limitation: Devin 3000.3.27 does
  discover `.devin/rules/<name>.md`, even though `devin rules paths` does not
  list it. See `diag/codes.md`.

Codex-specific notes:

- One compilation result emits paths in both partitions: skills and converted
  commands at `.agents/skills/<name>/SKILL.md`, subagents at
  `.codex/agents/<name>.toml`, hooks at `.codex/hooks.json`, and MCP servers as
  `[mcp_servers.<name>]` tables in `.codex/config.toml`. Commands carry
  `x-agent-marketplace-kind: command` so the ingester can restore them as
  commands.
- The ingester detects a `.codex` workspace and then reads the sibling
  `.agents/skills`. Other agents share the `.agents/skills` layout, so that
  directory alone is never autodetected as Codex. Callers can still select
  `TargetCodex` explicitly.
- Hook and MCP artifacts use `ReconciliationPolicy.Mode == ReconciliationExternal`.
  They are shared native files, so the consumer that owns them must reconcile
  their entries. Generic `Materialize` refuses these artifacts instead of
  overwriting the file. Rules and `AGENTS.md` generation are out of scope.
  Deprecated `.codex/prompts/*.md` files are read as commands during ingestion
  and are never emitted.
- The supported native schema was checked on 2026-08-19 against the official
  Codex documentation for [skills](https://developers.openai.com/codex/skills),
  [subagents](https://developers.openai.com/codex/subagents),
  [hooks](https://developers.openai.com/codex/hooks), and
  [MCP](https://developers.openai.com/codex/mcp), plus the
  [config reference](https://developers.openai.com/codex/config-file/config-reference)
  for `oauth_resource`. The Codex compiler and ingester tests and the 7×7 matrix
  pin the exact fixture outcomes and losses; see
  [`field-matrix.md`](field-matrix.md) and [`diag/codes.md`](diag/codes.md).

### Materialize options

These `WriteOption` values control how `Materialize` writes to disk:

- `WithWriteMode(WriteModeFailIfExists)`: the default; returns an error if an output file already exists
- `WithWriteMode(WriteModeOverwrite)`: overwrites existing files
- `WithWriteMode(WriteModeSync)`: overwrites existing files and prunes stale ones
- `WithPruneStale(true)`: removes files in the output directory that are absent from the result
- `WithDryRun()`: reports planned writes without performing any I/O

### Compile options

These `Option` values are passed to `Compile`, `CompileBundle`, or `Convert`:

- `WithScope(scope)`: selects project or user scope for targets whose layout depends on it
- `WithPluginRoot(path)`: sets the literal plugin root used by targets that expand `${CLAUDE_PLUGIN_ROOT}` at compile time
- `WithModelOverride(map)` and `WithToolOverride(map)`: override entries in the model and tool mapping tables

### Golden-file testing

```bash
# Run golden tests
go test -run TestGolden ./...

# Refresh golden files
UPDATE_GOLDEN=1 go test -run TestGolden ./...
```

## Status

v0.x. The 7×7 translation matrix and the core API are implemented. The design document lists the deferred items.
