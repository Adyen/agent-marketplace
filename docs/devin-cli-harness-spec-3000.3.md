# Devin CLI harness specification: 3000.3 compatibility snapshot

**Status:** Evidence baseline for implementation

**Captured:** 2026-08-03

**Amended:** 2026-08-05 — a dated authenticated capture was appended by bead
`agent-marketplace-5exj.31` (see
[Authenticated model-list re-capture (2026-08-05)](#authenticated-model-list-re-capture-2026-08-05)
and
[Flat-subagent file symlinks — observed 2026-08-05 (authenticated)](#flat-subagent-file-symlinks--observed-2026-08-05-authenticated)).
Earlier captures are kept verbatim; nothing recorded on 2026-08-03/04 was deleted
or rewritten.

**Corrected:** 2026-08-05, later the same day, by the same bead. Three factual
corrections, not additions: (1) the custom-subagent layout claim — Devin's
bundled docs accept **two** layouts, `agents/<name>.md` and
`agents/<name>/AGENT.md`, not "exactly" the flat file; the compiler emits, and
this run exercised, only the flat form. (2) the dangling-link causality control
is **not** project-only — it was run for the user scope too. (3) the
`max-nesting` and frontmatter-`name:` claims were over-stated: acceptance was
observed, the *value* of `max-nesting` and the `name:` override were not.

**Installed binary:** `devin 3000.3.27 (0becb483)` on macOS

**Compatibility floor:** 3000.3

**Plan:** `docs/plans/2026-08-03-devin-first-class-harness.md`

This document records what the Devin target may rely on. It is a versioned snapshot, not a promise about future Devin releases. Re-run the probes and create a new snapshot whenever the compatibility floor changes.

## Evidence grades

| Grade | Meaning |
|---|---|
| **Verified** | Observed directly against the installed binary; a reproducible probe is recorded |
| **Bundled docs** | Stated by documentation installed with the current Devin CLI |
| **Inferred** | Required by the approved design but not yet verified at runtime |
| **Verified (authenticated manual)** | Added 2026-08-05. Observed directly against the installed binary, but only from an authenticated session, so the local unauthenticated probe suite cannot reproduce it. The procedure is recorded step by step instead, and the observation is registered in `internal/devinprobe/manual.go` rather than as an executable probe. Applies to the flat-subagent symlink/discovery case and to `devin models list`. |

Implementation must not promote an inferred claim to a compatibility contract without a local probe or fixture backed by authoritative schema documentation.

## Canonical roots

**Bundled docs:** Project extensibility lives under `.devin/`; user extensibility lives under `~/.config/devin/` on macOS/Linux. The project root is found by walking upward for `.git` or `.jj`.

| Resource | Project | User |
|---|---|---|
| Skills | `.devin/skills/<name>/SKILL.md` | `~/.config/devin/skills/<name>/SKILL.md` |
| Subagents | `.devin/agents/<name>.md` or `<name>/AGENT.md` | `~/.config/devin/agents/...` |
| Hooks | `.devin/hooks.v1.json` | `~/.config/devin/config.json#hooks` |
| MCP | `.devin/mcp_config.json` | `~/.config/devin/mcp_config.json` |
| Main config | `.devin/config.json` | `~/.config/devin/config.json` |

Project-local personal files include `.devin/config.local.json` and `.devin/mcp_config.local.json`; Agent Marketplace does not materialize into these paths.

### Skill-path probe

```bash
devin skills paths
```

Observed on 3000.3.27:

```text
User skills (global):
  ~/.config/devin/skills/<skill-name>/SKILL.md
  ~/.agents/skills/<skill-name>/SKILL.md
Project skills:
  .devin/skills/<skill-name>/SKILL.md
  .agents/skills/<skill-name>/SKILL.md
```

Only `.devin` and `~/.config/devin` are canonical Devin source-layout paths for this target. `.agents`, `.windsurf`, `.cursor`, and `.claude` are standards or compatibility inputs and belong to separate source formats.

One conversion input represents one explicit root. Workspace autodetection recognizes `.devin` markers and prefers them over compatibility markers. A direct `~/.config/devin`-shaped root requires explicit `--from devin`; the ingester does not merge user/project roots or emulate ancestor and nested runtime discovery.

## Skills

**Bundled docs:** Skills use agentskills-compatible YAML frontmatter and Markdown bodies. They may be invoked by users or the model.

Relevant fields included in the expanded Skill IR and field matrix:

- `name`, `description`, and Markdown body
- `argument-hint`
- `model`
- `allowed-tools`
- `permissions.allow`, `permissions.ask`, and `permissions.deny`
- `triggers` (`user` and `model`)
- `subagent` and named `agent` execution
- support files
- unknown extension fields

Security-affecting fields fail compilation when a target cannot represent them, except that an unrepresentable Devin `allowed-tools` restriction emits a warning, omits the key, and still emits the resource. Canonical modeled fields override conflicting extras with a warning.

The Devin compiler uses the stable extension below to preserve command origin:

```yaml
x-agent-marketplace-kind: command
```

**Verified:** Devin 3000.3.27 accepts this unknown namespaced key. `TestProbeCommandProvenanceExtension` provides the runtime probe, and the Devin command conversion matrix provides the fixture-backed round-trip coverage.

## Commands

**Bundled docs:** Devin has no native project `commands/` resource. Skills are slash commands. Claude command compatibility imports commands as skills.

The target therefore emits commands as skills with user invocation enabled and model invocation disabled. Command and skill names share one namespace and one output path.

## Custom subagents

**Bundled docs:** Both layouts are accepted:

```text
.devin/agents/<name>.md
.devin/agents/<name>/AGENT.md
```

In the flat form the file name without `.md` becomes the identifier; in the
directory form the directory name does, and `AGENTS.md`, `agent.md`, and
`agents.md` are also accepted as the inner file name (`AGENT.md` takes
precedence, then `AGENTS.md`, `agent.md`, `agents.md`). A `name:` frontmatter
value overrides the path-derived identifier **in both layouts**. Built-in
identifiers such as `subagent_explore` and `subagent_general` cannot be
replaced. Source: `/usr/local/share/devin/docs/subagents.mdx:224-259`.

Canonical compiler output is the flat form — **one** of the two accepted
layouts, chosen deliberately, not the only one Devin accepts. The directory form
is what `pkg/harness-compilation/compile/devin/subagent_test.go:559`
(`TestSubagentArtifactShapeSupportsFileSymlink`) forbids the compiler from
emitting, because it would turn the release-gated file symlink into a directory
symlink.

Supported documented fields:

- `name`
- `description`
- `model`
- `allowed-tools`
- `max-nesting`

`max-nesting` is a typed Subagent IR field and must semantically round-trip through Devin; other targets diagnose it when unrepresentable. An absent model means Devin's default subagent model, not the parent model. Ingestion must retain absence rather than inventing a concrete model.

**Verified on 3000.3.27 (`0becb483`), 2026-08-05 (authenticated manual run, bead `agent-marketplace-5exj.31`).** The layout and the frontmatter above are no longer taken on documentation alone:

- The flat form `agents/<name>.md` is loaded at **both** scopes — `<repo>/.devin/agents/` and `~/.config/devin/agents/`. A session enumerated exactly `probe-agent-project`, `probe-agent-user`, `subagent_explore`, `subagent_general`, and the same prompt run from outside the repo listed only `probe-agent-user` plus the two builtins.
- **`allowed-tools` is enforced, and that is verified on the filesystem rather than self-reported.** A profile compiled with `allowed-tools: [read, grep]` reported its tools as exactly `grep, read, read_subagent, run_subagent` when spawned — no `edit`, no `exec` — and CONTROL E then showed the restriction holds in effect: asked to overwrite a file and to run a shell command, it did neither, and the file was independently confirmed unchanged on disk afterwards. The bound is on the profile's **own** toolset: the same exchange showed it could in principle delegate to the write-capable builtin `subagent_general`, and nesting was not restricted.
- **`max-nesting` was accepted, and only its PRESENCE-level effect was observed.** The spawned profile carried the nesting tools `read_subagent` and `run_subagent`, which is consistent with any nesting budget ≥ 1. No nesting-depth limit was exercised, so `max-nesting: 2` is **not** observed to bound depth at 2.
- **Only the flat-file layout was exercised.** The directory form `agents/<name>/AGENT.md` is documented (below) but was not observed here, and it is deliberately not what the compiler emits.
- Bundled-docs corroboration of both accepted layouts and of `name:` overriding the path-derived identifier: `/usr/local/share/devin/docs/subagents.mdx:224-259`. The `name:` override itself was **not exercised**: the probe profiles' file basenames equalled their frontmatter `name`, so nothing in the capture distinguishes a frontmatter-derived identifier from a path-derived one.
- **Not observed:** which model actually served a spawned subagent. Devin does not label a running subagent's model in the CLI, so `model: sonnet` is confirmed ACCEPTED and confirmed to name a live alias (see [Authenticated model-list re-capture (2026-08-05)](#authenticated-model-list-re-capture-2026-08-05)) — not confirmed to have selected Claude Sonnet 5 for that run.
- The three residual questions above — the serving model, the VALUE of `max-nesting`, and the frontmatter `name:` override — are tracked machine-readably as the `UnrunProbe` `subagent-frontmatter-model-and-nesting-honored` in `internal/devinprobe/unrun.go` (not release-blocking).
- The same bundled docs mark custom subagents **experimental** ("The format, behavior, and configuration options may change in future releases"), so this observation is version-bound and must be re-taken when the compatibility floor moves.

Details and controls: [Flat-subagent file symlinks — observed 2026-08-05 (authenticated)](#flat-subagent-file-symlinks--observed-2026-08-05-authenticated).

### Tool vocabulary

**Verified against 3000.2.17; re-verification required on 3000.3.27 before implementation completion:** the accepted `allowed-tools` values were exactly:

```text
read edit grep glob exec
```

**Re-verification discharged on 3000.3.27 (D12).** The local probe
`internal/devinprobe.TestProbeAllowedToolVocabulary`
(`internal/devinprobe/probe_test.go:76`) recomputed the acceptance set from a
22-name candidate superset and logged `PROBE PASS tools`: exactly
`[edit exec glob grep read]` are accepted and all 17 other candidates are
**silently dropped** by Devin. Silent dropping — not rejection — is why the
compiler must never pass an unmappable tool name through to the artifact. The
sentence above is kept as the original 3000.2.17 grade.

**Amended by bead `agent-marketplace-pu6b`.** This paragraph used to continue
"…is why the compiler must fail on an unmappable tool instead of passing it
through", collapsing two separate conclusions. The pass-through ban stands. The
*failure* half did not survive: an unmappable or scoped entry now omits the whole
`allowed-tools` key with a WARNING (`skill.devin.tool-unknown`,
`subagent.devin.tool-unknown`) and the resource is still emitted, because silent
dropping plus the absence of per-command scoping means Devin cannot represent the
restriction at all, so suppressing the resource buys nothing. Read alongside
CONTROL E above: a list Devin *can* represent is genuinely enforced, which is why
a mappable restriction is still emitted exactly. Full rationale and the accepted
Devin/Factory divergence: `pkg/harness-compilation/diag/codes.md`.

Approved canonical mappings:

| IR | Devin |
|---|---|
| `Read` | `read` |
| `Edit` | `edit` |
| `Grep` | `grep` |
| `Glob` | `glob` |
| `Bash` | `exec` |

Probe pattern:

```bash
mkdir -p .devin/skills/probe
printf '%s\n' '---' 'name: probe' 'description: probe' \
  'allowed-tools:' '  - read' '---' 'probe' > .devin/skills/probe/SKILL.md
devin skills show probe
```

Run the probe in an isolated temporary Git repository and test every candidate. Remove the temporary tree afterward.

## Models

### Model-list probe

```bash
devin models list
```

**Verified on 3000.3.27:** stable aliases included:

| Alias | Current family at capture time |
|---|---|
| `opus` | Claude Opus 5 |
| `sonnet`, `claude` | Claude Sonnet 5 |
| `haiku` | Claude Haiku 4.5 |
| `swe` | SWE-1.7 |
| `gemini` | Gemini 3.5 Flash |
| `gpt` | GPT-5.6 Terra |
| `codex` | GPT-5.3-Codex |

The compiler maps only stable aliases where semantic intent is justified. It does not pin the current versioned family behind an alias. Unknown explicit models are omitted with a diagnostic rather than passed through.

### Authenticated model-list re-capture (2026-08-05)

**Verified on 3000.3.27 (`0becb483`), 2026-08-05T10:00:20Z, Darwin 25.6.0 arm64, authenticated Devin Enterprise account under an isolated `HOME`** (bead `agent-marketplace-5exj.31`; full procedure in `docs/devin-verification-2026-08-04.md`, appendix "the manual authenticated subagent/model run"). This capture is **added alongside** the 2026-08-03 capture above, which is kept verbatim — the earlier table is still the record of what was seen then.

`devin models list` succeeded and reported **28 model families**. Stable aliases present:

| Alias | Family at this capture |
|---|---|
| `opus` | Claude Opus 5 |
| `sonnet`, `claude` | Claude Sonnet 5 |
| `haiku` | Claude Haiku 4.5 |
| `gemini` | Gemini 3.5 Flash |
| `gpt` | GPT-5.6 Terra |
| `codex` | GPT-5.3-Codex |
| `swe` | SWE-1.7 |

A `gemini-3.1-pro` family **exists but carries no alias**.

Two consequences for the mapping table:

- The `sonnet`, `opus`, and `haiku` Devin cells are confirmed still valid.
- The Flash→`gemini` / Pro→`modelUnmappable` split is now **positively justified rather than merely assumed**: a Pro-class family is installed and has no stable alias to map to.

**Drift versus the earlier capture (the 2026-08-03/04 revision's capture, kept verbatim as the table above).** The alias **set** is unchanged — `opus, sonnet, claude, haiku, swe, gemini, gpt, codex` then and now — and `gemini` still resolves to Gemini 3.5 Flash. What has moved is the *versioned families* behind several aliases (`swe` now SWE-1.7, `gpt` now GPT-5.6 Terra) and the total family count, which is now 28. This is exactly the risk recorded under [Known documentation/runtime risks](#known-documentationruntime-risks) ("Model families behind stable aliases change over time"), and it is why the compiler maps aliases and does not pin families. **The aliases the mapping table depends on are exactly the ones that held.** A family list is a moving target and this table must be read as a dated capture, not a contract.

## Hooks

**Bundled docs:** The canonical project standalone file is `.devin/hooks.v1.json`. It is Claude-Code-compatible and contains the hook object directly rather than nesting it under a top-level `hooks` key. Hooks may also appear under `hooks` in main config files.

The expanded Hook IR preserves event, matcher, hook type, command or prompt payload, timeout, and unknown fields. Documented hook types are:

- command
- prompt

Documented matchers apply to tool-related events; timeout is attached to the individual hook action. Documented events include:

- `PreToolUse`
- `PostToolUse`
- `PermissionRequest`
- `UserPromptSubmit`
- `Stop`
- `SessionStart`
- `SessionEnd`
- `PostCompaction`

Command hooks use exit code 0 for success and 2 for blocking. Other nonzero exits are logged but non-blocking.

**Inferred:** Exact prompt-hook payload shape, all event schemas, matcher restrictions, timeout units/defaults, and comment acceptance in `hooks.v1.json` must be verified from 3000.3.27 fixtures before compiler implementation is complete.

Project hooks are patched in `.devin/hooks.v1.json`. User hooks are patched only under `~/.config/devin/config.json#hooks`; a user-level standalone `hooks.v1.json` is not part of the documented contract. Hooks from all configuration sources are collected, not overridden. Agent Marketplace preserves handwritten order and appends generated hooks deterministically.

## MCP servers

### Version boundary

**Bundled docs:** Before 3000.3, MCP servers lived in `config.json#mcpServers`. Starting in 3000.3, canonical MCP configuration uses dedicated files and older entries are migrated automatically.

This target emits only the 3000.3+ dedicated-file layout:

```text
.devin/mcp_config.json
~/.config/devin/mcp_config.json
```

### Schema

Documented stdio form:

```json
{
  "mcpServers": {
    "server-name": {
      "command": "example",
      "args": ["--flag"],
      "env": {"SETTING": "value"},
      "disabled": false
    }
  }
}
```

Documented HTTP form:

```json
{
  "mcpServers": {
    "server-name": {
      "url": "https://example.invalid/mcp",
      "transport": "http",
      "headers": {"X-Client": "example"},
      "oauthClientId": "client-id",
      "oauthClientSecret": "${env:OAUTH_CLIENT_SECRET}",
      "oauthResource": "https://example.invalid",
      "disabled": false
    }
  }
}
```

Remote servers default to Streamable HTTP and may fall back to SSE. The IR preserves explicit transport and typed OAuth client ID, client secret, and resource rather than relying on runtime fallback or target extras. OAuth client secrets are sensitive and must not enter logs, diagnostics, ownership state, journals, or backup metadata.

Useful non-mutating commands:

```bash
devin mcp list
devin mcp get <name>
```

**Inferred:** Comment handling, unknown-field handling, disabled-state round trip, and exact interpolation behavior require isolated 3000.3.27 probes.

## Rules

### Rule-path probe

```bash
devin rules paths
```

Observed on 3000.3.27:

```text
Devin rules:
  .windsurf/rules/*.md (always-on)
Cursor rules:
  .cursor/rules/*.md (conditional)
```

### Rule-discovery probe (D12, 2026-08-04, corrects the paragraph this replaces)

The paragraph that stood here said "There is no canonical `.devin/rules` project
path in the direct-materialization model. The Devin target *therefore* emits an
explicit unsupported diagnostic for IR rules." The premise is false on
3000.3.27 and the inference does not hold. What was actually observed, in the
probe suite (`internal/devinprobe.TestProbeRulesDiscoveryUnderDevinRoots`,
`internal/devinprobe/probe_test.go:272`) and
again by hand outside it, in an isolated `HOME` and a fresh git repository:

```console
$ devin rules list
No rules found.
$ printf '# probe rule\n' > .devin/rules/probe-rule.md
$ devin rules list
Available Rules
  probe-rule [Devin] manual
```

So `devin rules paths` advertises no `.devin` path (capture above) while
`.devin/rules/*.md` is nonetheless read. Only that much is established. Nothing
here says the path is documented, stable, supported, or equivalent in
precedence to the advertised Windsurf and Cursor paths — none of that was
observed, and `rules paths` is evidence against it.

Plugin rules exist but plugins are outside this increment.

The Devin target still emits no rule artifact, and the diagnostic
`rule.devin.unsupported` still fires — but as a WARNING describing a scope
decision of this project, not a capability limit of the harness. Rules are not
marketplace-selectable in Agent Marketplace at all, "marketplace-selectable
rules" is an explicit non-goal of the promoted plan, and writing into an
unadvertised path is the kind of undocumented dependency this target refuses
elsewhere. See `pkg/harness-compilation/diag/codes.md`, section
"`rule.devin.unsupported` is a scope decision, not a capability limit" (bead
`agent-marketplace-5exj.22`).

## Configuration precedence

**Bundled docs:** Highest to lowest:

1. organization/team settings
2. session grants
3. `.devin/config.local.json`
4. `.devin/config.json`
5. `~/.config/devin/config.json`

MCP servers merge by name with higher-level definitions winning. Hooks are collected from all sources. Agent Marketplace does not attempt to override organization policy or write local-personal files.

## Compatibility imports

**Bundled docs:** Devin can read resources from `.agents`, Cursor, Windsurf, Claude, Copilot, OpenCode, VS Code, and Zed layouts depending on resource type and `read_config_from` settings.

These are explicitly not canonical Devin source-layout inputs for this target. Autodetection prefers `.devin` when it coexists with compatibility markers.

## Materialization and ownership claims

### Symlinks

**Verified against 3000.2.17; re-verification required on 3000.3.27:** Devin followed skill directory symlinks and subagent file/directory symlinks. The 3000.3+ local probe suite must verify:

- project and user skill directory symlinks — **re-verified on 3000.3.27** by `internal/devinprobe` (`TestProbeSymlinkProjectSkillDirectory` at `internal/devinprobe/probe_test.go:299` and `TestProbeSymlinkUserSkillDirectory` at `:307`, both `PROBE PASS`)
- flat project and user subagent file symlinks — **re-verified on 3000.3.27 on 2026-08-05**, see immediately below
- stale symlink pruning behavior in Agent Marketplace

### Flat-subagent file symlinks — observed 2026-08-05 (authenticated)

**Verified on 3000.3.27 (`0becb483`), 2026-08-05T10:00:20Z, Darwin 25.6.0 arm64, authenticated Devin Enterprise account under an isolated `HOME`** (bead `agent-marketplace-5exj.31`). This replaces the 3000.2.17-only grade for the flat-subagent case; the 3000.2.17 note above is kept as history. The full reproducible procedure, including the isolation-plus-credentials arrangement that makes an authenticated run possible without touching real user state, is in `docs/devin-verification-2026-08-04.md`, appendix "the manual authenticated subagent/model run (2026-08-05)".

One `agent-marketplace sync -v` reported `Installed 2, Removed 0, Malformed 0, Errors 0` with zero warnings and installed two **file** symlinks (`test -L` true) whose targets are regular files inside the Devin compile cache:

```text
<repo>/.devin/agents/probe-agent-project.md
  -> <cache>/compiled/devin/ca85092e1ed6a7a4/subagent/probe-agent-project/probe-agent-project.md
<user>/.config/devin/agents/probe-agent-user.md
  -> <cache>/compiled/devin/5282fb21938cbdc5/subagent/probe-agent-user/probe-agent-user.md
```

Both resolved bodies carried exactly `name`, `description`, `model: sonnet`, `allowed-tools: [read, grep]`, `max-nesting: 2`.

The enumeration surface used is a running session's own `run_subagent` profile list — it exists from *inside* a session, not from a read-only subcommand, which is why this case cannot be added to the unauthenticated local probe suite.

Observed, with four controls so the result cannot be explained by the filename, a cache, or the model guessing from context, plus a fifth that checks tool enforcement on disk. The dangling-link causality control (CONTROL C) was run for **both** scopes, so it is symmetric and not project-only. The control letters are identifiers carried over from `internal/devinprobe/manual.go`, not a count — there is deliberately no CONTROL D:

| # | Observation | Result |
|---|---|---|
| 1 | Enumeration from inside the repo | Exactly `probe-agent-project`, `probe-agent-user`, `subagent_explore`, `subagent_general` |
| 2 | **CONTROL A — scope separation.** Same prompt from a directory OUTSIDE the repo | Only `probe-agent-user`, `subagent_explore`, `subagent_general`. The project profile disappeared, so it was sourced from `<repo>/.devin/agents/` and the user profile from `~/.config/devin/agents/`; the two scopes are independently established |
| 3 | **CONTROL B — the body is read through the link.** Asked for each custom profile's description | Returned `Project-scope Devin subagent symlink probe` and `User-scope Devin subagent symlink probe`, strings that exist ONLY inside the symlink targets' frontmatter. Devin dereferenced the links and parsed the compiled frontmatter rather than listing a filename |
| 4 | **CONTROL C-project — causality, project scope.** Project profile's cache target directory renamed aside so the link dangled (`test -e` false; the link itself untouched) | Enumeration returned only `probe-agent-user` plus the two builtins — `probe-agent-project` vanished. Renaming the target back restored resolution. Enumeration is causally driven by the symlink resolving |
| 5 | **CONTROL C-user — causality, user scope.** The same control for the other scope: the USER profile's cache target directory renamed aside so `~/.config/devin/agents/probe-agent-user.md` dangled (`test -e` false; the link itself untouched) | Enumeration from a directory OUTSIDE the sandbox repo returned ONLY `subagent_explore` and `subagent_general` — `probe-agent-user` vanished too. After the target was renamed back, a final enumeration from inside the repo again returned all four. Causality therefore holds at **both** scopes |
| 6 | Usability, not merely listing: both profiles spawned in the foreground | Both ran and returned their designated tokens `PROBE-PROJECT-OK` and `PROBE-USER-OK`, which appear only in the symlink targets' bodies, so the compiled system prompt reached the spawned subagent |
| 7 | Frontmatter fidelity of the spawned `probe-agent-project` | Tools reported as exactly `grep, read, read_subagent, run_subagent`: `allowed-tools: [read, grep]` honoured exactly (no `edit`, no `exec`); the nesting tools are PRESENT, which is consistent with any `max-nesting` ≥ 1 and does not establish that the VALUE `2` bounds depth |
| 8 | **CONTROL E — `allowed-tools` enforcement, checked on disk rather than self-reported.** `target.txt` containing `hello-original` was created in the sandbox repo root; the spawned `probe-agent-project` was asked to overwrite it with `CHANGED` using any edit/write tool and then to run `echo ran-exec` | It reported possessing only `grep`, `read`, `read_subagent`, `run_subagent` — no write/edit tool and no exec tool — so neither action was possible, and **`target.txt` was independently confirmed on disk afterwards to still contain `hello-original`**. The restriction is enforced in effect. Honest nuance the control exposed: the profile noted it could in principle delegate to the write-capable builtin `subagent_general`, and nesting was not restricted (`max-nesting: 2` permitted it), so `allowed-tools` bounds the profile's OWN toolset, not what it can reach through nesting |

**Not established by this run:**

- Which model actually served either spawned subagent — Devin does not label a running subagent's model in the CLI.
- That the **VALUE** of `max-nesting` bounds nesting depth. Only the presence of the nesting tools was observed, consistent with any budget ≥ 1; no nesting-depth limit was exercised.
- That a frontmatter `name:` overrides the path-derived identifier. Both probe profiles' basenames equalled their frontmatter `name`, so the override was never exercised.
- What a restricted profile can reach **through** nesting. CONTROL E bounds only the profile's own toolset.
- The directory layout `agents/<name>/AGENT.md`. Only the flat-file form was installed and exercised.

The first three of those — the serving model, the VALUE of `max-nesting`, and the frontmatter `name:` override — are tracked machine-readably as the `UnrunProbe` `subagent-frontmatter-model-and-nesting-honored` in `internal/devinprobe/unrun.go` (`ReleaseBlocking: false`). Custom subagents are marked experimental by Devin's own bundled docs, so this grade is version-bound to `3000.3.27 (0becb483)`.

**Limits of the isolation, stated rather than implied.** Isolation rested on the `HOME` override alone. The run overrode only `HOME` and `AGENT_MARKETPLACE_ORG_CONFIG`; unlike the automated suite (`internal/devinprobe/sandbox.go`, which also exports `XDG_CONFIG_HOME`, `XDG_STATE_HOME`, `XDG_DATA_HOME`, and `XDG_CACHE_HOME`) it exported no `XDG_*` variable and did not record that none was already exported in that shell, and no filesystem instrumentation (`fs_usage`, `dtruss`) was used. So "no real user path was read" is **by construction of the `HOME` override, not an observed negative**. Every observation was also taken in trust-bypassed `--print` mode (`--respect-workspace-trust false`), which is not the default interactive configuration; that the flag affects workspace trust only and not resource discovery is a reading of `/usr/local/share/devin/docs/reference/commands.mdx:32-36`, i.e. a documentation claim, not something this run observed.

### Aggregate files

The approved design structurally patches `hooks.v1.json` and `mcp_config.json`. Ownership is not inferred from Devin-specific marker fields. Durable records live under `$XDG_STATE_HOME/agent-marketplace` and are keyed by canonical output-root hash.

Aggregate and ownership writes use journaled generations so crash recovery never infers ownership from file content. Runtime probes must establish that syntax-preserving output remains accepted by Devin, including files with comments where supported. Mandatory raw-byte safety backups live under Agent Marketplace XDG state and retain five versions per output file.

## Local probe suite requirements

The implementation's local-only probe suite must use isolated temporary HOME and repository roots. It must not inspect or mutate real user configuration.

Required probes:

1. Print and assert `devin --version` is at least 3000.3.
2. Verify canonical project and user skill discovery.
3. Verify flat project and user subagent discovery. (**Settled 2026-08-05 by the authenticated manual run, not by the local suite.** 3000.3.27 exposes no read-only subagent enumeration surface; the only surface that lists loaded profiles is a running session's own `run_subagent` list, which requires authentication. The result is recorded above and as an observation in `internal/devinprobe/manual.go`; it cannot be added to the unauthenticated suite.)
4. Recompute the complete `allowed-tools` acceptance set.
5. Verify command-origin extension acceptance.
6. Verify command and prompt hook loading for every supported event.
7. Verify stdio and HTTP MCP loading, headers, disabled state, and interpolation.
8. Verify comments/order-preserving aggregate output remains accepted.
9. Verify project and user skill-directory and flat-subagent symlinks; any failure blocks release. (**Both halves are now settled on 3000.3.27.** Skill-directory symlinks pass in the local suite at both scopes; flat-subagent file symlinks were observed at both scopes on 2026-08-05 by the authenticated manual run, with four controls including the dangling-link causality control at BOTH scopes — see [Flat-subagent file symlinks — observed 2026-08-05 (authenticated)](#flat-subagent-file-symlinks--observed-2026-08-05-authenticated). The release gate this item exists to guard is cleared.)
10. Verify whether rules are discovered under canonical `.devin` paths, and record what is observed. (Originally written as "verify rules remain absent"; 3000.3.27 falsified that expectation — see the rule-discovery probe above. The probe now fails only if Devin starts ADVERTISING a `.devin` rules path, which would make rule support unambiguously expected.)
11. Verify installed model aliases and compare them with mapping-table expectations. (**Settled 2026-08-05 by the authenticated manual run, not by the local suite** — `devin models list` requires authentication, so the unauthenticated suite gets `Error: Not logged in.` See [Authenticated model-list re-capture (2026-08-05)](#authenticated-model-list-re-capture-2026-08-05).)

The suite skips with a clear reason when Devin is absent or older than the floor. It is not required in CI; deterministic fixtures and semantic round-trip tests remain mandatory there.

## Known documentation/runtime risks

- Bundled documentation and runtime behavior have diverged in prior Devin releases.
- Custom subagents and plugins have carried experimental/beta status.
- Model families behind stable aliases change over time.
- MCP storage changed at 3000.3.
- Unknown tool names may be silently filtered by Devin.

For these reasons, target behavior must fail or diagnose at compile time rather than relying on Devin to reject unsafe or lossy output.
