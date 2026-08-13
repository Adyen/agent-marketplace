
# agent-marketplace v2: harness-compilation integration

> **Status: Implemented.** This plan was the design for the v2 harness-compilation integration, which is now the default and only schema. The v1 symlink-based pipeline described below as "today's" model has been replaced.

## Context summary

The pre-v2 agent-marketplace was skill-only and harness-agnostic by fiction:
- `internal/skill.DiscoverSkills` walked registry caches looking for `SKILL.md` files.
- `internal/sync.Sync` symlinked each discovered skill into `.agents/skills/<name>` or `~/.agents/skills/<name>`.
- The whole pipeline assumed every harness reads from `.agents/skills/` — which only some did.

`harness-compilation` (HCC) just landed a v0.3 N-to-N design:
- IR covers 5 resource kinds: **Skill, Command, Subagent, Hook, McpServer**.
- `Ingest` reads any of 5 harness layouts (Claude / Factory / Cursor / Windsurf / Gemini); `Compile` emits target-native layouts; `Materialize` writes to disk atomically (stage → rename, with `WriteModeSync` that prunes stale files — the same orphan-cleanup property today's `sync` gives us via symlink removal).
- `Convert(srcFS, from, to)` and `Autodetect(srcFS)` are the one-liners.

Breaking change is explicitly allowed (new major). Proposal below.

## What changes conceptually

| Axis | v1 | v2 |
|---|---|---|
| Unit of distribution | SKILL.md only | Any IR resource kind (skill/command/subagent/hook/MCP) |
| Registry ingest | bespoke SKILL.md walker | `hcc.IngestBundle` (auto-detect, or pinned `source_harness`) |
| Sync mechanism | symlinks to `.agents/skills/` | `hcc.Materialize` writes native per-harness layouts |
| Install targets | implicit (`.agents/skills/`, `~/.agents/skills/`) | explicit `harnesses:` config; each target gets its native root |
| Source-of-truth | registry SKILL.md | registry in any of 5 formats |
| Diagnostics | errors only | HCC `Diagnostic` with codes surfaced in `status`/`sync` |

The *selection* UX stays skill-centric for non-power-users (`install vm-lifecycle`), but the data model and output are full-fidelity IR.

## Target config schema (v2, breaking)

```yaml
apiVersion: agent-marketplace/v2

# Which harnesses to materialize into. Each is an independent output target.
# Can be overridden per-registry-entry or per-collection.
harnesses:
  - factory
  - cursor

registries:
  - url: git@gitlab.example.com:example-org/example-infra-skills.git
    ref: main
    source_harness: claude      # optional; defaults to hcc.Autodetect
    # Selection expanded to kinds:
    skills:     [vm-lifecycle]
    commands:   [onboard-vm]
    subagents:  []
    hooks:      []
    mcp:        []
    tags:       [k8s]
    exclude:    []
    harnesses:  [factory]       # optional override; else inherits top-level
```

Machine-scope (`~/.agents/config.yaml`) gains the same shape. Top-level `harnesses:` is **required** at v2; loader errors hard if absent, suggesting `agent-marketplace migrate` (small helper that rewrites v1 configs).

## Package-level refactor map

```
internal/
├── catalog/        NEW — wraps hcc.IngestBundle, produces Catalog per registry
│                   Entry { Kind, Name, Description, Tags, Resource ir.Resource, Path }
├── resolver/       EXTENDED — resolves at (Kind, Name) granularity; tag match across all kinds
├── materialize/    REPLACES internal/sync — drives hcc.Compile + hcc.Materialize per target
├── config/         schema.go extended with Harnesses, per-kind selection, source_harness
├── skill/          DELETED (or kept only as a thin shim exporting types for TUI catalog)
└── harness/        NEW — small mapping layer: target name ↔ install root (repo & machine)
```

**Dependency:** use the embedded `pkg/harness-compilation` package from the root `github.com/Adyen/agent-marketplace` module; do not add an external HCC module dependency.

## Install-root layout per target

```go
// internal/harness/roots.go
type Roots struct{ Repo, Machine string }

var Roots = map[hcc.Harness]Roots{
  hcc.HarnessClaude:   {Repo: ".claude",   Machine: filepath.Join(home, ".claude")},
  hcc.HarnessFactory:  {Repo: ".factory",  Machine: filepath.Join(home, ".factory")},
  hcc.HarnessCursor:   {Repo: ".cursor",   Machine: filepath.Join(home, ".cursor")},
  hcc.HarnessWindsurf: {Repo: ".",         Machine: home}, // Windsurf lives under .windsurf/
  hcc.HarnessGemini:   {Repo: ".gemini",   Machine: filepath.Join(home, ".gemini")},
}
```

HCC emits paths relative to these roots; we pass them straight to `hcc.Materialize(outDir=root, WriteModeSync)`.

## Core sync loop (new `internal/materialize`)

```go
func Run(ctx context.Context, repoRoot string, resolved []resolver.ResolvedResource) (*Report, error) {
    perTarget := map[hcc.Harness][]hcc.Artifact{}
    var diags []hcc.Diagnostic

    for _, r := range resolved {
        for _, h := range r.Harnesses { // filtered in resolver
            res, err := hcc.Compile(r.Resource, h)
            if err != nil { return nil, err }
            perTarget[h] = append(perTarget[h], res.Artifacts...)
            diags = append(diags, res.Diagnostics...)
        }
    }

    for h, arts := range perTarget {
        outDir := harness.Roots[h].For(r.Source.IsMachineScope(), repoRoot)
        _, err := hcc.Materialize(
            &hcc.Result{Artifacts: arts},
            outDir,
            hcc.WithWriteMode(hcc.WriteModeSync), // prunes stale — replaces today's orphan cleanup
        )
        if err != nil { return nil, err }
    }
    // ...collect written/skipped/removed into Report, attach diags
}
```

`WriteModeSync` + `WithPruneStale` preserves today's "config is truth; disk is ephemeral" invariant for free.

## Catalog builder (new `internal/catalog`)

```go
func Load(entry config.RegistryEntry, cachePath string) (*Catalog, error) {
    srcFS := os.DirFS(entry.SkillPath(cachePath))
    from := hcc.Harness(entry.SourceHarness)
    if from == "" {
        var ok bool
        from, ok = hcc.Autodetect(srcFS)
        if !ok { /* fall back to claude + info diag */ }
    }
    bundle, err := hcc.IngestBundle(srcFS, from)
    if err != nil { return nil, err }

    entries := make([]Entry, 0, len(bundle.Resources))
    for _, r := range bundle.Resources {
        entries = append(entries, Entry{
            Kind:        r.Kind(),
            Name:        r.Name(),
            Description: descOf(r),
            Tags:        tagsOf(r), // from Extra["tags"] / metadata.tags
            Resource:    r,
        })
    }
    return &Catalog{Entries: entries, Diagnostics: bundle.Diagnostics}, nil
}
```

## CLI surface changes

- `list` grows a `KIND` column; `--kind {skill,command,subagent,hook,mcp}` filter.
- `install <name>` infers kind by name uniqueness across catalog; ambiguous → requires `--kind`.
- `install --tag <tag>` works across all kinds (honors `metadata.tags` on each resource).
- `remove` unchanged semantically; same polymorphic rules.
- `status` gains `HARNESS` and `DIAGNOSTICS` columns; shows per-target materialization report.
- NEW `agent-marketplace convert --from <harness> --to <harness> <path>` — thin `hcc.Convert` wrapper for power users / debugging.
- NEW `agent-marketplace doctor` — reports detected harnesses, writable roots, HCC version, diagnostic-code catalog link.
- REMOVED: the implicit `.agents/skills/` destination. If needed as a compatibility emit for non-native harnesses, ship it behind `harnesses: [agentskills]` — but that is a stretch goal, not v2-blocking.

## Phased rollout

| Phase | Scope | Exit criteria |
|---|---|---|
| **0** | Embed HCC under `pkg/harness-compilation`; add the new `internal/harness` package; `internal/catalog` wraps `hcc.IngestBundle` and is used only by `list` and TUI catalog. Resolver and sync untouched. | Listing works; existing symlink sync still functions. Regression tests pass. |
| **1** | Schema v2: add `harnesses:`, `source_harness`, per-kind selection. Version gate: `apiVersion: agent-marketplace/v2` required. Migration helper `agent-marketplace migrate`. | New configs load; old ones fail with a pointer to `migrate`. |
| **2** | Resolver upgrade: `ResolvedResource{Kind, Name, …, Resource}`; cross-layer excludes and tag selection work across kinds. | Policy matrix tests extended per kind. |
| **3** | Replace `internal/sync` with `internal/materialize` using `hcc.Compile + hcc.Materialize(WriteModeSync)`. Per-target install roots wired through `harness.Roots`. Delete symlink code paths. | End-to-end: installing a Claude-format skill into a Factory + Cursor target produces `.factory/skills/<n>/SKILL.md` and `.cursor/skills/<n>/SKILL.md` with proper pruning. |
| **4** | CLI polish: `--kind`, `KIND`/`HARNESS`/`DIAGNOSTICS` columns, `convert`, `doctor`. README + migration guide rewrite. Bump to `v2.0.0`. | CHANGELOG + package manager rollout notes. |

Phases 0–1 are non-breaking (can ship inside v1.x). Phase 2 onward is v2.

## Decisions I want you to confirm (or override)

1. **Scope of v2.0 cut**: do we ship all 5 resource kinds on day one, or skills-only via HCC first (to validate the pipeline) and flip on the other kinds in v2.1? Shipping skills-only first is lower-risk; all-kinds is more honest.
2. **Default target if `harnesses:` is absent**: hard error and force migration, or silently default to the single harness detected in the repo? I propose hard error — it's a major version bump and silent defaults cause drift.
3. **`agentskills` pseudo-target** (materializes to `.agents/skills/` in agentskills.io format, for non-native harnesses): include in v2.0, defer to v2.x, or drop entirely? Today's users depend on this path implicitly.
4. **Module path for HCC**: HCC is embedded under `pkg/harness-compilation` in the root `github.com/Adyen/agent-marketplace` module. Imports use `github.com/Adyen/agent-marketplace/pkg/harness-compilation`.

Happy to pick defaults on any of these — just flagging the branch points.
