# Harness Compilation — N-to-N Conversion (v0.3)

**Status:** Accepted
**Date:** 2026-04-24
**Author:** example-user (with Droid)
**Predecessor:** [`2026-04-23-harness-compilation-design.md`](./2026-04-23-harness-compilation-design.md)
**Consumers:** primarily [`agent-marketplace`](https://github.com/Adyen/agent-marketplace)

## Purpose

Extend the library from **1-to-N** (Claude source → 5 targets) to full **N-to-N** conversion
across all five supported harnesses (Claude Code, Factory Droid, Cursor, Windsurf, Gemini CLI).

After this change, a user can ingest a skill/hook/subagent/MCP/command in *any* supported
harness format and emit it to any *other* harness format, with honest lossy diagnostics when
features don't cross.

## Non-goals (inherited from predecessor)

- Distribution / registry / sync (owned by `agent-marketplace`)
- Interactive UI or drift reconciliation
- New resource kinds beyond the Day-1 scope (Skills, Commands, Subagents, Hooks, MCP servers)

Added explicitly to non-goals for v0.3:

- **Cross-harness migration tooling** (e.g. "migrate a workspace from Windsurf to Factory").
  v0.3 is a library capability; workflow tooling is out of scope.
- **Round-trip lossless guarantees across harnesses.** Identity round-trips (X → IR → X) must
  be byte-stable. Cross-harness round-trips (X → Y → X) are *best-effort* — the loss profile
  per pair is documented in matrix fixtures.

---

## Corrections to the predecessor design

Research into Gemini CLI (`geminicli.com`) and Windsurf Cascade (`docs.windsurf.com`) docs
revealed that several emission-side lossy policies in the v0.1 design were based on outdated
assumptions. These corrections land in **Phase 0** of this plan, *before* any N-to-N work.

| v0.1 assumption | Reality (2026-04) | Phase-0 correction |
|---|---|---|
| Windsurf has no skill primitive; flatten into `workflows/` | Windsurf has `.windsurf/skills/<n>/SKILL.md` (agentskills.io standard, identical to Claude) | Emit `.windsurf/skills/<n>/SKILL.md`; drop `skill.windsurf.scripts-flattened` diagnostic |
| Gemini has no skill primitive; coalesce into `GEMINI.md` | Gemini has `.gemini/skills/<n>/SKILL.md` (agentskills.io) | Emit `.gemini/skills/<n>/SKILL.md`; drop `skill.gemini.coalesced` |
| Gemini has no subagent primitive | Gemini has `.gemini/agents/<n>.md` with YAML frontmatter | Emit subagents natively; drop `subagent.gemini.no-primitive` |
| Gemini has no hook support | Gemini has settings.json hooks with events nearly 1:1 with Claude (`BeforeTool`↔`PreToolUse`, `PreCompress`↔`PreCompact`) and even provides `CLAUDE_PROJECT_DIR` as a compat env alias | Emit hooks into `.gemini/settings.json`; drop `hook.gemini.no-support` |
| Windsurf hooks: expand env tokens literally, leave event names as-is | Windsurf hooks are split-per-tool (`pre_read_code`, `pre_write_code`, `pre_run_command`, `pre_mcp_tool_use`, `pre_user_prompt`, `post_cascade_response`, etc.); need explicit event-vocabulary mapping | Extend mapping table `compile/mapping/hook_events.go` |

### Revised capability matrix

| Primitive | Claude | Factory | Cursor | Windsurf | Gemini |
|---|---|---|---|---|---|
| Skills | ✓ | ✓ | ✓ | ✓ `.windsurf/skills/` | ✓ `.gemini/skills/` |
| Commands | ✓ md | ✓ md | ✓ md | ✓ workflows (md) | ✓ TOML |
| Subagents | ✓ | ✓ | ✓ | ✗ | ✓ `.gemini/agents/*.md` |
| Hooks | ✓ | ✓ | ✓ | ✓ (split-per-tool events) | ✓ (near 1:1) |
| MCP | ✓ | ✓ | ✓ | ✓ | ✓ |

The only true "drop" across the whole 5×5 matrix is **Subagent → Windsurf**
(`subagent.windsurf.no-primitive`). Everything else is a shape or vocabulary translation.

---

## Architecture

### Pivot-through-IR (unchanged)

```
  ingest/<h>/ ──►  ir (superset)  ──►  compile/<h>/  ──►  artifact/
    × 5                                   × 5
```

`Convert(src, from, to)` is a thin wrapper: `IngestBundle(src, from) → Compile(ir, to)`.

### Module layout diff

```
ingest/                         NEW — symmetric to compile/
├── ingest.go                   Ingester interface + registry + Autodetect()
├── aliases.go                  Shared: .agents/skills/ and .ai_skills/ loaders
├── claude/                     Wraps existing bundle/ loaders
├── factory/                    .factory-plugin + .factory
├── cursor/                     .cursor-plugin + .cursor
├── windsurf/                   .windsurf + ~/.codeium/windsurf
└── gemini/                     gemini-extension.json + .gemini

ir/
├── ir.go                       EXTENDED — Ext ext.Bag + Extra map[string]any on all resources
└── ext/                        NEW — typed per-harness sidecars
    └── ext.go                  Bag{Factory, Cursor, Windsurf, Gemini, Claude}

compile/<h>/                    MODIFIED — prefer Ext.<target> when non-nil, else derive

diag/codes.go                   EXTENDED — new ingest.* code family

testdata/matrix/                NEW — per-kind input fixtures + canonical_ir.json + expected/
```

---

## Superset IR with `ext.*` sidecars

IR core stays generic. Target-specific detail lives in typed sidecars.

```go
// ir/ir.go
type Skill struct {
    Name, Description, Body string
    Assets []Asset             // portable across all 5 (agentskills.io)
    Ext    ext.Bag             // typed target-specific sidecars
    Extra  map[string]any      // truly unknown frontmatter
}

type Command struct {
    Name, Description, Body string
    Ext    ext.Bag
    Extra  map[string]any
}

type Subagent struct {
    Name, Description, Body string
    Model           string    // canonical Claude family name ("sonnet", "opus", "haiku")
    Tools           []string  // canonical Claude tool IDs
    DisallowedTools []string
    Ext             ext.Bag
    Extra           map[string]any
}

type Hook struct {
    Event, Type, Command, Matcher string
    Ext   ext.Bag
    Extra map[string]any
}

type McpServer struct {
    Name, Transport, Command string
    Args  []string
    Env   map[string]string
    URL   string
    Ext   ext.Bag
    Extra map[string]any
}
```

```go
// ir/ext/ext.go
type Bag struct {
    Factory  *Factory
    Cursor   *Cursor
    Windsurf *Windsurf
    Gemini   *Gemini
    Claude   *Claude
}

type Factory struct {
    ReasoningEffort string  // Subagent
    Isolation       string  // Subagent
}

type Windsurf struct {
    AutoExecutionMode string             // Command/Workflow
    OriginalHookEvent string             // Hook — preserves e.g. "pre_read_code"
    HookPowerShell    string             // Hook: Windows variant
    HookShowOutput    *bool
    HookWorkingDir    string
}

type Gemini struct {
    Temperature      *float64
    MaxTurns         *int
    TimeoutMinutes   *int
    Kind             string             // "local" | "remote"
    InlineMCPServers map[string]ir.McpServer // Subagent
    TOMLCommand      *GeminiTOMLCommand // Command
    HookTimeout      *int
    HookName         string
}

type Cursor struct { /* Cursor-specific passthrough */ }
type Claude struct { /* rarely used; e.g. hook.type=http that other harnesses drop */ }
```

### Emitter precedence rule

Each emitter reads `Ext.<target>` when non-nil (verbatim emission) and falls back to deriving
from canonical fields otherwise.

```go
// compile/factory/subagent.go
if sub.Ext.Factory != nil && sub.Ext.Factory.ReasoningEffort != "" {
    fm.ReasoningEffort = sub.Ext.Factory.ReasoningEffort  // verbatim
} else {
    fm.ReasoningEffort = mapping.ModelToEffort(sub.Model)  // derived
    result.Diag(diag.Info, "subagent.factory.effort-derived-from-model", ...)
}
```

### Identity round-trip guarantee

With `Ext.*` populated by ingesters, **`<H> → IR → <H>` is byte-stable** for every supported
harness, enforced by the test matrix's identity cells.

---

## Public API

```go
package harnesscompile

// Target retained as deprecated alias; new code should use Harness.
type Harness = Target

// One-call conversion
func Convert(src fs.FS, from, to Harness, opts ...Option) (*Result, error)

// Ingest a bundle for a specific source harness (autodetect layout).
func IngestBundle(src fs.FS, from Harness) (*ir.Bundle, error)

// Existing surface unchanged:
func Compile(r Resource, t Harness, opts ...Option) (*Result, error)
func CompileBundle(src fs.FS, t Harness, opts ...Option) (*Result, error)  // defaults from=Claude
func Materialize(r *Result, outDir string, opts ...WriteOption) (*WriteReport, error)

// New ergonomic helper:
func Autodetect(src fs.FS) (Harness, bool)
```

`Convert` is the one-line happy path for agent-marketplace:

```go
result, err := hcc.Convert(srcFS, hcc.HarnessWindsurf, hcc.HarnessFactory)
```

---

## Ingestion layer

### Ingester interface

```go
// ingest/ingest.go
package ingest

type Bundle struct {
    Manifest    ir.Manifest          // synthetic for Windsurf/Gemini
    Resources   []ir.Resource
    Diagnostics []diag.Diagnostic    // ingest-side loss; code prefix "ingest.*"
}

type Ingester interface {
    Source() Harness
    Detect(src fs.FS) bool
    LoadBundle(src fs.FS) (*Bundle, error)
}

func Register(Ingester)
func For(Harness) (Ingester, bool)
func Autodetect(src fs.FS) (Harness, bool)
```

### Ingest roots per harness (union, first-match-wins per file)

| Harness | Plugin form | Workspace form | Cross-agent skills aliases |
|---|---|---|---|
| Claude | `.claude-plugin/plugin.json` + siblings | `.claude/` (skills, agents, commands, hooks/, .mcp.json) | `.agents/skills/`, `.ai_skills/` |
| Factory | `.factory-plugin/plugin.json` | `.factory/` | `.agents/skills/`, `.ai_skills/` |
| Cursor | `.cursor-plugin/plugin.json` | `.cursor/` | `.agents/skills/`, `.ai_skills/` |
| Windsurf | — | `.windsurf/` (+ `~/.codeium/windsurf/`) | `.agents/skills/`, `.ai_skills/` |
| Gemini | `gemini-extension.json` | `.gemini/` | `.agents/skills/`, `.ai_skills/` |

### Autodetect order (most-specific first)

1. `.claude-plugin/plugin.json` → Claude
2. `.factory-plugin/plugin.json` → Factory
3. `.cursor-plugin/plugin.json` → Cursor
4. `gemini-extension.json` → Gemini
5. `.claude/` with skills/agents/commands/hooks/.mcp.json → Claude
6. `.factory/` → Factory
7. `.cursor/` → Cursor
8. `.gemini/` → Gemini
9. `.windsurf/` → Windsurf
10. Only `.agents/skills/` or `.ai_skills/` → ambiguous; default Claude + info diag

### Canonicalization rules per ingester

**Env tokens** — normalize harness-specific to canonical `${PLUGIN_ROOT}`:
- Claude `${CLAUDE_PLUGIN_ROOT}` → `${PLUGIN_ROOT}`
- Factory `${DROID_PLUGIN_ROOT}` → `${PLUGIN_ROOT}`
- Gemini `${GEMINI_PROJECT_DIR}` / `${CLAUDE_PROJECT_DIR}` → `${PLUGIN_ROOT}`
- Windsurf literal paths preserved (info diag `ingest.windsurf.literal-path-preserved`)

**Hook event vocabulary** — canonicalize to Claude event names; preserve original in `Ext.*`:
- Windsurf `pre_read_code` → `PreToolUse` + matcher `Read`
- Windsurf `pre_write_code` → `PreToolUse` + matcher `Write|Edit`
- Windsurf `pre_run_command` → `PreToolUse` + matcher `Bash`
- Windsurf `pre_user_prompt` → `UserPromptSubmit`
- Windsurf `post_cascade_response` → `Stop`
- Windsurf `post_setup_worktree` → no Claude equivalent; preserve in `Ext.Windsurf`, warn
- Gemini `BeforeTool`/`AfterTool` → `PreToolUse`/`PostToolUse` (direct)
- Gemini `PreCompress` → `PreCompact`
- Gemini `BeforeModel`/`AfterModel` → preserve in `Ext.Gemini`, warn

**Subagent model/effort** — Factory ingester populates both `Model` (via mapping table) AND
preserves `ReasoningEffort` in `Ext.Factory` for lossless round-trip.

**Command shape** — Gemini TOML ingester parses to canonical `Command{Body, Description}`
AND preserves the TOML structure in `Ext.Gemini.TOMLCommand`.

**Windsurf workflow disambiguation** — Windsurf workflows ingest as `Command` by default
(Windsurf has first-class Skills as a separate primitive). Info diag
`ingest.windsurf.workflow-assumed-command`.

### Cross-agent skills aliases

`.agents/skills/` and `.ai_skills/` are harness-agnostic. Shared helper `ingest/aliases.go`
is called by every `LoadBundle` after harness-specific roots. Skills from aliases emit
`ingest.<source>.skills-from-alias` (info). Plugin/bundle form wins over workspace form
wins over alias form on conflict, with `skill-override-shadowed` warning.

### New diagnostic code family

```
ingest.windsurf.workflow-assumed-command            (info)
ingest.windsurf.literal-path-preserved              (info)
ingest.windsurf.hook-event-unmappable               (warning)
ingest.windsurf.subagent-via-agents-md-skipped      (info)
ingest.factory.reasoning-effort-mapped-to-model     (info)
ingest.gemini.toml-command-canonicalized            (info)
ingest.gemini.hook-event-partial-mapping            (info)
ingest.<source>.bundle-manifest-synthesized         (info)
ingest.<source>.skills-from-alias                   (info)
ingest.<source>.skill-override-shadowed             (warning)
ingest.<source>.extension-field-preserved           (info)
```

All enforced by the existing `TestDiagnosticCodes_Documented` CI gate.

---

## E2E test matrix

### Directory layout

```
testdata/matrix/
├── skill/
│   ├── inputs/
│   │   ├── claude/deploy-prod/SKILL.md
│   │   ├── factory/deploy-prod/SKILL.md
│   │   ├── cursor/...
│   │   ├── windsurf/...
│   │   └── gemini/...
│   ├── canonical_ir.json        # single source of truth for what all 5 inputs normalize to
│   ├── matrix.yaml              # cell classification
│   └── expected/
│       ├── claude-to-claude/        # identity — byte-stable
│       ├── claude-to-factory/       # marquee — byte-stable
│       └── ... (25 dirs)
├── command/  (same shape)
├── subagent/ (same shape; drops for subagent→windsurf)
├── hook/     (same shape; exercises event-vocabulary mapping)
└── mcp/      (same shape; exercises interpolation rewriting)
```

### Cell classes

| Class | Semantics | Assertion |
|---|---|---|
| **identity** (5) | source == target | byte-stable golden |
| **marquee** (~3-5 per kind) | high-value cross-cells (Claude→Factory, Windsurf→Gemini, …) | byte-stable golden |
| **cross** (rest) | lower-priority cross-cells | artifact shape + diagnostic codes only |
| **drop** (subagent→Windsurf only) | target lacks primitive | empty artifacts + specific diag code |

Classification declared in per-kind `matrix.yaml`:

```yaml
identity: [claude, factory, cursor, windsurf, gemini]
marquee:
  - {from: claude, to: factory}
  - {from: claude, to: windsurf}
  - {from: windsurf, to: gemini}
drop: []
```

### Test driver

One table-driven test per kind iterates all 25 cells, dispatching assertion mode from the
`matrix.yaml` class:

```go
func TestMatrix_Skill(t *testing.T) {
    for _, from := range allHarnesses {
        for _, to := range allHarnesses {
            t.Run(fmt.Sprintf("%s-to-%s", from, to), func(t *testing.T) {
                cell := loadCell(t, "skill", from, to)
                bundle, err := ingest.For(from).LoadBundle(cell.InputFS)
                require.NoError(t, err)
                result, err := harnesscompile.Compile(bundle.Resources[0], to)
                require.NoError(t, err)
                switch cell.Class {
                case identity, marquee:
                    assertBytesEqualGolden(t, result, cell.ExpectedDir)
                case cross:
                    assertArtifactShapeMatches(t, result, cell.ExpectedDir)
                    assertDiagnosticCodesMatch(t, result.Diagnostics, cell.ExpectedDiagCodes)
                case drop:
                    assert.Empty(t, result.Artifacts)
                    assertDiagnosticCodesMatch(t, result.Diagnostics, cell.ExpectedDiagCodes)
                }
                assertCanonicalIRModuloExt(t, bundle.Resources[0], cell.CanonicalIR, from)
            })
        }
    }
}
```

### Round-trip property tests (free with the matrix)

- `TestMatrix_RoundTripSameHarness` — for every `(h, kind)`: Ingest → Compile(h) → Ingest,
  assert IR unchanged modulo Ext normalization. Catches identity-cell fidelity regressions.
- `TestMatrix_CrossHarnessLossProfile` — for every `(X, Y)`: ingest X, compile Y, ingest Y,
  assert produced IR equals `canonical_ir.json` minus the documented loss set declared in
  `matrix.yaml`.

### CI gate extensions

- Every diagnostic code emitted by any matrix cell must appear in `diag/codes.md`.
- Every code in `diag/codes.md` must be exercised by at least one matrix cell (catches dead
  codes).
- Golden-file diff must be clean.

---

## Phased implementation plan

Scope is large; phasing keeps `main` green and `agent-marketplace` unbroken throughout.

### Phase 0 — Correct existing emit-side assumptions (5 beads)

One bead per emit correction. Each regenerates its own goldens.

| Bead | Correction |
|---|---|
| Windsurf skill emitter | Write `.windsurf/skills/<n>/SKILL.md`; drop `skill.windsurf.scripts-flattened` |
| Gemini skill emitter | Write `.gemini/skills/<n>/SKILL.md`; drop `skill.gemini.coalesced` |
| Gemini subagent emitter | NEW — write `.gemini/agents/<n>.md`; drop `subagent.gemini.no-primitive` |
| Gemini hook emitter | NEW — write `.gemini/settings.json` hooks section; drop `hook.gemini.no-support` |
| Windsurf hook emitter | Remap Claude events to Windsurf split-per-tool events via mapping table |

### Phase 1 — Superset IR + `ext.*` sidecar (1 bead)

Additive. `Ext` fields default nil → existing emitter paths unchanged.

### Phase 2 — Public API symmetry (1 bead)

- `Harness` alias + `Convert()` + `IngestBundle()` + `Autodetect()`.
- Only `from == HarnessClaude` works until Phase 3 ingesters land; other values return
  "not implemented yet" error with tracking bead ID in the message.
- `agent-marketplace` can adopt the `Harness` naming immediately.

### Phase 3 — Per-harness ingesters (5 beads, independently parallelizable)

Order by payoff-to-complexity:

1. Factory (simplest, mirrors Claude)
2. Cursor (mirrors Factory)
3. Claude (wraps existing `bundle/` — mostly reorganization)
4. Gemini (TOML command codec, settings.json hooks, subagent frontmatter)
5. Windsurf (hook event remapping, workflow-as-command, literal-path env)

Each bead includes: ingester under `ingest/<h>/`, per-kind unit tests, input fixtures under
`testdata/matrix/<kind>/inputs/<h>/`, diagnostic codes in `diag/codes.md`, enables the
corresponding matrix row.

### Phase 4 — Matrix test driver + canonical IR (1 bead)

- `testdata/matrix/<kind>/{matrix.yaml, canonical_ir.json}` per kind.
- `matrix_<kind>_test.go` table-driven driver per kind.
- Round-trip and loss-profile property tests.

### Phase 5 — Hardening & docs (1 bead)

- Flesh out `diag/codes.md` examples for every new code.
- Update predecessor design doc status to reference this one.
- Add `cmd/hcc convert --from X --to Y <dir>` subcommand.
- Bump to `v0.3.0`; changelog.

### Estimated size

| Phase | Beads | Rough LoC |
|---|---|---|
| 0 | 5 | ~300 |
| 1 | 1 | ~400 |
| 2 | 1 | ~150 |
| 3 | 5 | ~2500 (~500 each) |
| 4 | 1 | ~600 |
| 5 | 1 | ~200 |
| **Total** | **14** | **~4150** |

### Rollback strategy per phase

- Phase 0 — contained emit corrections; per-bead revert-safe.
- Phase 1 — purely additive; revert-safe.
- Phase 2 — additive, `Convert` returns "not implemented" for most sources; revert-safe.
- Phase 3 — each ingester independent; revert one without affecting others.
- Phase 4 — can disable via `SKIP_MATRIX=1` env while keeping fixtures.
- Phase 5 — docs/CLI only.

---

## Versioning

- `v0.3.0` — N-to-N support lands.
- Minor-bumps for additional ingest codes, new `Ext.*` fields (additive).
- Major-bumps for renamed/removed `ext.*` fields or removed ingest codes.

## Integration with agent-marketplace

One-liner cross-harness conversion becomes:

```go
import hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"

result, err := hcc.Convert(srcFS, hcc.HarnessWindsurf, hcc.HarnessFactory)
report, err := hcc.Materialize(result, outDir, hcc.WithWriteMode(hcc.WriteModeSync))
```

agent-marketplace continues to own: registry discovery, per-user config layering, drift
reporting, TUI/CLI UX. The library adds the ability to treat *any* registry entry as a
first-class source, not just Claude-formatted ones.
