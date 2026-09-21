# Harness model catalogs and reasoning conversion

Date: 2026-09-21
Status: approved by the user on 2026-09-21; generic implementation complete, with new Devin forward variants still gated by D1.
Baseline: `d688dce101c4d2bddc434d8a2665ea9d1874ab64`.
Audience: harness-compilation maintainers. This is the design reference; the library README contains the implemented maintenance workflow.

## 1. Contract and scope

Model selection and reasoning are one translation concern. The compiler decodes the source representation into an intent, resolves that intent against the destination catalog, and emits native fields with explicit loss diagnostics.

Approved product requirements:

- An unresolved model produces a warning and an artifact **without the model field**. Model resolution alone never suppresses the resource or fails sync.
- Unsupported reasoning produces a warning and loses only the unrepresentable preference. A resolved model remains where possible.
- Omission intentionally accepts the destination harness's runtime default. Diagnostics describe that consequence; omission is not described as preserving inheritance.
- There is no automatic cross-model or nearest-effort substitution. `max`, `xhigh`, `off`, `none`, and `dynamic` are not interchangeable.
- Model releases are maintained in shipped per-harness catalog data, not Go switch statements. Ordinary additions require catalog and fixture changes only.
- There is no new Marketplace user/project configuration, network discovery during compilation, runtime catalog refresh, or dependency on an installed harness.
- Source-native model selections survive same-harness conversion without requiring a Claude equivalent.

The first implementation covers subagent model/reasoning conversion across all registered targets and uses the same resolver for documented Claude/Devin typed skill model conversion. Other skill field policies remain unchanged with their existing coverage limitations; this work does not establish new support or remove undocumented skill fields. Adding typed skill effort, command model fields, or new harness resource primitives is not part of this change. Existing security-related resource suppression and no-primitive behavior are unchanged.

Non-goals include benchmarking model quality, cost-based selection, account availability checks, provider configuration, session settings generation, Gemini thinking settings aggregates, and a new user-facing explanation command. Resolution results retain enough information for diagnostics and tests without adding a CLI surface.

## 2. Native contracts and evidence

Evidence was reviewed on 2026-09-21. Documentation, source inspection, catalog discovery, and runtime acceptance are distinct evidence classes. Syntax support is not proof of account entitlement or model availability.

### Subagents

| Harness | Model selection | Reasoning representation | Evidence / constraint |
|---|---|---|---|
| Claude | `model`: family alias, full ID, or `inherit`; omission inherits | Separate `effort`; supported levels depend on model | [Claude subagents](https://code.claude.com/docs/en/sub-agents.md) documents `low`, `medium`, `high`, `xhigh`, `max` and session inheritance. |
| Factory | `model`: public ID, configured `custom:` ID, or `inherit`; omission inherits, subject to native routing | Separate `reasoningEffort`, compatible with the selected model | [Factory subagents](https://docs.factory.ai/harness/subagents) says effort is ignored with `inherit`. Its subagent table names low/medium/high; the broader [model catalog](https://docs.factory.ai/docs/models) lists additional session/CLI levels. Do not infer custom-droid acceptance of all session levels from that table alone. |
| Cursor | `model`: ID or `inherit`; omission inherits | Parameters embedded in model string, for example `claude-opus-5[effort=high]` | [Cursor subagents](https://cursor.com/docs/subagents) documents bracket syntax. Parameter names and values depend on the model. Empty brackets and unrelated parameters have meaning. |
| Gemini | `model`: ID or `inherit`; omission inherits | No reasoning field in the inspected agent Markdown schema | [Gemini subagents](https://geminicli.com/docs/core/subagents/) and [pinned loader](https://github.com/google-gemini/gemini-cli/blob/cfbcaa8df13ea4610bb379b377b56d62980c0032/packages/core/src/agents/agentLoader.ts). `localAgentSchema` is strict and has no `effort` or `thinkingConfig`; `markdownToAgentDefinition` uses `markdown.model || 'inherit'`. |
| Devin | Custom profile `model`; omission selects the configured default subagent model, not the parent | Catalog lists model variants containing thinking levels; acceptance of those UIDs in custom profiles is not yet established | [Devin subagents](https://docs.devin.ai/cli/subagents), [models](https://docs.devin.ai/cli/models), and the capture below. No documented custom-profile `inherit` sentinel or independent effort field was found. |
| Codex | Optional `model`; omission inherits | Optional `model_reasoning_effort`; omission inherits when model is also inherited; an explicitly selected model can use its own default | [Codex subagents](https://developers.openai.com/codex/subagents.md). No literal `model = "inherit"` is needed to express inheritance. |
| Windsurf | No compiler-supported subagent primitive | Not applicable | Existing compiler no-primitive contract remains unchanged; no model resolver may invent a subagent artifact. |

The current Gemini emitter writes `effort`, which the inspected upstream schema rejects. The current Cursor emitter also writes a standalone `effort` rather than the documented bracketed representation. These are encoding defects, not missing model rows.

### Model-only skills

[Claude skills](https://code.claude.com/docs/en/skills.md) and [Devin skill authoring](https://docs.devin.ai/cli/extensibility/skills/creating-skills) document model overrides. Devin's skill omission uses the current/profile model, unlike omission in a custom subagent definition. Capabilities must therefore be keyed by resource surface, not only by harness.

The reviewed [Factory](https://docs.factory.ai/harness/skills), [Cursor](https://cursor.com/docs/skills), and [Gemini](https://geminicli.com/docs/cli/creating-skills/) skill references do not establish a skill model override. This is an evidence gap, not proof that every version rejects the field. The initial model resolver must not claim those surfaces support model selection merely because their subagents do. Existing undocumented field behavior is explicitly identified in the implementation review rather than silently promoted to a new contract. Source skill-directory symlink installation remains distribution policy: this design does not rewrite source trees or change installation mode to force skill conversion.

### Devin capture and unresolved gate

Installed CLI: `devin 3000.10.31 (b98cc431)`. Authenticated `devin models list --format json` returned:

| Family UID / slug | Variant UID | Variant label |
|---|---|---|
| `gpt-5.6-luna` | `gpt-5-6-luna-medium` | GPT-5.6 Luna Medium Thinking |
| `gpt-5.6-terra` | `gpt-5-6-terra-high` | GPT-5.6 Terra High Thinking |

Both families also listed low, medium, high, xhigh, and max variants. The capture establishes identifiers in an account-visible catalog, not accepted custom-profile syntax. The repository's [Devin validation guide](../harness-validation/devin.md) records no standalone metadata-only custom-profile inventory. No new profile, model turn, trust mutation, or authentication change was performed.

**Gate D1:** new Devin variant mappings require first-party confirmation that custom-profile `model` accepts these UIDs, source-backed resolution evidence, or an explicitly authorized isolated native verification. Until then, example entries below remain candidate data, not enabled verified mappings. If D1 remains blocked, the compiler still warns and emits without an unresolved model as required. The original Luna/Terra warning examples cannot be claimed fixed by catalog discovery alone.

## 3. Module and data flow

Proposed module: `pkg/harness-compilation/modelpolicy`.

Its interface accepts source harness, resource surface, model string, optional separate effort string, destination harness, and existing invocation overrides. It returns native model/effort values, semantic outcome, and structured losses. It has no filesystem, network, environment, executable, or logging dependency.

```text
native ingestion + source provenance
    -> modelpolicy.Decode(source, surface, model, effort)
    -> Selection + Identity + Reasoning
    -> modelpolicy.Encode(target, surface, intent, overrides)
    -> native values + losses
    -> existing target serializer + diagnostic adapter
```

`Resolve` is the normal combined operation. Decode/encode share immutable indexes built once from embedded catalogs. Target serializers retain YAML/TOML formatting and artifact paths; no target reimplements model recognition, effort fallback, or loss policy. Direct target compiler entry points must use the same resolver as `Compile` and `CompileBundle`.

### Internal intent

- **Selection:** inherit parent, harness default, or explicit selection. An absent input is interpreted using its source surface; it is not always inheritance.
- **Identity:** provider-qualified concrete model identity, provider-qualified floating family selector, or opaque native identifier scoped to its source harness.
- **Reasoning:** unspecified, recognized named preference for that model, or opaque native setting. Names are catalog data, not a closed Go enum requiring changes for each new level.
- **Original representation:** retained for same-harness spelling fidelity and unrecognized native values. It is not an independent editable configuration.

Named preferences only join across representations of the same model/selector with catalog evidence. This is not a universal compute scale. A token budget is not converted to a named level. Unrelated Cursor parameters, fast variants, context sizes, and composite/fusion models are not stripped or reclassified by suffix heuristics.

### IR authority and compatibility

`Skill.Model` and `Subagent.Model`/`Effort` remain the authoritative editable strings. Add optional `ModelSource` provenance to the relevant IR types; the empty value denotes legacy canonical input. New ingestion retains native strings and sets their source harness rather than normalizing exact models into floating Claude aliases. Decoding into typed intent happens inside the resolver, so there is no second editable model-selection object or stale cached interpretation.

Existing constructor signatures and compile entry points remain unchanged. A caller changing `Model` or `Effort` changes the next resolution; old decoded state cannot override it. Adding source provenance is additive at the type level, but the raw values returned by affected ingesters intentionally change. That behavioral change must be documented, tested, and isolated from legacy digest verification.

Existing `Ext.Claude`, `Ext.Cursor`, `Ext.Codex`, and `Ext.Factory` model/effort fields remain readable for older payloads and callers. For legacy input without `ModelSource`, the compatibility adapter applies the existing target-native sidecar precedence, then constructs one resolver request. A single unambiguous source sidecar can supply source-native provenance for cross-target conversion; conflicting native source sidecars without explicit provenance produce a warning and fall back to the canonical fields. New ingestion does not populate duplicate model/effort sidecars. For input with `ModelSource`, current top-level model/effort strings take precedence over stale legacy sidecars. Other sidecar fields are unaffected.

Legacy canonical input recognizes the existing family vocabulary through declarative bindings and concrete provider IDs known to the catalogs. Source harnesses do not limit the provider vocabulary: a Claude-layout resource naming the known `gpt-5.6-luna` must still resolve to that OpenAI identity when targeting a harness that supports it. This fallback uses exact known provider model names only, never fuzzy string rewriting or arbitrary cross-harness aliases.

## 4. Shipped catalog schema

Location: `pkg/harness-compilation/modelpolicy/catalogs/<harness>.yaml`.
Format: embedded YAML, schema version `1`, decoded strictly using the existing YAML dependency. Catalogs are library data, not tool configuration or Marketplace config files.

One file contains the harness's surface declarations, model records, family selectors, and source evidence. All Go code uses the same loader and indexes. No source-by-destination matrix or separately authored reverse table exists.

### Example: separate reasoning field

This is a schema illustration, not a complete shipped inventory:

```yaml
schema: 1
harness: codex
evidence:
  checked: "2026-09-21"
  refs:
    - https://developers.openai.com/codex/subagents.md
surfaces:
  subagent:
    model_field: model
    effort_field: model_reasoning_effort
    omitted_model: inherit
    inherit_model: ""
    inherited_efforts: [low, medium, high]
models:
  openai/gpt-5.6-terra:
    model: gpt-5.6-terra
    reasoning:
      low: {effort: low}
      medium: {effort: medium}
      high: {effort: high}
```

### Example: reasoning encoded in a model

Candidate Devin data, subject to D1:

```yaml
schema: 1
harness: devin
evidence:
  checked: "2026-09-21"
  refs:
    - https://docs.devin.ai/cli/subagents
surfaces:
  subagent:
    model_field: model
    omitted_model: default
models:
  openai/gpt-5.6-luna:
    model: gpt-5.6-luna
    reasoning:
      low: {model: gpt-5-6-luna-low}
      medium: {model: gpt-5-6-luna-medium}
      high: {model: gpt-5-6-luna-high}
      xhigh: {model: gpt-5-6-luna-xhigh}
      max: {model: gpt-5-6-luna-max}
```

The base `model` spelling requires acceptance evidence too. A variant-only record may omit the base model: it resolves only when a supported reasoning selection identifies a complete encoding. Unspecified effort must not arbitrarily choose the first variant.

Cursor uses the same shape, for example `high: {model: "claude-opus-5[effort=high]"}` beneath `anthropic/claude-opus-5`. No generic period-to-hyphen replacement, appended reasoning suffix, parameter-name guess, or executable template is allowed. Explicit spellings are inexpensive data and avoid accidental model changes.

### Field definitions

| Field | Meaning |
|---|---|
| `schema` | Required integer `1`; unknown versions rejected. |
| `harness` | Required registered target name; must agree with filename. |
| `evidence` | Required checked date and nonempty first-party refs; per-record evidence may override it. These are provenance assertions, not automatic proof. |
| `surfaces` | Model-bearing resource surfaces. Lack of a surface is not permission to invent fields. |
| `model_field` | Native field name supported by that serializer. Not an arbitrary output path. |
| `effort_field` | Optional native separate reasoning field. Absence means reasoning needs a model encoding or is unsupported on this surface. |
| `omitted_model` | Required `inherit` or `default`, describing actual native omission semantics. |
| `inherit_model` | Optional native inheritance sentinel; empty string means omission represents inheritance and is valid only when `omitted_model: inherit`. Missing means no established inheritance representation. |
| `inherited_efforts` | Optional explicit list of separate effort values legal with inherited selection. Absent or empty means no independently representable effort. Native model-dependent validation still belongs to the harness; the compiler does not know the parent's live model. |
| `models` | Concrete identities keyed by `<provider>/<model>`. The model portion is a known provider spelling, never a capability tier. |
| `model` | Optional native base spelling. Does not encode a newly selected effort unless explicitly represented as a native-only identity. |
| `aliases` | Optional additional spellings of exactly the same selection, for ingestion only. They may not turn a concrete model into a floating family. |
| `reasoning` | Map of model-specific named preferences to exact native `{model, effort}` overrides. A missing `model` reuses the base spelling; a missing `effort` emits no separate effort field. |
| `families` | Floating family selectors, separate from concrete models. Each declares native `model`/`aliases` or an explicit `pin` to a concrete identity in this catalog. |
| `pin` | Output-only family binding for harnesses lacking an equivalent floating alias. Emits a selector-pinned informational diagnostic. Never reverse-maps the concrete model back to the family. |

Catalog syntax is deliberately small. Overrides may change model and effort only. It cannot write security fields, select artifacts, execute commands, or mutate harness settings. In this version, `reasoning` entries apply to subagents only; model-only skill conversion uses base/family/native selections without introducing a skill effort field. A future typed skill-effort feature must deliberately extend the surface contract.

### Index and validation rules

- Unknown schema keys, duplicate mapping keys, duplicate/case-colliding modeled keys, invalid target/surface names, empty identifiers, control characters, malformed native-field declarations, and broken family pins fail catalog verification.
- Family selectors and concrete identities cannot collide. Alias matching is exact and case-sensitive; no global punctuation normalization applies to native IDs.
- Every native base or complete reasoning encoding must reverse-resolve unambiguously within its harness and surface. Aliases with identical meaning are allowed; different meanings sharing an encoding are rejected.
- Portable unqualified provider names are indexed from concrete identity keys. If several providers claim the same name, only a provider-qualified input resolves globally; no first-row-wins behavior is allowed.
- A reasoning entry must form a complete output and may not request a separate effort field on a surface that has none. Model-specific inventory and inherited-effort inventory are validated independently.
- A floating native alias remains floating; a concrete version is never rewritten to a latest-family alias. A family pin is an explicit catalog policy, not an inferred cross-version equivalence.
- Catalogs contain only enabled, evidence-backed mappings. Pending proposals such as D1 live in the design/test planning evidence, not in the runtime catalog behind a silently ignored flag.

## 5. Resolution order and losses

1. Validate/normalize the caller's legacy sidecars at the single compatibility seam. Resolve source omission and inheritance using the source surface; legacy canonical omission retains historical parent-inheritance intent.
2. Decode an exact source-native encoding or alias. If absent, resolve a provider-qualified identity or unambiguous exact provider model name known to the catalog union. Otherwise retain opaque source-native provenance.
3. Honor an existing `WithModelOverride` mapping for the current authored model key. It authorizes the exact replacement, including an empty replacement. Do not infer reasoning support for an unknown override value. The option remains invocation-specific; no new runtime config interface is added.
4. Preserve valid same-harness native selections, including opaque model strings, without requiring them to be present in the release's catalog. Preservation claims spelling fidelity, not current vendor acceptance. Known-invalid field encodings, such as Gemini top-level effort, are not replayed as if they were valid native fields.
5. For cross-target resolution, look up the same concrete identity or family selector in the destination catalog. An opaque identifier does not pass through merely because both targets expose a string field.
6. Resolve the requested reasoning jointly with that model. Exact declared representations succeed without loss diagnostics. No nearest-level fallback exists.
7. If reasoning is unsupported, retain the destination base model when available, omit reasoning and warn. If only reasoning-specific variants exist and none matches, omit the model too rather than choose an arbitrary variant.
8. If the model is unresolved, omit it and warn. A dependent reasoning preference is also omitted with a reason. Separate reasoning on an inherited/default model is retained only where the surface explicitly permits that named value.
9. If inheritance cannot be represented, omit the model and warn about the actual target-default behavior. Devin-to-Devin omitted models do not warn; Claude-to-Devin omitted or explicit inherited selection warns once. Conversely, a source harness-default selection converted to a target whose omission means inheritance also warns: a target default cannot recreate another harness's configured default model.

For a known embedded reasoning variant plus an explicit conflicting separate effort, the explicit separate value is the requested preference. Resolution emits a conflict warning, then uses its exact encoding or the normal unsupported-effort fallback. It never silently retains the embedded level while claiming the explicit one was applied. Unknown parameterized native strings remain opaque: same-harness preservation keeps all parameters, cross-target conversion warns and omits the model rather than discarding unmodeled parameters.

No model-policy loss has error severity. Diagnostic attachment supplies resource name, source location, target, original request, omitted fields, and fallback effect. Catalog load/validation defects are programming/package errors, not unresolved-model cases; tests must prevent distributing invalid catalog data.

Existing diagnostic constants remain available. Existing Devin unmappable/default/effort codes remain for equivalent losses, with accurate messages instead of an alias-only prescription. New shared codes distinguish model omission, inheritance/default mismatch, reasoning omission, and conflicting reasoning. Automatic Codex-native loss diagnostics move behind resolution so successfully converted native values do not also report themselves dropped. Security-related guards remain in place. One structured loss is adapted once; wrappers must not duplicate it.

## 6. Persistence and compatibility

### Canonical artifacts

`artifact.resourceJSON` serializes exported IR fields and `DecodeResource` reconstructs them. New provenance must survive this path and remain reserved from `Extra`, including case aliases. Missing provenance in existing payloads invokes the legacy adapter. New fields use `omitempty`; decoded old payloads must re-encode with their existing semantics, not invented source provenance.

An ingested native concrete model must not be collapsed into a family before canonical archival. This intentionally changes newly generated content digests for affected inputs. Published archives and exact pins are immutable and are not rewritten.

### Git source revisions

`internal/sourcerevision` normally hashes original source-file bytes. When those bytes are unavailable it falls back to IR JSON, where changing model normalization or adding provenance can invalidate old locks. Source-revision v4 captures model provenance in that fallback and includes bare-bundle MCP resources; v1/v2/v3 retain frozen legacy projection without changing original-file hashing. This source-revision version is independent of config API version and lockfile schema version.

The legacy verification adapter must reproduce the historical model/effort projection for every supported old revision, using frozen legacy model data rather than the evolving catalog. Normal runtime compilation consumes current source-aware resources after existing exact-source verification. Existing locks are not refreshed solely because the compiler gained a catalog; explicit lock upgrades can adopt the new generator. Compatibility fixtures must include known Factory/Cursor exact models, Devin aliases, Codex sidecars, and model-less agents, not only fixtures with empty model fields.

Old archives that already lost concrete identity cannot recover it by guessing. They remain legacy canonical input until their source is explicitly republished or reacquired through the supported update workflow.

### Installation and output changes

Materialization already compiles attached resources before comparing output. Catalog updates may change generated bytes and therefore legitimately appear as drift. A second sync with unchanged inputs must be a byte/mtime no-op. Catalog contents affect compiler output, not the identity of the original source revision. No new aggregate, root, installation mode, trust setting, or ownership authority is introduced.

Source skill-directory symlinks remain untouched; catalog resolution is not permission to turn symlink mode into file mode. Consumer acceptance for this feature specifically includes compiled subagent paths in both installation modes.

## 7. Maintenance and verification contract

The implementation supplies one contributor gate, proposed as `make model-catalog-check`. It validates all catalogs and runs independently authored model-resolution/serialization fixtures. It makes no vendor calls, performs no automatic fixture rewrite, and needs no credentials. A normal new model release changes its harness YAML and a small fixture file; a newly invented native field format still requires deliberate Go support and evidence.

Tests must assert observable outputs and loss codes, not compute their expectations from the catalog under test:

- All harnesses: inherit, omitted selection, explicit concrete model, family alias, unsupported model, supported/unsupported reasoning, and native opaque same-target round trip.
- Exact identity remains exact; floating identity remains floating or reports an explicit catalog pin. Old Sonnet/Opus/Haiku versions never silently float to the latest generation.
- Factory/Codex separate effort, Cursor embedded effort, Gemini warning/drop, and gated Devin variants are separate expected-output fixtures through the public compiler.
- Regression examples: Luna + medium; Terra + high; repeated inherited agents; valid body retained with no model when resolution fails. D1 controls the Devin success expectations.
- Unknown model plus valid reasoning, variant-only models without effort, explicit versus embedded effort conflicts, unknown model override values, empty overrides, ambiguous portable IDs, opaque custom models, and unknown Cursor parameters.
- Public paths: native loaders, direct IR constructors, target `Translate`, `Compile`, `CompileBundle`, conversion, canonical encode/decode, and Marketplace materialization. A wrapper-only fix fails acceptance.
- Source revision legacy vectors and verified exact locks survive; new provenance reaches hosted artifact decode; malformed/case-colliding provenance is rejected rather than hidden in extras.
- Hermetic CLI integration: warning-only unknown subagent model exits successfully and emits the body/tools without model; supported siblings emit; repeated sync/check agrees; security-restricted resources retain existing fail-closed behavior.
- Catalog indexes are deterministic and initialized once. Baseline and post-change compile/resolver benchmarks cover known, unknown, inherited, and variant selections with allocations reported. No performance improvement is claimed without measurement.

Existing library tests, affected materialization/source-revision tests, `make harness-compat-check`, and `make harness-compat-config` remain required according to the repository workflow. The compatibility E2E-based gates run sequentially because they share a test binary. Native validation remains in the existing isolated CI process; generated-file assertions are not relabeled native verification.

## 8. Review and implementation gates

The user approved the concrete design and bounded skill scope before implementation. The generic resolver, catalogs, provenance handling, compatibility projection, and offline maintenance gate are implemented. No runtime configuration surface was added.

D1 remains open: acceptance evidence is required before enabling the proposed Devin forward variant mappings. This does not block the generic implementation, but does block claiming the Luna/Terra-to-Devin examples are fully corrected. Native same-harness preservation is a separate, tested contract and is not acceptance evidence.

Required before delivery: independent expected-output tests, legacy digest tests, consumer-path coverage, native evidence labels, and updated diagnostic/field-matrix reference. Existing approximate cross-provider mappings such as Sonnet to Gemini are intentionally removed from automatic conversion; explicit library overrides remain the escape hatch.

### Pinned planning context

All references are Blackboard project `agent-marketplace`, revision 1; these are planning context, not execution authority:

| Entry | Role |
|---|---|
| `c8bef23a-6cb0-4b68-a688-6495c249f39f` | Root objective and constraints |
| `ec9eef55-ed93-424a-b032-e76ed430a9c8` | Native-contract question; produces `native-contracts` |
| `1583f35b-df96-4b7a-9894-17e954a5355e` | Resolver/schema question; needs `native-contracts`, produces `resolution-design` |
| `fae64f3b-2387-4ed6-a604-6424f14629da` | Compatibility question; needs `resolution-design`, produces `implementation-handoff` |
| `d1894c5b-cef7-4025-b202-23ce4e7394ff` | Initial code/catalog observations |
| `161a3bde-591c-42d1-a06d-77ef19b18529` | User's warn/omit/resource-survives policy |
| `c25162c1-eece-4577-89e5-57aeceaa1aa8` | User's shipped-only catalog scope |
| `e3b3b761-8b1b-400c-a809-700df02ebeb4` | Cross-harness native-format findings |
