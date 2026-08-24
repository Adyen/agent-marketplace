# Devin harness target

**Status:** Promoted for decomposition, not implementation

**Date:** 2026-08-03

**Compatibility floor:** Devin CLI 3000.3

**Origin:** an approved `/grilling` + `/domain-modeling` session on 2026-08-03

**Evidence reference:** `docs/devin-cli-harness-spec-3000.3.md`

## Promotion boundary

The user approved this exact design scope before this document was written. Promotion authorizes planning artifacts and implementation Bead creation. It does not authorize implementation until a child Bead is claimed through the normal lifecycle.

This is a new plan. It does not modify or inherit authority from any earlier work-in-progress document on the subject.

## Objective

Add `devin` as a bidirectional harness-compilation and Agent Marketplace materialization target. It must use canonical Devin paths, preserve representable semantics, report loss explicitly, coexist safely with handwritten aggregate configuration, and require no Windsurf target or compatibility import.

The Devin target covers the marketplace-selectable resource kinds: skill, command, subagent, hook, MCP server, and Rule. Rules are marketplace-selectable generally; Devin explicitly emits a warning and no Rule artifact in this increment.

## Non-goals

- Devin plugin installation or plugin bundles
- Devin Rule materialization in this increment (selected Rules emit a warning and no artifact)
- The Windsurf `.mdc` to `.md` rule-renderer correction
- Windows `%APPDATA%\devin` roots
- Adding Devin to default harness lists
- Automatic migration of existing configurations
- A CLI config mutation command for adding the harness
- Ingesting Claude, Cursor, Windsurf, or `.agents` compatibility layouts as Devin
- Supporting Devin versions older than 3000.3

## Target contract

### Roots

| Scope | Root |
|---|---|
| Repository/local selection | `<repo>/.devin` |
| User | `~/.config/devin` |

Sync does not require a Devin binary. `doctor` detects an installed binary and warns when its version is below 3000.3.

### Resource mapping

| IR kind | Output | Behavior |
|---|---|---|
| Skill | `skills/<name>/SKILL.md` | Native Devin skill |
| Command | `skills/<name>/SKILL.md` | User-triggered skill with `x-agent-marketplace-kind: command` |
| Subagent | `agents/<name>.md` | Native flat custom-subagent profile |
| Hook | Project: `hooks.v1.json`; user: `config.json#hooks` | Command or prompt hook merged into a shared file |
| MCP server | `mcp_config.json` | Stdio or HTTP server merged into a shared file |
| Rule | none | Warning diagnostic; no artifact is emitted (target scope decision; see the amendment below) |

**Amendment, 2026-08-05 (bead `agent-marketplace-5exj.22`).** This row originally
read "Stable unsupported *error* diagnostic", and the evidence snapshot justified
it with "there is no canonical `.devin/rules` project path". The D12 runtime
probe falsified that premise on Devin 3000.3.27: `devin rules paths` advertises
only `.windsurf/rules` and `.cursor/rules`, but a file written to
`.devin/rules/<name>.md` **is** discovered. Rules are marketplace-selectable
generally, but Devin's target contract still emits nothing in this increment;
the decision is target scope, not a limitation of Devin's reader. What changed
is the justification and, in consequence, the severity:
`rule.devin.unsupported` is now a WARNING, matching `rule.claude.no-primitive`
and `rule.gemini.no-primitive`, because dropping a selected rule writes nothing
unsafe and must not fail an otherwise-complete sync or conversion. See
`pkg/harness-compilation/diag/codes.md`.

User-scope paths are relative to the user root. User hook reconciliation patches only the `hooks` member of `~/.config/devin/config.json` and preserves unrelated settings.

Skills use directory symlinks and flat subagents use file symlinks. Both shapes are release requirements: 3000.3 runtime verification must cover project and user scopes before the target ships. There is no real-file fallback in this increment. Aggregate files are real files updated through syntax-preserving structural patches.

### Bidirectional conversion

`devin` is valid for `convert --from`, `convert --to`, registry `source_harness`, and autodetection. One conversion input represents one explicit source root; project and user roots are never merged implicitly. A workspace input contains `.devin/`. A direct user config root contains `skills/`, `agents/`, `config.json`, and `mcp_config.json` at its top level and requires explicit `--from devin` or `source_harness: devin` because that shape is too ambiguous for autodetection.

The ingester does not walk ancestors, descendants, or nested `.devin` directories. Workspace autodetection recognizes canonical `.devin` resource/config markers. When `.devin` and compatibility layouts coexist at the explicit root, autodetection selects Devin; callers may explicitly choose another source harness.

The round-trip guarantee is semantic IR stability:

1. Ingest canonical Devin input.
2. Represent supported meaning in IR.
3. Compile back to Devin deterministically.
4. Re-ingest to equivalent IR.

Byte-for-byte source reproduction is not required. Unknown valid fields are retained in `Extra` or target extensions and re-emitted when they do not conflict with canonical fields.

A command compiled as a skill carries the stable YAML extension:

```yaml
x-agent-marketplace-kind: command
```

The Devin ingester interprets that value as command provenance. An ordinary Devin skill remains a skill.

## IR evolution

Breaking harness-compilation API changes are allowed when needed for a coherent model. All in-repository consumers must migrate in the same dependency-ordered program.

### MCP server

The MCP IR must distinguish at least:

- stdio command and arguments
- environment values
- HTTP URL and transport
- HTTP headers
- disabled state
- OAuth client ID, client secret, and resource
- unknown target fields

OAuth client secrets are sensitive values. Existing target ingesters and compilers map every new field their native formats represent and emit stable diagnostics for unsupported semantics.

### Hook

The hook IR must explicitly model event, matcher, type, command or prompt payload, timeout, and unknown fields rather than overloading a command string. Existing targets map each field where their native formats permit it and emit stable diagnostics for unsupported event/type/field combinations.

### Skill

The Skill IR must explicitly model description, body, argument hint, model, allowed tools, permissions, triggers, subagent execution, named-agent execution, support files, and unknown fields. Existing target ingesters and compilers map each field their native formats represent.

Security-affecting restrictions such as allowed tools and permissions fail a resource when a target cannot represent them; they are never silently narrowed, broadened, or dropped. A converted command skill is emitted with `triggers: [user]`, no model trigger, command argument metadata where available, and the stable command-origin extension.

**Amended for Devin `allowed-tools` by bead `agent-marketplace-pu6b`** (this plan is otherwise unchanged, and the amendment is deliberately narrow). On Devin only, and for `allowed-tools` only, an entry the target cannot represent — a scoped specifier such as `Bash(openspec:*)`, a name outside `read`/`edit`/`grep`/`glob`/`exec`, or a non-list shape — no longer fails the resource. The key is omitted with a warning (`skill.devin.tool-unknown`, `subagent.devin.tool-unknown`) and the artifact is still emitted, which inherits Devin's default tool set and is a real, accepted widening. `permissions` is untouched, Devin's `isolation` and `disallowedTools` rules are untouched. Factory's analogous `tools` restriction now also warns and omits the key while emitting the droid; Gemini and Cursor still fail closed. Rationale, rejected alternatives, and evidence: `pkg/harness-compilation/diag/codes.md`.

### Unknown-field precedence

Canonical modeled fields win over conflicting values in preserved extras. The compiler emits a stable warning naming the conflicting field without including sensitive content. Nested extras are covered by round-trip fixtures.

## Translation and loss policy

### Tools

Bundled 3000.3 documentation and earlier 3000.2 runtime probes identify this Devin `allowed-tools` vocabulary; the 3000.3 runtime probe is a release gate before the mapping is frozen:

| Canonical | Devin |
|---|---|
| `Read` | `read` |
| `Edit` | `edit` |
| `Grep` | `grep` |
| `Glob` | `glob` |
| `Bash` | `exec` |

If any explicitly allowed tool is unmappable, the resource produces an error diagnostic and no artifact. A partial list would narrow intended capability; omitting the restriction would broaden privilege.

**Amended by bead `agent-marketplace-pu6b`** — this is the sentence the compiler used to quote verbatim, and it is the one the reversal overturns. The first clause no longer holds: an unmappable or scoped entry now omits the whole `allowed-tools` key with a WARNING (`skill.devin.tool-unknown`, `subagent.devin.tool-unknown`) and the artifact is still emitted. The second clause survives unchanged and is still enforced — a partial list is never emitted — and omitting the restriction does broaden privilege, which is now the accepted cost rather than a reason to suppress the resource: Devin cannot represent this restriction at all (five bare names, no per-command scoping, unknown names silently dropped), so every possible output is unfaithful and losing the resource buys nothing. See the amendment under "Skill" above and `pkg/harness-compilation/diag/codes.md` for the evidence, the rejected `allowed-tools: []` alternative, and the accepted Devin/Factory divergence.

### Models

Only stable Devin aliases are mapping targets. At minimum, `sonnet`, `opus`, and `haiku` map to the same aliases. Versioned model IDs must not be guessed.

An unmappable explicit model is omitted and emits a warning. An empty subagent model stays empty and emits a warning because Devin interprets absence as its default subagent model, not parent-model inheritance. Reverse ingestion leaves an absent model empty.

### Subagent-only fields

The Subagent IR gains a typed `MaxNesting` field. Devin round-trips it; other targets map it where representable or emit stable diagnostics.

- `Isolation`: error; no artifact
- `DisallowedTools`: error; no artifact
- `Effort`: warning and drop
- built-in name conflict: error; no artifact

### Bundle collisions

A skill and converted command with the same name target one path. Bundle compilation emits a stable error naming both resources; neither silently wins.

### Literal credentials

Repo-scoped literal values in MCP environment variables, HTTP headers, or OAuth client secrets are preserved but emit a security warning. Secret values must never appear in structured logs, diagnostics, ownership state, journals, or backup metadata.

## Error behavior

Compilation and sync collect all diagnostics. Resources with error diagnostics are skipped, valid resources continue, and `sync` exits non-zero if any error occurred.

Suppression is per resource, not per bundle. A resource that emits any error diagnostic contributes no artifacts at all, including its fragments of a shared aggregate file; every other resource in the same bundle is compiled and published normally. An error diagnostic that is not attributable to a single resource must define its own suppression at the point of emission and may never suppress a resource it says nothing about. There are two such cases: a load/ingest failure describes a resource that never reached the compiler and therefore suppresses nothing further, and a bundle-level artifact-path conflict suppresses both named producers as whole resources — fragments included, so a manifest cannot advertise an entry whose file was not written.

Materialization does not refuse a partial compilation. It writes the artifacts of every resource that compiled cleanly, reports the run as partial together with the list of skipped resources, and returns a distinguishable partial-result error so the caller exits non-zero rather than reading a partial write as a clean run. A partial run does not prune: a resource skipped this run is indistinguishable from a resource that was removed, so pruning would delete output the previous clean run wrote. "Does not prune" is scoped to the output the skipped resource would have written, because that is exactly where the indistinguishability argument holds: consumer-side, `internal/materialize` suppresses pruning per output COMPONENT (`skills`, `agents`, a non-Devin `hooks` directory) and, for a Devin shared aggregate, per FILE, so one unrepresentable hook never stops `.devin/skills/` from pruning. A skip that cannot be attributed to a component still suppresses the whole output root. The library-level `harnesscompile.Materialize` keeps a bundle-wide `WriteReport.PruneSuppressed`, because the one-shot `convert` path writes a whole output tree in a single call.

Aggregate updates are atomic per file, not per target. Within valid aggregate JSON, invalid or conflicting desired entries are skipped while other valid entries are patched atomically; sync still exits non-zero. Both halves of that sentence are implemented: the entry-skip `hook.devin.*` and `mcp.devin.*` codes are error diagnostics, which `internal/pipeline` promotes into `SyncResult.Errors` and `sync` returns as a non-zero exit (bead `agent-marketplace-5exj.20`). Malformed destination JSON blocks that file only. A failed MCP merge leaves `mcp_config.json` untouched but does not prevent unrelated skills, agents, or a valid hooks file from being materialized.

## Shared aggregate ownership

### Durable state

Ownership authority is stored outside repositories and outside caches:

```text
$XDG_STATE_HOME/agent-marketplace/
# default
~/.local/state/agent-marketplace/
```

Project records are keyed by a hash of the canonical absolute target root. User-target records use the canonical user target root in the same scheme. State updates are atomic and durable.

The state records enough identity and non-secret digest information to distinguish owned entries, detect drift, prune stale entries, and validate adoption. It must not copy hook payloads, MCP environment values, headers, OAuth secrets, or other sensitive values.

### Journaled generations

Aggregate bytes and external ownership state form one recoverable generation. Each mutation uses a durable transaction journal with non-secret digests and explicit phases: prepare after safety backup, atomically rename the patched aggregate, atomically commit the matching ownership generation, then clear the journal. On startup, incomplete generations are replayed or rolled back according to the recorded phase; ownership is never inferred heuristically. Crash-injection tests cover every boundary between backup, journal fsync, aggregate rename, state commit, and journal cleanup.

### Entry identities and safety rules

- MCP identity is the server object key.
- Hook behavior identity is the Devin event name plus the digest of the canonical hook value (the whole rule object).
- Duplicate desired hook values collapse before patching.
- One semantically identical unmanaged hook may satisfy any number of selected marketplace resources without duplication or adoption.
- Cache deletion has no ownership effect.
- Missing or corrupt ownership state makes all existing aggregate entries unmanaged.
- Without proof of ownership, sync preserves existing entries and refuses collisions or pruning.
- An unmanaged MCP key collision is preserved and causes only the generated entry to fail.
- Manual edits to a proven owned entry are reported as drift and replaced from marketplace configuration.
- Stale proven owned entries are pruned.
- Handwritten hooks keep their relative order and execute before generated hooks.
- Generated hooks use deterministic resource ordering after handwritten hooks.

### JSON preservation

Shared Devin JSON files may contain comments. Structural patching must preserve unmanaged comments and key order, including comment association with the same unmanaged key or array element. Whitespace may be normalized. Malformed JSON is safety-backed up before parsing, left unchanged, and reported with a precise parse location.

A vetted concrete-syntax JSON/JSONC implementation may be added. Dependency selection must follow repository supply-chain policy.

### Backups and recovery

Safety backups are mandatory even when configured backups are disabled. Raw pre-mutation bytes are stored under the Agent Marketplace XDG state root before first managed merge, adoption, or confirmed orphan removal. Retain the five newest backups per canonical output file. Backup filenames and metadata must not expose secret values.

`doctor --repair` is the recovery surface:

- `doctor --repair` previews exact adoption matches and evidence-backed orphan candidates; on a TTY it then prompts for each authorized action
- off-TTY, `doctor --repair` is preview-only
- `doctor --repair --yes` adopts exact current desired matches without prompting
- orphan-removal candidates require non-heuristic retained journal or safety-backup evidence linking the entry to prior managed output, plus explicit per-entry confirmation and a fresh backup
- entries without such evidence are never nominated; users must edit them manually
- orphan candidates are never described as currently owned, and unrelated or merely similar entries remain unmanaged
- exit 0 means clean, repaired, or declined without operational failure
- exit 1 means corrupt state/file or partial repair failure

## Target registry and validation

Introduce one authoritative target registry before Devin-specific integration. It must drive target validity and expose metadata needed by config validation, CLI help/allowlists, TUI selection, conversion, and tests without creating import cycles.

Reject invalid top-level harnesses, registry harness overrides, and `source_harness` values during config validation rather than deferring failure to compilation.

Compiler and ingester registration remains explicit and testable. Registry tests must fail when a declared target lacks required compiler, ingester, root, or UI metadata.

## Rollout UX

- Existing and new default harness lists remain unchanged.
- TUI harness selection is the only harness-list mutation UX in this increment.
- `doctor` detects `devin`, prints the installed version, reports compatibility with 3000.3+, and recommends selecting the harness when installed but unconfigured.
- `doctor --repair` owns aggregate-state recovery only; it does not silently edit harness lists.
- Help text, status, list badges, conversion allowlists, and target displays include Devin.

## Observability

Use structured `slog` events for:

- compile/ingest start and completion by target and kind
- unsupported or lossy mapping decisions without sensitive values
- aggregate merge decisions and conflict paths
- ownership-state load/write/adoption outcomes
- backup and repair outcomes
- doctor detection and compatibility decisions

Never log hook payloads, MCP environment values, headers, or full aggregate files.

## Work packages and dependencies

### D1. Authoritative target registry and config validation

Consolidate target declarations and reject invalid target/source-harness names early. This blocks all target integration.

### D2. Rich Skill, Subagent, MCP, and hook IR

Redesign IR and public APIs, including Skill permissions/execution fields, Subagent max nesting, hook matcher/timeout/payload, and MCP OAuth/HTTP fields. Migrate in-repo callers and add cross-target field matrices, mappings, and diagnostics. Depends on D1 only where target metadata is required.

### D3. Devin compiler foundation and mapping tables

Register `TargetDevin`, compiler plumbing, model/tool/event/MCP mappings, and diagnostic codes. Depends on D1 and D2.

### D4. Skills and converted commands

Compile native skills and command-as-skill provenance; detect cross-kind path collisions. Depends on D3.

### D5. Subagents

Compile flat profiles with strict tool and unsupported-field policy. Depends on D3.

### D6. Hooks

Compile command and prompt hook fragments for `hooks.v1.json`. Depends on D2 and D3.

### D7. MCP servers

Compile stdio and HTTP fragments for `mcp_config.json`. Depends on D2 and D3.

### D8. Canonical Devin ingester and autodetection

Ingest canonical project/user layouts, recover command provenance, preserve extras, and prefer Devin markers. Depends on D2 through D7.

### D9. Durable ownership and syntax-preserving JSON

Implement XDG state, journaled generations, exact ownership, behavior identities, JSONC patching, mandatory bounded safety backups, drift, adoption/orphan-removal primitives, and concurrency/crash-injection tests. Can proceed after D2 in parallel with compiler resource work.

### D10. Devin materialization

Add roots, generic plans, symlink handling, per-file aggregate atomicity, pruning, partial failure, and backup integration. Depends on D4-D7 and D9.

### D11. Doctor, repair, TUI, convert, and status UX

Expose target selection and conversion, version detection, recommendation, and confirmed repair. Depends on D8-D10.

### D12. Evidence, integration, E2E, and performance

Refresh the evidence snapshot, run local Devin probes, add declarative E2E coverage, complete documentation, and report benchmark deltas. Depends on all prior packages.

## Verification policy

Every work package includes focused unit/golden tests, stable diagnostic catalog updates, structured logging for changed behavior, and relevant benchmarks for touched hot paths.

Before optimizing or changing a user-visible hot path, capture a baseline:

```bash
go test -run '^$' -bench '^BenchmarkName$' -benchtime 2s -count 5 -benchmem | tee before.txt
```

Afterward, run the same benchmark into `after.txt` and compare with `benchstat`. Benchmark files remain in the tree; transient result files do not.

Integrated quality gates include:

- formatting and lint
- unit and golden tests
- build
- E2E module compilation and applicable local E2E tests
- version-gated local Devin 3000.3+ probes
- semantic round-trip matrices
- benchmark comparisons for sync, materialization, ingestion, and resolution paths touched by the work

## Acceptance criteria

**Certification:** [`docs/devin-verification-2026-08-04.md`](../devin-verification-2026-08-04.md) records the D12 verdict for each criterion below, the probes still awaiting a live 3000.3 run, and the releasability decision.

1. `devin` is accepted and validated everywhere targets are exposed, without changing defaults.
2. The five artifact-producing marketplace kinds compile to their scope-specific canonical Devin 3000.3+ paths, including user hooks in `config.json#hooks`; selected Rules remain marketplace-visible, while Devin emits a warning and no artifact.
3. One explicit canonical Devin source root ingests and semantically round-trips without runtime ancestor/nested discovery; user roots require an explicit source harness.
4. Versioned Skill/Hook/MCP field-by-target matrices specify expected artifact, diagnostic severity/code, and skip behavior for every modeled field and event.
5. Rules and every unsupported/lossy field produce documented diagnostics; canonical fields win extra-field conflicts with warnings.
6. Tool and permission handling never passes an unverified restriction through. On Devin only, an unrepresentable `allowed-tools` restriction is diagnosed, the whole key is omitted, and the resource is emitted with the accepted widening to Devin's default tool set; unsupported isolation or `disallowedTools` still suppresses the resource, and untyped `permissions` never passes through.
7. Command provenance and user-only trigger behavior round-trip through the stable extension.
8. Rich Skill, hook, and MCP/OAuth semantics map across every target that can represent them.
9. Handwritten aggregate values, associated comments, key order, and hook order survive reconciliation; whitespace normalization is allowed.
10. Journaled generations recover every crash boundary without heuristic ownership, and ownership cannot be inferred from cache contents or missing state.
11. `doctor --repair` implements the specified TTY/non-TTY behavior, exact adoption, confirmed orphan removal, backups, and exit codes.
12. Invalid desired entries are skipped, valid entries/files commit independently and atomically, and sync returns non-zero.
13. Mandatory safety backups retain five raw versions per file under XDG state regardless of normal backup settings.
14. Literal credential warnings and all state/log/diagnostic paths never expose environment, header, hook payload, or OAuth secret values.
15. Current-version probes pass for project/user skill-directory and flat-subagent symlinks; failure blocks release.
16. Doctor and local probes establish the 3000.3+ compatibility contract.
17. All required tests, logs, diagnostics, and benchmark evidence are present.
