# Design

## Context

Harness targets are declared in `pkg/harness-compilation/target` (`target.Definitions()`), and installation routing lives in `internal/harness` (`InstallationAdapterRegistry`, the `TargetInfoMap` compatibility view, and native-layout adapters for Devin, Codex and OpenCode). No declaration states which resource kinds a target accepts; tests and completeness checks assume every target implements every kind.

## Decisions

1. **Declaration.** Add `AcceptedKinds []ir.ResourceKind` to the compiled `target.Definition`. Current targets declare `skill`, `command`, `subagent`, `hook`, `mcpserver` and `plugin`; Factory, Cursor and Windsurf also declare `rule`. `collection` is never declared.
2. **Composition seam.** Evolve `InstallationAdapterRegistry` to compose `ApplicationTarget` entries from `target.Definitions()` and the existing legacy and native-layout adapter factories. Expose `Lookup(id string) (ApplicationTarget, bool)`, `Targets() []ApplicationTarget`, and `ApplicationTarget.Accepts(kind ir.ResourceKind) bool`. `TargetInfoMap` stays a derived compatibility view. Devin, Codex and OpenCode stay adapter-backed with no legacy-metadata fallback. Do not add a third hand-maintained per-target map.
3. **Selectable view.** The application view intersects declared kinds with `config.AllKinds` minus `collection`, plus `plugin` when declared. Do not add `plugin` to `config.AllKinds` or create a public selector for it; plugin handling dispatches through `CompilePackage`, never leaf `Compile`.
4. **Completeness.** Registry/completeness checks require compiler, ingester and catalog registrations only for declared capabilities. Hook catalogs stay scoped to hook translation, plugin catalogs to package projection, model/tool catalogs to their translation surfaces. Every current compiled target keeps every current catalog and native-probe requirement.
5. **Rules.** `rule` acceptance means the target emits rule artifacts. Diagnostic-only rule translation on other targets is unchanged and is not acceptance.
6. **Refusal.** A partial target (test-only synthetic definition) refuses an unaccepted kind before compile or write with a diagnostic naming the kind and target.
7. **Out of scope.** Profile-only targets are not constructed, and `target.Definitions()`/`CompilerTargets()` stay compiled-only. `target.ValidateDefinition`'s paired-root check stays a compiled-target constraint.

## Preserved behavior (regressions)

Factory forced command copies, Devin and Codex converted command skills, Codex partitioned destinations, Devin scope-varying hook envelopes, and OpenCode native/owned aggregate paths keep byte-identical output. The existing tests named in `docs/plans/2026-11-13-target-profiles-design.md` ("Bounded `generalize-target-kinds` brief") keep passing; `TestGolden_AllTargets` compares unfiltered output trees and `TestGolden_PerResourceKind` keys its positive cases to `AcceptedKinds`.

## Risks

- A derived view could silently drop `plugin` if derived from `config.AllKinds` alone; the matrix test pins it.
- Weakening catalog checks while making them capability-specific; the existing catalog tests stay unchanged for every compiled target.
