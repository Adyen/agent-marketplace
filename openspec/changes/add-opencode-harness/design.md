## Context

See `proposal.md` for scope and motivation. The current target registry validates compiler/ingester completeness and native topology; Devin and Codex bind native installation and repair through validated installation adapters. Shared configuration already has durable ownership state, private safety backups, mutation sessions, generation checks, and journaled recovery. The `materialization` and `owned-entry-reconciliation` specifications remain applicable to OpenCode.

OpenCode v1.18.34 is the investigation baseline, not a certified supported version. The following primary-source observations constrain the design:

- [Skills](https://opencode.ai/docs/skills/) accept limited metadata; unknown frontmatter is not execution enforcement.
- [Configuration substitution](https://github.com/anomalyco/opencode/blob/v1.18.34/packages/opencode/src/config/variable.ts) expands environment values before JSONC parsing and then searches for file references. Environment expansion can therefore introduce syntax or file reads before typed validation.
- [Permission parsing](https://github.com/anomalyco/opencode/blob/v1.18.34/packages/core/src/v1/config/permission.ts) retains authored order; [native permission rules](https://opencode.ai/docs/permissions/) allow agent-level rules to override global rules.
- [Tool execution](https://github.com/anomalyco/opencode/blob/v1.18.34/packages/opencode/src/session/tools.ts) awaits tool callbacks; [plugin dispatch](https://github.com/anomalyco/opencode/blob/v1.18.34/packages/opencode/src/plugin/index.ts) does not await general event callbacks and can skip failed plugin initialization. Declared interfaces alone do not prove blocking behavior across normal tools, MCP tools, or subagent paths.
- [Configuration startup](https://github.com/anomalyco/opencode/blob/v1.18.34/packages/opencode/src/config/config.ts) has filesystem/dependency side effects. Even native parsing probes need hermetic execution.

These are source observations. Native execution proof is a required implementation deliverable.

## Goals / Non-Goals

**Goals:**

- Reuse the established pure translation, destination admission, ownership, and consumer-policy boundaries rather than introducing a parallel installation system.
- Put native execution feasibility first, before registering a publicly supported target that cannot meet its security contract.
- Keep all new executable behavior in a small reviewable adapter and expose registry-authored hook content as typed inspectable data.
- Make every security claim testable through the actual conversion, native installation, checking, and repair consumers with valid controls and negative-side-effect assertions.

**Non-Goals:**

- Running native executables or source plugins as part of production Marketplace operations.
- General plugin packaging or JS/TS decompilation, trust approval, OAuth/login, provider account management, consumer-wide permission changes, or shared instruction generation.
- Changing acquisition-lock formats, existing source-revision semantics, defaults, or other harness targets to accommodate OpenCode.
- Treating resource assessment acceptance, source template recognition, or successful config parsing as runtime safety or destination ownership proof.

## Decisions

### 1. Establish native feasibility before target registration

The first implementation phase creates controlled probes and records a versioned compatibility matrix using the verified pinned executable. Minimum hook scope is command-type `PreToolUse` with matcher, input, allow/block, and bounded failure behavior. Additional events/effects are admitted individually only after equivalent proof.

The matrix records the native version/executable digest, supported event and handler forms, tool consumer paths, lifecycle ordering, matcher vocabulary, input/output protocol, subprocess environment and working directory, timeout/cancellation behavior, activation failures, and supported runtime modes. Unsupported paths are named explicitly. No broad default permission grant or asynchronous notification fallback is allowed.

**Activation is a stop condition:** the v1.18.34 source permits skipped initialization and disabled external plugins. A runner that executes correctly only after successful activation does not prove required enforcement. The gate must establish that each supported required-enforcement configuration prevents protected effects when activation fails. If the native runtime cannot do that, target release remains blocked pending upstream support or a separately proven route; neither a warning nor dropping hooks completes this change. User choice fixes this release requirement, so implementation must not silently reduce scope.

Alternative considered: release skills/commands/subagents/MCP and postpone hooks. Rejected because proven hooks are required in the first release. Alternative considered: infer compatibility from callback types. Rejected because timing, exceptions, bypass paths, and activation failure are runtime behaviors.

### 2. Extend the authoritative registries and adapter composition

Add `TargetOpenCode`, compiler/ingester registrations, aliases, capabilities, UI order, `NativeLayout`, and complete installation/repair bindings through the existing validated composition. Source marker detection requires OpenCode-specific evidence. Catalog and consumer surfaces obtain identity and paths from this authority, not new scattered switches.

The declared topology uses native plural directories: skills, commands, agents, and plugins under repository `.opencode` and the supported machine OpenCode resource directory. MCP configuration is an explicitly mapped external aggregate, not a broad output root. `OPENCODE_CONFIG` and `OPENCODE_CONFIG_DIR` are resolved separately according to the pinned loader; unsupported relative or ambiguous destinations refuse. JSON/JSONC precedence and native config-defined/built-in identity collisions are checked before choosing output. Source-layout legacy singular aliases and compatible shared skills locations are ingested once using proven native precedence.

Where runtime overrides affect multiple scopes, resolve all active destinations before mutation, canonicalize their identities, and use existing overlap planning. Coalesce only compatible work; conflicts or inseparable scope-restricted operations refuse. Existing explicitly supported machine aliases remain usable; a repository override cannot silently confer authority outside its authorized scope.

Alternative considered: add OpenCode to the legacy single-root tables alone. Rejected because external configuration, precedence, repair, and overlapping roots need the validated adapter boundary.

### 3. Model execution fields explicitly and preserve literal/reference intent

Use strict native decoders for JSON/JSONC and YAML frontmatter, with duplicate/alias checks before lossy decoding. Reserve modeled execution fields even when empty, and keep arbitrary dictionary keys case-sensitive. Native sidecars carry representable fields only; extras cannot inject commands, events, handlers, matcher, prompt, timeout, or permission authority. Scalar emission uses the established Unicode-safe serialization.

Preserve permission order and explicit emptiness in typed data, including unknown tool mappings and native subagent role. If faithful enforcement cannot be emitted, suppress the resource using registered severity-error diagnostics. Ignored skill frontmatter is not a restriction mechanism.

MCP literals and intentional references remain distinct throughout ingestion and compilation. Do not substitute source references using the operator's environment or filesystem. Probe generated syntax through the native loader with synthetic environment/file sentinels, adversarial quoting, and supported reference combinations. If OpenCode cannot represent a literal safely or preserve authentication/execution restrictions, suppress the whole affected resource, not just the sensitive value.

Alternative considered: retain unknown security fields as metadata or omit one unsafe MCP value. Rejected because either can change effective authority while appearing successful.

### 4. Generate one fixed hook adapter with typed contributions

Use one versioned, dependency-free fixed adapter per resolved native resource destination, with declarations outside the autoloaded plugin directory and source-disambiguated support payloads. The compiler emits typed declaration fragments; bundle merging produces deterministic contribution order and the fixed runner artifact. Registry names, metadata, commands, and payloads are serialized as data, never interpolated into executable source. Commands execute only later in the native runtime under the proven handler protocol.

A recognized adapter template/version and validated declaration document support non-executing ingestion. A changed runner, unsupported version, arbitrary plugin, or ambiguous declaration produces a stable diagnostic. Template recognition or a source digest identifies source format only; destination overwrite/prune still requires the existing file/tree ownership policy. The adapter/declarations/support artifacts are planned as one logical installed generation, so refused support cannot publish a partially updated runner or declaration set.

Use existing hook source-root substitution and captured support-source admission. Machine-scope commands remain location-independent; repository commands retain authorized scope-relative behavior. Contributions retain selector/source/scope identity so collection expansion, removal, and ordinary pruning cannot erase surviving hooks. Coexisting handwritten plugins are not modified to create artificial parity.

The proposed runner policy is a 60-second default handler deadline, explicit positive timeouts up to 600 seconds, a 600-second total event deadline, 1 MiB serialized input, 1 MiB combined stdout/stderr per handler, and at most 64 handler invocations per tool event. These are explicit new conservative runner bounds, not claims about existing native defaults: they bound each stream, invocation count, and total time while allowing longer explicitly declared handlers. Explicit declarations exceeding supported bounds fail compilation with `hook.opencode.unsupported`; runtime overflow blocks required protected effects and cleans the attempt's process tree. Source-support admission retains existing applicable read policy rather than inventing a separate aggregate source budget. If feasibility proof requires a different runner policy, revise the spec, design, and boundary cases before enabling support; do not silently truncate or weaken restrictions.

Alternative considered: arbitrary generated plugins per resource or copied npm plugin packages. Rejected because they enlarge the executable/review surface, complicate additive ordering, and introduce dependency installation. Alternative considered: parse arbitrary plugin source heuristically. Rejected because executable behavior cannot be recovered safely by importing or guessing.

### 5. Reuse complete filesystem and ownership mutation boundaries

Resource and support reads, staged output, publication, and attempt-owned cleanup use captured directory/file authority. Validate opened regular singly-linked payloads, admitted roots and ancestors, unchanged parent/leaf generations, and portable ownership manifests before reading claims or publishing. Use applicable existing read budgets and bounded readers, including alternate loaders and growing files. Do not replace descriptor admission with lexical containment, `Lstat` then path reopening, or rename-only reasoning.

MCP uses an OpenCode-specific formatting-preserving JSON/JSONC patcher behind the durable ownership store. State remains outside repository/cache and stores identity/value digests rather than raw payloads. No in-file ownership marker is emitted. Missing/corrupt state treats existing entries as unmanaged. Mandatory raw-byte backups remain private even if optional backups are disabled.

One canonical mutation session surrounds read, derivation, recovery, staging, and publication; advisory locking and generation checks retain their existing crash/degraded-filesystem behavior. Owned entry updates preserve unrelated bytes, authored key order, existing modes, and no-op identity/mtime. Sensitive artifact modes survive fragment merging and direct conversion; incompatible fragment modes refuse. Output scopes are explicit, avoiding a HOME/repository-wide prune walk.

Incomplete compilation/acquisition/support admission retains affected installed output and ownership proof. Independent valid work continues only when it cannot publish or prune the affected generation. Known source markers and desired absence are not substitutes for complete acquisition evidence.

Alternative considered: marker-based MCP merging or a new ownership cache. Rejected because editable native data and disposable caches cannot prove authorization.

### 6. Keep inspection and repair on the same policy path

Check/status/diagnostic-only doctor resolve and admit the same destinations but do not create directories, lock files, state, backups, or native process side effects. Native backup mappings enumerate only the selected explicit destinations and retain private archives.

Repair computes desired entries from verified exact snapshots and existing Marketplace acquisition policy. It reuses disjoint byte-neutral adoption, evidence-backed orphan removal, and collision replacement. `--yes` does not authorize destructive actions. Confirmation happens outside mutation locks. Revalidate configuration, acquisition-lock inputs, and required-resource policy after confirmation and again at publication; stop and clean attempt-owned staging on stale authority.

Diagnostics and assessment findings name field/resource identities without credential, header, command/prompt body, or other sensitive values. Resource assessment and inspection projection ingest the new typed fields and declarations using existing disclosure policies; private backups are not published as inspection evidence.

Alternative considered: target-specific repair/preview synthesis from mutable refs. Rejected because it bypasses exact acquisition and preview-to-publication authority.

### 7. Prove boundaries through consumers and measure their cost

Every delta scenario receives an annotated behavioral test in the implementation pull request. Tests cover refused cases plus independently authored valid controls through public compilation/ingestion, direct conversion, actual native installation, repair, and read-only inspection. Assert stable diagnostic identities and absence of forbidden effects, external reads/writes, mode/mtime changes, or attempt-owned residue. A preservation assertion must also prove a legitimate update reached the intended consumer.

Native probes use a checksum-verified pinned executable, temporary HOME/XDG/config/state/cache roots, a sanitized environment including OpenCode override flags, controlled local endpoints, synthetic credentials and commands, bounded process trees, and network isolation. Do not execute raw downloaded registry content. Gate invocation must witness execution rather than pass on a skipped/missing binary. Native startup/config parsing is isolated too. Standard tests remain independent of optional native executables.

Cover multi-harness/default/lock/source-revision regressions, Marketplace inspection, declarative E2E, and a real-terminal TUI journey. Record baselines and validation overhead on affected hot paths before and after implementation. Before publication, run privacy-screened OCR on the exact code diff, report excluded files, and separately validate adversarial findings concerning consumer reachability, failure behavior, writes, scope/ownership, and regression validity.

## Risks / Trade-offs

- [Required activation enforcement may be unavailable in the investigated native version] → Gate it first; retain a release blocker until upstream or another proven route satisfies the contract. Do not advertise the target prematurely.
- [Native config precedence or startup changes between versions] → Pin executable identity, exercise actual loaders, and require the compatibility gate for version changes.
- [Environment substitution makes literal encoding impossible for some values] → Preserve typed reference intent and suppress unrepresentable output with an error.
- [A shared adapter makes one invalid contribution affect a common artifact] → Validate contributions before merging, retain blocked installed contributions, and publish only a coherent safe generation.
- [Permission order or native agent defaults change effective authority] → Prove effective behavior, including explicit empty policies, built-in/config collisions, and unsupported-tool controls.
- [Locking only coordinates cooperating writers] → Preserve generation/refusal checks and descriptor authority; do not claim advisory locks prevent all hostile same-user filesystem replacement.
- [Finite runner bounds exclude previously accepted source declarations] → Diagnose explicitly, document the supported protocol/bounds, and cover below/at/above controls rather than silently truncating.
- [Added validation affects sync/status latency] → Measure realistic changed hot paths and retain benchmark guards without weakening safety checks.

## Migration Plan

1. Complete the native feasibility matrix and activation/failure gate before public target registration. If blocked, retain planning/evidence and do not enable or release partial OpenCode support.
2. Implement translation, the fixed adapter, native destinations/ownership, and consumer bindings with all new scenarios proven; existing target defaults and committed locks remain unchanged.
3. Ship only after strict OpenSpec validation, annotated scenario traceability, affected tests, `make validate`, touched E2E/TUI gates, pinned native execution gates, privacy screening, OCR, and adversarial review pass.
4. Document explicit opt-in and supported version/forms. No automatic move/adoption of handwritten resources or legacy native configuration is performed.
5. Roll back by deselecting OpenCode and removing only demonstrably owned output under the normal complete-input removal contract. Handwritten configuration, unrelated plugins, authentication/trust state, and other harness targets remain intact.
6. Archive this change in the final implementation commit of the same pull request. Planning completion is not implementation completion.
