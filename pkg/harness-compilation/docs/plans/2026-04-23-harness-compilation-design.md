# Harness Compilation Library — Design

**Status:** Superseded by [N-to-N Design](./2026-04-24-harness-compilation-n-to-n-design.md)
**Date:** 2026-04-23
**Author:** example-user (with Droid)
**Consumers:** primarily [`agent-marketplace`](https://github.com/Adyen/agent-marketplace)

## Purpose

A Go library that converts AI-agent configuration resources written in a single canonical source format into the vendor-specific layouts consumed by six target harnesses:

- Claude Code
- Factory Droid
- Cursor
- Windsurf
- Gemini CLI
- Devin CLI

The library is **resource-centric**: the atomic unit is one skill, one command, one subagent, one hook, or one MCP-server entry. A "plugin" is only a convenience wrapper (a set of resources under one manifest).

## Non-goals

- Distribution / registry / sync (owned by `agent-marketplace`).
- Rendering, UI, or interactive management.
- Anything outside of file-format translation.

---

## Design decisions (locked)

| Decision | Value | Rationale |
|---|---|---|
| **Source format** | Claude-plugin (`.claude-plugin/plugin.json` + `skills/`, `agents/`, `hooks/hooks.json`, `.mcp.json`, `commands/`) | Most feature-complete among the five; already the interop standard — Factory documents binary compatibility with Claude plugins. |
| **Day-1 resource scope** | Skills, Commands, Subagents, Hooks, MCP servers | Covers ~95% of real plugins. Explicitly deferred: LSP servers, monitors, channels, userConfig, dependencies. |
| **Lossy policy** | Warn + skip (nil artifacts + structured `Diagnostic`) | Honest for programmatic consumers; lets agent-marketplace surface skips in its UI. |
| **Output API** | Pure `Compile` returns `[]Artifact`; separate `Materialize` writes to disk (atomic staged-then-rename, optional prune, dry-run) | Keeps core testable without temp dirs; `agent-marketplace` uses the write helper instead of re-implementing it. |
| **Name/tool mapping** | Curated built-in translation table, with `Option` overrides for escape-hatch | Captures the business-logic concentration; overrides prevent library lag from blocking consumers. |
| **Granularity** | Per-resource compile is core; bundle compile is a thin wrapper | Matches `agent-marketplace`'s existing per-skill registry model. |

---

## Public API

```go
package harnesscompile

type Target string
const (
    TargetClaude   Target = "claude"
    TargetFactory  Target = "factory"
    TargetCursor   Target = "cursor"
    TargetWindsurf Target = "windsurf"
    TargetGemini   Target = "gemini"
    TargetDevin    Target = "devin"
)

// Resource is the sealed sum type for all compilable kinds.
type Resource interface { Kind() ResourceKind; Name() string }

type ResourceKind int
const (
    KindSkill ResourceKind = iota
    KindCommand
    KindSubagent
    KindHook
    KindMcpServer
)

// Per-resource loaders — parse a single resource (Claude format).
// Thin facades over the bundle package; the root package is self-registering,
// so importing it alone is enough to load, compile, and ingest resources.
func LoadSkill(src fs.FS, path string)      (*ir.Skill, error)
func LoadCommand(src fs.FS, path string)    (*ir.Command, error)
func LoadSubagent(src fs.FS, path string)   (*ir.Subagent, error)
// LoadHooks also returns diagnostics: flattening hooks.json is lossy for
// non-command hook types (http/prompt/agent), and the lossy policy forbids
// dropping those warnings on the floor.
func LoadHooks(src fs.FS, path string)      ([]*ir.Hook, []diag.Diagnostic, error)
func LoadMcpServers(src fs.FS, path string) ([]*ir.McpServer, error)

// CORE: compile a single resource into target-specific artifacts.
func Compile(r Resource, t Target, opts ...Option) (*Result, error)

// Convenience: compile an entire Claude-plugin directory.
func CompileBundle(src fs.FS, t Target, opts ...Option) (*Result, error)

// Phase 3: write a Result to disk atomically.
func Materialize(r *Result, outDir string, opts ...WriteOption) (*WriteReport, error)

type Result struct {
    Artifacts   []artifact.Artifact   // Path relative to target's install root
    Diagnostics []diag.Diagnostic
}

type WriteMode int
const (
    WriteModeFailIfExists WriteMode = iota
    WriteModeOverwrite
    WriteModeSync                     // overwrite + prune stale
)

type WriteOption func(*writeConfig)
func WithWriteMode(m WriteMode) WriteOption
func WithPruneStale(enabled bool) WriteOption
func WithDryRun() WriteOption

type WriteReport struct {
    Written []string
    Skipped []string  // identical content, sha256 match
    Removed []string  // only populated with WithPruneStale
    OutDir  string
}
```

### Install roots

Artifact paths are relative to each target's install root:

| Target | Install root |
|---|---|
| Claude | plugin directory (sibling of `.claude-plugin/`) |
| Factory | plugin directory (sibling of `.factory-plugin/`) |
| Cursor | plugin directory (sibling of `.cursor-plugin/`) |
| Windsurf | workspace root (artifacts live under `.windsurf/`) |
| Gemini | extension directory (`gemini-extension.json` at root) |
| Devin CLI | plugin directory (`.devin/`) |

---

## Architecture

```
harness-compilation/
├── go.mod
├── harnesscompile.go         Public API facade
├── bundle/                   Claude-format loader (Stage 1)
│   ├── skill.go
│   ├── command.go
│   ├── subagent.go
│   ├── hook.go
│   ├── mcp.go
│   └── load.go               CompileBundle's walker
├── ir/                       Normalized in-memory representation
│   └── ir.go                 Resource, Skill, Command, Subagent, Hook, McpServer
├── compile/
│   ├── compile.go            Compiler interface + registry + dispatcher
│   ├── mapping/
│   │   ├── models.go         Claude model family → per-target model ID
│   │   ├── tools.go          Claude tool ID → per-target tool ID(s)
│   │   └── envvars.go        ${CLAUDE_PLUGIN_ROOT} → per-target substitution
│   ├── claude/               Near-identity passthrough
│   ├── factory/              Dir renames, FM normalization, env swap
│   ├── cursor/               Isomorphic to factory (different dir names)
│   ├── windsurf/             Lossy: skills→workflows, subagent→drop, hook→literal path
│   ├── gemini/               Lossy: skills→TOML or inline GEMINI.md, hook/subagent→drop
│   └── devin/                Available target; consumer-controlled enablement: skills→symlinks, subagents→flat profiles, shared aggregates
├── diag/
│   ├── diag.go               Diagnostic, Severity, SourceRef
│   └── codes.go              Code catalog + DocForCode
├── artifact/
│   ├── artifact.go           Artifact struct
│   └── fragment.go           Fragment merge (multiple resources → one file)
├── internal/yamlfm/          Shared frontmatter parser
├── docs/plans/               (this doc)
├── testdata/
│   ├── resources/            Hand-crafted input fixtures (per resource kind)
│   └── expected/             Golden output per target
└── cmd/hcc/                  Optional CLI for local testing
```

---

## Pipeline

```
         ┌─────────────┐   ┌─────────────┐   ┌──────────────┐
 fs.FS ─►│    Load     │──►│   Compile   │──►│  Materialize │──► disk
         │ (bundle/*)  │   │ (compile/*) │   │  (artifact/) │
         └─────────────┘   └─────────────┘   └──────────────┘
              │                  │                   │
              ▼                  ▼                   ▼
           ir.*              *Result            *WriteReport
                          (Artifacts +
                           Diagnostics)
```

Load and Compile are pure; only Materialize has I/O.

### Stage 1 — Load

Walk Claude-plugin layout, parse per-resource, validate:

- **Errors (halt):** missing `plugin.json` (only for `CompileBundle`), invalid frontmatter, duplicate resource names, invalid name (must be kebab-case), path traversal.
- **Warnings (attach to `ir.LoadDiagnostics`):** unknown manifest fields we intentionally don't translate (LSP/monitors/channels), unsupported hook types (`http`/`prompt`/`agent` — only `command` is universally supported in v0).

Unknown but non-blocking frontmatter fields are preserved in `Extra map[string]any` on each IR node to enable round-trip correctness for target-specific fields we pass through verbatim (e.g., `isolation: worktree`).

### Stage 2 — Compile

Dispatch `Compile(r, t)` to `compile.<t>.Translate(r)`. Each translator:

1. Applies shared token substitution (env vars, MCP interpolation).
2. Applies mapping tables (models, tools).
3. Emits `[]Artifact` + `[]Diagnostic`.

### Stage 3 — Materialize

1. Validate artifact paths are relative, no `..` escapes.
2. Stage to `outDir/.tmp-<rand>/`, write each artifact.
3. Atomic `rename` onto `outDir`.
4. If `WithPruneStale`, remove files in `outDir` absent from artifacts.
5. Identity detection: `sha256(bytes) == existing` → mark `Skipped`.

---

## Translation matrix

Symbols: `✓` direct, `~` translated with named diagnostic, `✗` unsupported → nil + diagnostic.

| Resource → | Skill | Command | Subagent | Hook | MCP Server |
|---|---|---|---|---|---|
| **Claude** | ✓ passthrough | ✓ passthrough | ✓ passthrough | ✓ passthrough | ✓ passthrough |
| **Factory** | ✓ `skills/<n>/SKILL.md` | ✓ `commands/<n>.md` | ~ `droids/<n>.md` (`effort`→`reasoningEffort`, `disallowedTools`→drop+diag, model table, tool table) | ~ `hooks/hooks.json` fragment, `${CLAUDE_PLUGIN_ROOT}`→`${DROID_PLUGIN_ROOT}`, non-command types→diag+drop | ~ `mcp.json` fragment |
| **Cursor** | ✓ `skills/<n>/SKILL.md` | ✓ `commands/<n>.md` | ~ `agents/<n>.md` (model table, tool table) | ~ `hooks/hooks.json` fragment | ~ `mcp.json` fragment |
| **Windsurf** | ~ `.windsurf/workflows/<n>.md` (diag on scripts/ presence) | ~ `.windsurf/workflows/<n>.md` | ✗ `subagent.windsurf.no-primitive` | ~ `.windsurf/hooks/<event>.json`, env tokens expanded to literal paths + diag | ~ `mcp_config.json` fragment |
| **Gemini** | ~ `commands/<n>.toml` or inline into `GEMINI.md` section | ~ `commands/<n>.toml` (MD→TOML) | ✗ `subagent.gemini.no-primitive` | ✗ `hook.gemini.no-support` | ✓ `gemini-extension.json` fragment under `mcpServers` |

### Fragment merging

Multiple resources that share an output file (MCP servers → `mcp.json`; hooks → `hooks.json`; Gemini inline skills → `GEMINI.md`) compile to **fragment artifacts** with `Fragment = true`. `Materialize` and `CompileBundle` merge fragments sharing a Path into a single final artifact. This keeps per-resource `Compile` uniform.

---

## Shared helpers (`compile/mapping/`)

- **`Model(claudeValue, target) (string, bool)`** — curated translation table for Claude model families (`sonnet`, `opus`, `haiku`, specific IDs). `false` → emit `W_MODEL_PASSTHROUGH` diagnostic, pass-through value.
- **`Tool(claudeToolID, target) (string, bool)`** — curated tool-ID translation. `Read`→`Read`, `Write`→`Edit`+`Create` (Factory), etc. Unknown → `W_TOOL_UNKNOWN` diagnostic.
- **`EnvToken(source, target) string`** — `${CLAUDE_PLUGIN_ROOT}` → target's equivalent or literal expansion (Windsurf).
- **`McpInterpolation(source, target)`** — Windsurf's `${env:VAR}`/`${file:...}` patterns, Claude/Factory's inline env, Gemini's `${extensionPath}`.

All tables versioned with `// Last verified: <date> against <target> <version>` comments. CI checks for table-shrinkage regressions. Callers can override any entry via `WithModelOverride`, `WithToolOverride`, etc.

---

## Diagnostics taxonomy

```go
type Diagnostic struct {
    Code     string     // "subagent.windsurf.no-primitive"
    Severity Severity   // Error | Warning | Info
    Target   Target
    Resource string
    Message  string
    Hint     string
    Source   SourceRef  // original file + line
}
```

### Severity rules

| Severity | Meaning | Halts compile? |
|---|---|---|
| `Error` | Source is malformed or violates safety (path traversal) | Yes |
| `Warning` | Resource translated lossily or dropped | No |
| `Info` | Translation succeeded with notable transformation | No |

### v0 code catalog (documented in `diag/codes.md`)

```
skill.gemini.coalesced                   (info)
skill.windsurf.scripts-flattened         (warning)
command.*.ok                             (no diagnostic — happy path)
subagent.factory.disallowed-tools-dropped   (warning)
subagent.factory.isolation-dropped          (warning)
subagent.windsurf.no-primitive              (warning)
subagent.gemini.no-primitive                (warning)
subagent.*.tool-unknown                     (warning; one per unmapped tool)
subagent.*.model-passthrough                (info)
hook.factory.non-command-type-dropped       (warning)
hook.windsurf.env-token-expanded            (warning)
hook.gemini.no-support                      (warning)
mcp.*.interpolation-unsupported             (warning)
bundle.conflict.duplicate-artifact-path     (error)
```

Every emitted code must have a catalog entry (enforced by CI test). `harnesscompile.DocForCode(code)` returns structured documentation for consumer UIs.

---

## Testing strategy

### L1 — Loader unit tests

Table-driven per-resource loaders. Fuzz-style cases for malformed frontmatter, path traversal, missing delimiters. Borrow patterns from `agent-marketplace/internal/skill/skill_test.go`.

### L2 — Matrix cell tests

One test file per cell of the 5×5 translation matrix (25 files). Each exercises one translator function in isolation with hand-crafted IR inputs. These are the fastest, highest-business-logic-coverage tests.

### L3 — Golden-file tests

```
testdata/
├── resources/           Hand-crafted input fixtures
└── expected/<target>/   Golden output per target
```

Refresh via `UPDATE_GOLDEN=1 go test`. Any byte change fails the test loudly.

### L4 — Round-trip & Materialize

- Claude→IR→Claude must be byte-stable (canonical YAML key ordering on emit).
- Materialize tests use `testing/fstest` for pure cases and `t.TempDir()` for atomicity.
- Second materialize of identical input → all `Skipped`, zero `Written`.

### Property tests (Phase 2)

Generate random valid IR → compile → reparse via target's own validator (where available) → assert round-trip. Deferred from v0.

### CI gates

- `go test ./...`, `go vet`, `staticcheck`
- `TestMappingTables_Coverage` — anchor entries must stay present
- `TestDiagnosticCodes_Documented` — every code has a catalog entry
- Golden-file diff must be clean

---

## Versioning

- `v0.x` until the 5×5 matrix and diagnostic codes stabilize.
- Patch: internal refactors, passthrough additions.
- Minor: new diagnostic codes (additive), new `Option`s, new resource kinds.
- Major: renamed/removed diagnostic codes, breaking IR changes.

---

## Out of scope (parked for future consideration)

- LSP servers (Claude plugin reference)
- Monitors (Claude plugin reference)
- Channels (Claude plugin reference)
- `userConfig` prompt flow
- Plugin `dependencies` resolution
- Non-Claude source formats (a hypothetical "neutral bundle" manifest, or reverse-direction compilers like Windsurf→Claude)
- Property-based random-IR testing

---

## Integration with agent-marketplace

agent-marketplace discovers skills in its registries (each typically a loose SKILL.md). Its integration pattern:

```go
import hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"

// For each registry skill × enabled harness target:
skill, _ := hcc.LoadSkill(registryFS, skillDir)
result, _ := hcc.Compile(skill, hcc.TargetFactory)
report, _ := hcc.Materialize(result, "/path/to/.factory/skills", hcc.WithWriteMode(hcc.WriteModeSync))
```

agent-marketplace continues to own:
- Registry discovery and caching
- Per-user/per-project config layering
- Symlinks vs copies placement decisions (can point `Materialize` at its target dir instead)
- TUI/CLI UX and drift reporting (consuming `Diagnostics` + `WriteReport`)
