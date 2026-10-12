## Context

See [proposal.md](proposal.md) for motivation and the delta specs for externally visible requirements. At the approved base, `Resolve` already orders direct and required selections, expands collections, and validates prerequisites against selected kind/name pairs. Catalogs for configured Git and directory sources are available during resolution. `Via` is consumed as collection provenance. Materialization classifies proven owned outputs absent from desired state as stale and reconciles shared aggregates against desired entries.

This slice is approach A: add requirement expansion after collection expansion and required-shadow handling without synthetic registry selections or Marketplace acquisition. The release-N default remains disabled.

## Goals / Non-Goals

**Goals:**

- Preserve the existing resolver entry point and collection behavior while supporting opt-in closure and advisory preview.
- Keep lookup bounded to configured, already-discovered catalog sources and apply the existing selection, scope, and harness-target policy.
- Retain only matching existing preview outputs during materialization without adding them to desired writes or weakening ownership safety.
- Keep settings reads local and keep the acquisition-lock format and bytes unchanged.

**Non-Goals:**

- Marketplace requirement lookup, pins, or requirement-edge writes; new acquisition; N+1 default changes; slice-2 command or TUI surfaces; hooks or MCP servers authoring prerequisites; and cascading removal.

## Decisions

### Keep expansion inside resolver phases

Add expansion after collections and required-shadow handling, and before unmet-prerequisite validation. Reuse the already discovered catalog indexes through a lookup boundary that distinguishes a usable candidate, an absent candidate, an excluded candidate, and a source hint with no configured match. Without a hint, lookup uses the requiring resource's exact source URL, ref, and root. Git hints match only configured sources using `NormalizeGitURL`; an optional path narrows the configured root and a failed match has no fallback. Directory hints match only an absolute cleaned configured directory location. Lookup performs no fetch or source addition.

The walk is deterministic: begin with the existing resolved order, process each declaration in order, and track visits by effective scope, kind, and name. Any resource actually selected in the same resolution with matching kind and name satisfies a prerequisite, regardless of source, layer, scope, or Marketplace selection. Scope and effective harness targets control only admission of newly implied resources; direct selections win collisions. Expansion applies required and exclusion precedence and admits implied resources per requiring scope with the supplying entry's targets intersected with the effective target set. This avoids creating synthetic selections and keeps collision, scope, and exclusion decisions in the established resolver phases.

### Preserve the existing resolver API and provenance distinction

Keep the `Resolve` signature. Add `WithImpliedPreview(*[]ResolvedArtifact)` as an optional output-capture option and expose the captured values as `ResolveResult.ImpliedPreview`. Add `RequiredBy []ResourceRef` to `ResolvedArtifact`, where each reference contains an `ir.ResourceKind` and name. Keep `Via` collection-only. When expansion is enabled, append implied resources to the actual resolution result; when disabled, capture the same closure separately without changing actual selections or configuration.

Extend `UnmetRequirementWarning` with a typed reason for not-found, excluded, or source-not-configured. Keep current warning text, credential redaction, shell-literal remediation, and strict-sync decision path; after expansion, only genuinely unmet requirements remain warnings.

### Model the setting without losing explicit false

Add `config.RequiresConfig` with `Expand *bool` to the v4 configuration model and `config.RequiresExpansionEnabled() bool` to read the effective value with a false default. Merge the pointer through the existing organization, user, repository, personal-local, and recursive-include settings paths so explicit false is not confused with absence. v2/v3 reads use the release default; migration does not write the key. Settings reads must not enter remote acquisition or open acquisition locks.

### Make preview preservation a separate materialization input

Add `WithImpliedPreview([]resolver.ResolvedArtifact)` as a preservation-only `RunOption`. Compile preview artifacts separately from selected desired artifacts, and pass them to reconciliation without appending them to desired writes.

For each admitted scope and harness target, derive the exact existing managed file, link, manifest path, or shared-aggregate identity associated with the preview. Preserve only matching proven ownership and existing aggregate values; do not synthesize missing output, rewrite a preview value, or adopt handwritten entries. Keep preview identities out of other owners' desired state, and retain normal pruning for unrelated stale outputs. Apply the same identity filter to shared JSON and Codex TOML aggregates. Missing or corrupt ownership and malformed aggregate input continue through existing fail-closed paths. When no selected resource implies an output, do not pass it as preview so ordinary cleanup resumes.

Plain sync, locked sync, and check-only sync use this same preservation-only input. The option changes neither selection nor acquisition locks, and check-only mode remains read-only.

### Keep removal in the existing serialized config mutation path

When expansion is enabled, removal of an authorized optional prerequisite removes its direct selection if present and adds an effective explicit exclude through the existing serialized mutation. It does not remove requiring resources. With expansion disabled, removal does not add a requirement-specific exclude. Existing required-selection precedence and scope authorization remain the authority for whether configuration may change.

### Validate behavior rather than a new performance budget

Add focused resolver examples and property coverage for closure, cycles, collisions, source hints, exclusions, scopes, harness targets, direct winners, advisory selection invariance, and idempotence. Add configuration, materialization/ownership, pipeline, command, and local-Git E2E regressions that bind every changed scenario to a test annotation. Measure the existing resolver and materialization hot paths before and after with retained raw evidence and report observed deltas; no numeric performance threshold is introduced.

## Risks / Trade-offs

- **[Risk] Preview retention could preserve stale state too broadly or interfere with selected writes.** → Key preservation to exact currently admitted scope/target paths and aggregate identities, keep it out of desired writes, and prove unrelated stale outputs still prune.
- **[Risk] Cross-scope exclusions could leak into machine output.** → Apply existing per-scope policy to admission of newly implied resources while preserving global satisfaction by actual selected kind/name, and cover machine/repository collision cases.
- **[Risk] Advisory warnings or strict exit behavior could change when closure is computed.** → Keep actual selections separate from `ImpliedPreview` and retain the existing warning and strict decision path for only unresolved prerequisites.
- **[Risk] A malformed ownership record or aggregate could be misinterpreted as permission to preserve or adopt an entry.** → Reuse existing fail-closed parsing and ownership proof; preview is never evidence of ownership.
- **[Risk] Requirement resolution could broaden the trust boundary or disturb mixed-version lock state.** → Resolve only from configured, already-discovered sources and assert healthy acquisition-lock bytes remain unchanged.

## Migration Plan

No selection or lock migration is required. Unset and legacy configurations retain the false release default, and migration leaves `requires.expand` absent. Users opt in by authoring the v4 setting. Rolling back the resolver does not require rewriting selections; if a prior client rejects the newly authored v4 setting, remove that setting before using that client.
