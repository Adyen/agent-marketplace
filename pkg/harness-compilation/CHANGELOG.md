# Changelog

## Unreleased

### Ambiguous Windsurf identities and repeated rule keys are refused

Windsurf ingestion collected workflows and rules into a map keyed by name, so a later file claiming the same name silently replaced an earlier one, for example a `deploy` workflow in `zzz.md` replacing the reviewed `deploy.md`. Every claimant of a repeated name within one workspace root is now suppressed with the new error `ingest.windsurf.duplicate-identity`; unrelated resources still convert, the workspace root still overrides the user root, and a refused workspace name does not fall back to the user root's resource. The tolerant `.mdc` frontmatter parser, used when strict YAML rejects Cursor's unquoted globs, kept the last value of a repeated key, so `alwaysApply: false` followed by `alwaysApply: true` applied the rule everywhere. It now rejects repeated keys, and the rule fails to load with the strict parser's error.

### Devin converted command skills keep their restrictions

Devin ingestion no longer recovers a skill marked `x-agent-marketplace-kind: command` as a command when the skill declares `permissions`, an `agent` or `subagent` selector, or triggers that do not include `user`. Those controls used to be dropped with `ingest.devin.command-field-dropped`, so a Devin-to-Devin round trip lost a deny policy and made a model-only resource user-invocable. The resource is now refused with the new error `ingest.devin.command-security-unsupported`, which names the fields but never their values; removing the marker ingests it as a skill with its controls intact. `model`, and extra activation next to `user`, are still dropped with the existing warning: a converted command is user-only, so dropping them only narrows it.

Devin command compilation now reserves `agent` and `subagent` alongside `permissions`, so command extension fields cannot select the execution agent of a converted command skill. The selectors are omitted with `compile.canonical-field-overrides-extra` and the mapped `allowed-tools` restriction is kept. Ordinary Devin skills keep their typed `agent` and `subagent` fields.

### The Claude plugin form no longer drops a root-level SKILL.md

`LoadBundleWithOptions` selects the plugin form for both `.claude-plugin/plugin.json` manifests and bare plugin layouts, and that form only scanned `skills/` subdirectories. A repository that IS a single skill (root `SKILL.md`, no `skills/` tree) therefore ingested zero resources with no diagnostic — the layout Claude Code documents for manifest `"skills": ["./"]` and that `github.com/blader/humanizer` ships. The plugin form now loads a root `SKILL.md` through `bundle.LoadRootSkill` when one exists. A root skill whose frontmatter name duplicates a `skills/` entry is dropped with the new warning `ingest.claude.duplicate-root-skill`; the `skills/` entry wins deterministically because it loads first in sorted order. Skill support files no longer include `.git` directories at any depth: a root skill's directory is the whole checkout, a marketplace clone keeps `.git` on disk, and VCS internals were being published into installable archives and fed to bounded support reads.

The consumer-side materializer walkers (`copySupportFilesExcluding` and the `--check` virtual digest in `internal/materialize`) now skip `.git` directories the same way. Previously a root skill's compiled cache copy contained the registry's entire `.git` object database, exposed to every workspace through the harness symlink and copied again by file-mode materialization; stale copies are pruned on the next sync's existing reconcile pass.

### Shipped model catalogs and joint reasoning conversion

Subagent compilers now share a source-aware model/reasoning resolver backed by
`modelpolicy/catalogs/<harness>.yaml`. Unknown cross-harness models warn and omit
only the model field; unsupported reasoning also warns without suppressing the
resource. Automatic cross-provider approximations are removed. Floating-family
pins are explicit catalog entries and produce informational diagnostics.

Cursor reasoning uses cataloged model-parameter encodings; Gemini no longer emits
an unsupported agent `effort` field. Devin native model strings round-trip without
requiring a Claude alias, and native default selection is distinct from parent
inheritance. Devin forward variant mappings are enabled from the captured
`devin models list` CLI inventory plus the documented custom-profile `model`
override; live acceptance in a real custom profile remains untested and is not
claimed.

Ingestion preserves native model/effort strings with `ModelSource` provenance.
Existing constructors and legacy sidecars remain readable; source-aware editable
fields take precedence over stale sidecars. The application adds source-revision
v4 for provenance-bearing IR fallback hashes while retaining frozen v1/v2/v3
projection and original-file hashing for existing exact locks.

Model updates use a harness catalog plus an independent YAML conversion fixture;
`make model-catalog-check` runs the offline maintenance gate. See the update recipe
in the library README. No runtime catalog configuration or vendor CLI dependency
is added.

### Validated native-layout descriptors now cover Codex, Devin, and Claude

`target/native_layout.go` now carries a complete `NativeLayout` descriptor for
Devin and Claude alongside Codex's existing one: scope-neutral partitions,
per-kind resource path patterns, and the shared aggregate files each kind
merges into. `NativeAggregate` gains per-scope `Project` and `User`
`NativeAggregateFile{Path, Member}` fields, which let Devin's hook aggregate
be declared per scope (project `hooks.v1.json` versus the `hooks` member of
user `config.json`); the single `Path` field is gone. `NativeAggregatePath`
now refuses a scope-variant aggregate rather than answering for one scope;
`NativeAggregateFileForScope` is the scope-aware accessor.

`ValidateTargetRegistrations()` runs the new `ValidateNativeLayouts` at the
registration boundary, rejecting duplicate partition IDs or prefixes,
invalid or escaping paths, malformed `%s` patterns, and declarations
inconsistent with the target's declared root model, and rejects a
partitioned-root target that registers no layout. No emitted bytes change:
the descriptors describe the layout the compilers already emit, pinned
against real compiler output by the descriptor pin tests in `target` and the
consumer-side `internal/materialize` artifact-path test.

### Codex is a first-class native target

Codex is the seventh bidirectional compiler/ingester/conversion target. It
covers all five target-materialized kinds without plugin packaging:

- skills and converted commands use `.agents/skills/<name>/SKILL.md`, with the
  command provenance marker `x-agent-marketplace-kind: command`;
- subagents use `.codex/agents/<name>.toml`;
- command hooks merge into `.codex/hooks.json`; and
- stdio or remote MCP servers merge into `[mcp_servers.<name>]` tables in
  `.codex/config.toml`.

The application resolves those artifact prefixes into repo `.agents`/`.codex`
roots and machine `~/.agents` plus `$CODEX_HOME` (or `~/.codex`). It reconciles
the two shared files through durable ownership and mandatory backups, preserves
unmanaged entries, and reports repo trust review without editing Codex trust
state. Missing/corrupt ownership and malformed or unpatchable aggregate forms
fail closed.

Conversion to Codex refuses any destination that already contains
`.codex/hooks.json` or `.codex/config.toml`; use a new or empty destination so
one-shot conversion never overwrites shared configuration. Sync, check,
ingestion, and conversion do not install, require, or probe a Codex executable.

The mapping is evidence-bound to the official Codex skills, subagents, hooks,
and MCP documentation as of 2026-08-19. Benign metadata loss warns; any loss of
tool/sandbox restrictions, executable behavior, authentication, transport, or
interpolation semantics suppresses only the affected resource or aggregate
entry with a stable diagnostic. `.agents/skills` alone is not autodetected as
Codex. Rules and `AGENTS.md` generation remain out of scope, and deprecated
`.codex/prompts/*.md` files are ingest-only migration input, never compiler
output.

### `diag` names the severity threshold so consumers cannot get the ordinal direction wrong

`SeverityError` is the ZERO value of `Severity` (error 0, warning 1, info 2), so
the ordinals count DOWN in gravity. Nothing in the API said so, and every
consumer that wrote the obvious-looking `d.Severity >= diag.SeverityError` got a
condition that is true for every severity; `<` gets one that is true for none.
Four sites in `agent-marketplace` had it wrong, which is a library-shaped
problem, not four independent mistakes.

Two additions, no behaviour change to compilation or diagnostics:

- `Severity.AtLeastAsSevereAs(threshold)` — reports whether a severity is at
  least as grave as a threshold, and is the only place the ordinal direction is
  written. The name says "severe" rather than "at least" because an ordinal
  reading of the name would be backwards.
- `Diagnostic.IsError()` — the fail-closed-error predicate, next to the existing
  `HasErrors` helper, which now delegates to it.

The ordinal VALUES are deliberately unchanged. They are wire-visible and
consumer UIs key off `diag/codes.md`, so renumbering them to make magnitude
comparisons read naturally would silently change behaviour for every consumer.
No catalog entry changes; `codes.md` only gains a section stating the direction
and naming the predicates.

### An unrepresentable Factory `tools` restriction now omits the key instead of costing the droid

**This reverses a documented, tested contract on purpose.**
`subagent.factory.tool-unknown` was an error that suppressed the droid artifact;
it is now a **warning** that omits the `tools` key from a droid that is still
emitted. `disallowedTools` and `isolation` remain fail-closed errors because
Factory has no field for those controls.

A subagent `tools` list containing an unknown Factory tool produces one warning
per unmapped tool and omits the entire `tools` frontmatter key. `sync` exits 0
and installs the droid. Explicitly empty lists still render `tools: []`, fully
mappable lists still render their mapped values, and tool overrides still work.
Omitting the key intentionally inherits Factory's default tool set; emitting an
unknown identifier would cause Factory to reject the droid.

### An unrepresentable Devin `allowed-tools` restriction now omits the key instead of costing the resource

**This reverses a documented, tested contract on purpose** (bead
`agent-marketplace-pu6b`). `skill.devin.tool-unknown` and
`subagent.devin.tool-unknown` were errors that suppressed the artifact; they are
now **warnings** that omit the `allowed-tools` key from an artifact which is
still emitted. It applies to every Devin resource kind — skills, converted
commands, and subagents — because `resolveAllowedTools` is shared by all three
call sites and a security-shaped rule must not drift between kinds.

**What broke.** Three skills selected by `agent-marketplace`'s own committed
`.agents/config.yaml` (`openspec-apply-change`, `openspec-archive-change`,
`openspec-explore`) each carry `allowed-tools: Bash(openspec:*)`.
`resolveAllowedTools` rejects any entry containing a parenthesis, so three
resources produced three errors, `sync` exited non-zero, and nothing was
installed. Scoped `Bash(...)` is what the openspec generator emits, so this was a
class of real Claude-authored skills, not an authoring mistake. The non-zero exit
also gated off `--prune-stale`, so unrelated stale-selection cleanup stopped
working too. The user's only available fix was editing a registry they may not
own.

**What changed, precisely.** All three failure modes behind these two codes now
take the same outcome — the key is omitted, one warning is emitted per offending
entry, the artifact is published:

| Failure mode | Before | After |
|---|---|---|
| Scoped specifier (`Bash(openspec:*)`) | error, no artifact | warning, artifact without `allowed-tools` |
| Name outside the mapping table (`Write`, `WebFetch`, `WebSearch`, `mcp__*`) | error, no artifact | warning, artifact without `allowed-tools` |
| Unparseable `allowed-tools` shape (e.g. a mapping) | error, no artifact | warning, artifact without `allowed-tools` |

There is no half-mapped middle: if any entry fails, the WHOLE key is omitted
rather than the mappable subset being emitted. A partial list is narrower than
the source asked for and can leave the resource unable to do its job.

**Unchanged:** an explicitly empty restriction still renders `allowed-tools: []`;
a fully mappable restriction still renders the mapped list;
`compile.WithToolOverride` still resolves names the table does not know; and no
diagnostic, hint, or log attribute quotes the offending specifier (they carry
command strings such as `Bash(git push --force:*)`). The last property has its
own tests for both the skill and subagent paths.

**Omitting the key inherits Devin's default tool set, which is a real widening
relative to the source's stated restriction. That is the deliberate point of the
decision, not an oversight.**

The justification is stated no more strongly than the evidence allows, because
"Devin ignores `allowed-tools`" would be false. Devin **does** enforce a list it
can represent: CONTROL E on 3000.3.27 verified *on disk* that a profile compiled
with `allowed-tools: [read, grep]` could neither overwrite a file nor run a shell
command (`docs/devin-cli-harness-spec-3000.3.md`). What Devin cannot do is
*represent* the restriction these codes are about: it has five bare names, no
per-command scoping, and the D12 probe result recorded further down this file
found every other name is silently dropped rather than rejected. Every available
output is therefore an unfaithful representation, and the decision is which one to
write — no resource, a narrower restriction than the source asked for, or Devin's
default set stated honestly by the absence of the key. The same control also
found that even a representable list bounds the profile's own toolset rather than
what it reaches through nesting, so `allowed-tools` is not a hard privilege
boundary on Devin either way.

**Two alternatives were considered and rejected.** Keeping the error leaves the
resource unavailable with no fix in the user's hands. Emitting `allowed-tools: []`
is the narrowest representable value and never broadens — the move `mapTriggers`
makes for an all-dropped trigger list — but the analogy breaks: `triggers: []`
only stops auto-activation and leaves the skill usable, whereas
`allowed-tools: []` means the resource may use **no** tools at all, and
`openspec-apply-change` needs `exec` to run `openspec`. That trades a loud absence
for a silent, inert install.

**Devin and Factory no longer diverge on tool-unknown, and the remaining split is
accepted rather than accidental.** `subagent.gemini.tool-unknown` and
`subagent.cursor.tool-unknown` still fail closed; `subagent.factory.tool-unknown`
was relaxed because Factory's droid frontmatter rejects unknown tool identifiers
and omitting the `tools` key uses Factory's documented default tool set.
`ingest.devin.tool-unreversible` also stays an error, because ingestion writes the
harness-neutral IR that every registered compiler reads, so a rewritten restriction there
propagates to targets that do enforce it.

**Decision records updated rather than quietly corrected.** The invariant "a
privilege-broadening loss must be an error" is now false for these two codes and
is amended, in place, with the reason:

- `diag/codes.go`: both constants moved from the error block to the warning
  block with the full rationale; the "split by OUTCOME" comment block and the
  Devin-ingest block now state the narrower honest rule ("an error when the
  emitted output would misrepresent a control the target actually enforces").
- `diag/codes.md`: both rows moved from `## Error` to `## Warning`; a new
  dedicated section records the reversal, the rejected alternatives, and the
  Factory divergence; the "What an error suppresses" section records that these
  two codes are no longer in that set.
- `field-matrix.md`: the Devin `allowed-tools` and Subagent `tools` cells.
- `compile/devin/{fields,skill,subagent}.go`: the rule, the `mapTriggers`
  contrast, and why `isolation`/`disallowedTools`/built-in-name conflicts are
  deliberately NOT reversed (Devin has no field for the first two, and a
  built-in name is silently shadowed).
- `testdata/matrix/subagent/matrix.yaml`: `cursor-to-devin`, `factory-to-devin`,
  and `gemini-to-devin` now emit a profile, so each gained an expected-loss
  entry for `tools` plus the diagnostics the compiler could not previously
  reach (`subagent.devin.effort-dropped`, `subagent.devin.model-unmappable`) and
  a new golden directory. `claude-to-devin` still produces nothing, but now
  because of its two `subagent.security-restriction-unsupported` ERRORS, with
  the tool warning riding along.
- Tests: `TestSecurityFieldFailuresSuppressArtifacts` became
  `TestUnrepresentableAllowedToolsOmitTheKey`, and every case it used to assert
  produced zero artifacts now asserts the exact emitted bytes, so a silent
  return to suppression fails. Same for the subagent equivalents and the two
  bundle-level tests. `internal/materialize`'s prune-scope fixture switched its
  "erroring subagent" from an unmappable tool to `isolation`.

**Blast radius on emitted bytes.** No non-Devin golden file changed. On Devin,
the only byte change is that an artifact which previously did not exist now
exists without an `allowed-tools` key; nothing that compiled before compiles
differently.

### Adversarial follow-up: the widened path is no longer reachable by accident

Three defects found while attacking the reversal above. Each one only became a
privilege question *because* of it: while an unrepresentable restriction cost the
resource, all three were merely "different ways to get the same error", and now
they are "different ways to get Devin's default tool set".

- **The empty-vs-absent boundary turned on the YAML shape.** `allowed-tools: ""`,
  `"  "` and `","` were trimmed and split down to nothing and rendered
  `allowed-tools: []` — no tools. The identical restriction written as a list,
  `[""]`, `["", " "]` or `[","]`, was not normalized at all, so it carried a
  blank entry that matched no tool, which after the reversal omits the whole key
  and inherits **every** default tool. Two spellings of "no tools" produced "no
  tools" and "all tools", and nothing but the author's choice of YAML shape
  decided which. The same asymmetry made `[" Read "]` unmappable while
  `" Read "` mapped to `read`, so a trailing space in a scalar replaced a
  read-only restriction with the full default set. All accepted shapes are now
  normalized identically by `normalizeToolEntries` before anything is classified
  (split on commas, trim, drop blanks). It only ever removes blanks and never
  invents a name the source did not write, so it cannot broaden a restriction;
  scoped fragments still carry their parenthesis and still drop the key whole.
- **`compile.WithToolOverride` could not rescue the input the bead exists for.**
  The scoped-specifier check ran before the override lookup, so a consumer who
  explicitly declared `Bash(openspec:*) -> exec` was ignored and got Devin's
  default tool set instead of `allowed-tools: [exec]`. The override is now
  consulted first: it is a per-invocation statement of intent, and discarding it
  in favour of a strictly wider outcome is a lost restriction, not a safe
  default.
- **A suppressed subagent claimed a tool set applied to it.** When a profile
  still fails closed on `isolation`, `disallowedTools`, or a built-in name
  conflict, its tool warning still rides along (deliberately — one compile shows
  every finding), but it said "the allowed-tools key was omitted and Devin's
  default tool set applies" about an artifact that was never written. The wording
  on that path now says the profile was not written. Code and severity are
  unchanged.

Attacks that did NOT find anything, recorded so they are not re-run blind: a
partial/narrowed list is never emitted for any mixed input; no diagnostic, hint
or `slog` attribute at any level (including Debug) quotes the offending value,
and the raw extension value never reaches the artifact on any of the skill,
command, or subagent paths; the fail-closed subagent fields still suppress and
still carry their error; and Claude, Factory, Cursor, Windsurf and Gemini remain
byte-identical, still failing closed on the same inputs.

Recorded but not fixed, because fixing it means reversing the accepted decision:
the widening is not confined to Devin. A Devin artifact written without an
`allowed-tools` key is a legitimate conversion SOURCE, and ingest cannot tell
"the author wanted no restriction" from "the compiler could not spell one", so
`convert --from claude --to devin` followed by `convert --from devin --to claude`
yields an unrestricted Claude skill. `ingest.devin.tool-unreversible` cannot
catch it: there is no unreversible tool left in the file, only an absence.
`TestDevinWideningLaundersIntoAnEnforcingTargetOnRoundTrip` pins that exit.

### The Devin model table cites a recorded capture instead of implying a check you can rerun

Comment-only change in `compile/mapping` from bead `agent-marketplace-5exj.31`.
No behaviour change and no semantic diff to go hunting for: not one mapped value
in `models.go` or `tools.go` moved, so no golden tree can move either.

`models.go` said each Devin alias was "verified on 3000.3.27
(`devin models list`)" next to a suite that never authenticates. That check
requires a logged-in account, so the sentence implied a reproducibility the
package does not have — the unauthenticated probe suite cannot run it, ever. An
authenticated re-capture (28 families: `opus`→Claude Opus 5, `sonnet`/`claude`→
Claude Sonnet 5, `haiku`→Claude Haiku 4.5, `gemini`→Gemini 3.5 Flash, `gpt`→
GPT-5.6 Terra, `codex`→GPT-5.3-Codex, `swe`→SWE-1.7) confirms the aliases the
Devin cells depend on, and the comments now cite that recorded capture instead
of implying a local assertion.

The capture also justifies the table's shape after the fact: the alias SET was
stable while the families behind the aliases had drifted (`swe` is now 1.7,
`gpt` is now 5.6 Terra), which is exactly why the Devin cells map aliases and
do not pin families.

**One justification did not survive review.** The `gemini-3-preview`
(Pro-class) row explained its `modelUnmappable` by asserting there is no stable
Pro-class identifier, on the evidence that the capture's `gemini-3.1-pro` family
carries no alias. "No alias" does not establish "no usable identifier": nobody
tried `model: gemini-3.1-pro` as a value. The row still maps to
`modelUnmappable` — the plan forbids guessing versioned IDs — but it now records
what was tested rather than a conclusion that outran it.

`tools.go` separately still described its 3000.3.27 re-verification of the
closed `read/edit/grep/glob/exec` allowed-tools vocabulary as "still pending"
when the probe exists and passes. Corrected.

### A hook the marketplace already wrote no longer moves on the next sync

Factory's `hooks/hooks.json` has no envelope and its array elements are rule
GROUPS — `{"matcher": "Edit", "hooks": [{"type": "command", ...}]}` — with no
`name`. The merge identified array elements by `name` only, so a managed rule
group was unidentifiable: every one of them was dropped and the desired set was
re-appended at the END of the array. A sync that changed nothing therefore
still rewrote the file, moved the user's own rule groups up past ours and —
because a comment belongs to the element that FOLLOWS it — left their comments
sitting above our generated groups.

Bead `.26` did not already cover this. It made the merge converge (the second
sync of an unchanged file is now a no-op), but the FIRST sync after a user
edited around our entries still shuffled the file, which is precisely the
byte-identity the acceptance criterion asks for.

A managed element with no name is now identified by its BEHAVIOR TUPLE, the
same identity `compile/devin/hook.go` gives a Devin hook: for a Factory rule
group that is (event, matcher, type, payload, timeout, extras), realized
exactly by the event key plus the canonical bytes of the group without our
ownership marker. Nothing in a rule group is anything but behavior, and there
is no member Factory reads that a compiler could stamp an id onto.

The ordering guarantee is now the same for named and unnamed elements: every
surviving element keeps the index it already had, a superseded managed element
is rewritten IN PLACE in the slot it already held, and only genuinely new
elements are appended — named ones sorted by name, then unnamed ones in the
compiler's own deterministic order. A first-ever write has no existing
elements, so the output is that sorted tail alone and the golden trees do not
move.

**Claude, Cursor, and Gemini hooks had the same defect** and are fixed by the
same change rather than a parallel one. Their hooks use the
`{"hooks": {"<event>": [...]}}` envelope, which took a different code path that
also dropped every managed entry and re-appended it after the user's; both hook
shapes now share one implementation and one ordering contract.

One `jsonc` change came with it: an element that survives at a different index
is carried over whole, its own leading trivia included, instead of inheriting
whatever trivia the index it lands on used to hold. Deleting a superseded
generated hook used to delete the comment the user had written above the hook
BELOW it.

All then-existing golden trees are byte-for-byte identical. Benchmarks (`benchstat`,
5×300ms): the steady-state paths are flat; the worst case in the suite — 100
stale named array entries, none reusable, all replaced — is +5.6% time and
+2.6% allocs for the trivia-preserving element match, on a container that is
rewritten wholesale anyway. A new `BenchmarkMergeWithExistingJSONHooks` guards
the unnamed-element path (steady, changed, grown) that this bead introduced.

### The aggregate WRITE path stopped destroying comments, key order, and duplicate keys

`artifact.MergeWithExistingJSON` ended in `json.MarshalIndent`, so every merge
into an existing file re-serialized the user's whole document. Comments were
deleted, every key was sorted, and duplicate keys were collapsed
last-value-wins and then **persisted**. Bead `.18` fixed exactly this for the
PRUNE path by moving it onto `pkg/harness-compilation/jsonc`; the write path
was never covered, and it is the far more frequent one — a prune happens when a
resource is removed, this runs on every ordinary sync of Claude, Factory,
Cursor, Windsurf, and Gemini. Devin was never affected: it reconciles through
`internal/ownership` and the `jsonc` patcher and does not call this function.

The fix reuses the same single concrete-syntax layer rather than adding a
second patcher. The merge DECISIONS still run on decoded values, so all three
merge shapes are unchanged; the RESULT is now applied to the parsed document as
a minimal patch. A member that already says what we want is not touched at all,
so it keeps its exact bytes, its position, and the comments written above it.

- **Duplicate keys fail closed** with the existing `json.duplicate-key`
  `*diag.CodedError`, the same code the loaders got in `.16` and the prune path
  in `.18`. No new code was invented: the outcome is identical (refuse the file,
  write nothing, name the file and line, never echo the value). The destination
  is byte-identical after a refusal and no staging directory survives.
- **A commented file is no longer a hard failure.** `encoding/json` rejected
  `//` and `/* */` outright, so a user who commented their `mcp.json` got
  `invalid existing JSON` and a failed `sync`. It now parses, and the comments
  survive the merge.
- **Array containers keep the destination's order.** Surviving entries stay at
  their original index and genuinely new entries are appended sorted by name,
  replacing the old "all managed entries sorted first, user entries pushed to
  the back" order, which re-sorted the user's file on every sync and — because
  a comment belongs to the element that follows it — handed their comments to
  our generated entries. A first-ever write has no existing entries, so the
  output is the sorted tail alone and the golden trees do not move.
- **An empty desired container no longer deletes user entries.** `{}` matches
  the map-of-arrays shape and the keyed-object shape equally well, so the
  destination now decides which one it is. Sending `{"mcpServers": {}}` (the
  last managed server removed) used to go down the map-of-arrays path, look for
  arrays, find the user's server objects, and drop every one of them.
- A `null` destination decoded to a nil map and panicked on the first
  assignment. It is now treated as an empty document.

`jsonc` grew four small, documented members for this — `Document.Decode`,
`Document.ReplaceRoot`, `Node.Canonical`, and `Node.AppendIndent` — so the
patch can decide on values, render an inserted entry at the file's own
indentation, and replace a root that has nothing to preserve.

Benchmarks (`benchstat`, 5×2s): the keyed-object shape that three of the five
targets use is flat (`small` +3.7%, `large_existing` no significant change) and
a steady-state re-sync of a 100-entry array container is ~4% FASTER than the
old re-marshal. The worst case — 100 stale array entries, none reusable — is
+51% (177µs → 268µs) for a container that is rewritten wholesale anyway. A
no-op sync now writes nothing at all, because the merge output is a fixed point
and `Materialize` skips a file whose bytes already match.

### The entry-removing MCP interpolation loss gets its own error code

`mcp.interpolation-unsupported` was doing two jobs. On Devin it fires both when
one value is omitted from a server entry that is still written (an argument, an
`env` value, a header, an OAuth member) and when the omitted value is the field
the server's documented form cannot do without — `command` on a stdio server,
`url` on a remote one — in which case the whole keyed entry disappears. It is a
warning, so the second case dropped an MCP server and let `sync` exit 0. That
was the one hole left in acceptance criterion 12 after bead
`agent-marketplace-5exj.20` made the eight other Devin entry-skip codes errors.

**A code that is used for two outcomes is the defect; the fix is to split it,
not to argue about its severity.** Bead `.20` established that severity follows
the OUTCOME: a code that suppresses an entry is an error, a code that drops an
inert sub-field is a warning. So:

- New ERROR code **`mcp.devin.interpolation-unrepresentable`**, emitted only
  when the unrepresentable value is the `command` of a stdio server or the
  `url` of a remote server. The entry is skipped, siblings are still patched
  atomically, and `sync` exits non-zero — the same contract as
  `mcp.devin.transport-ambiguous`, `-incomplete`, and `-unsupported`.
- `mcp.interpolation-unsupported` keeps its **warning** severity and now means
  exactly one thing: a single value was omitted (never blanked) from an entry
  that was still emitted.
- `compile/devin`'s interpolator grew a `requiredValue` sibling to `value`; the
  mapping itself is shared in `mapValue`, so only the reporting differs. The
  slog level follows the severity, as it already does in `hook.go`.

**Escalating the shared code instead would have been a regression, not a fix.**
A resource that emits any error diagnostic contributes no artifacts at all
(per-resource suppression, bead `.17`), so flipping the shared code to error
would have started deleting an entire MCP server because one `env` value used
an unmappable interpolation style — the exact inversion of the emit-the-entry
rule. A per-target severity mechanism was rejected for a different reason: the
catalogue's value is that a code means one thing, and consumer surfaces
(`status`, `doctor`, the TUI) key off the code.

**The other five targets are deliberately unchanged, and cannot reach either
case.** Devin is the only emitter of `mcp.interpolation-unsupported`: Claude,
Factory, and Cursor write MCP values through verbatim with no interpolation
mapping, Gemini uses its own env-token table with no unmappable outcome, and
the Windsurf column of `compile/mapping/mcp.go` declares a pattern for all three
source styles, so `McpInterpolationSupported` never returns `ok=false` for it.
`TestMcpInterpolationUnsupportedIsDevinOnly` pins that, and fails if any of the
five starts emitting the code or if Devin stops.

Tests: `TestMcpInterpolationSeverityFollowsTheOutcome` runs the same
unrepresentable style through both outcomes in one table;
`TestMcpFailureShapesEmitNoArtifact` now asserts that every entry-removing
branch reports an error; `TestMcpDiagnosticsNeverQuoteValues` already covered
the new branch and its completeness guard now requires it. No message, hint, or
log attribute carries the value.

### `rule.devin.unsupported` is restated as a scope decision and downgraded to a warning

The code kept its name and its outcome — a rule compiled for Devin still emits
no artifact — but the reason it gave was false and the severity followed from
that false reason.

**What was wrong.** The diagnostic said "Devin has no rules directory
primitive", `diag/codes.md` repeated it, and the evidence snapshot concluded
"there is no canonical `.devin/rules` project path … the Devin target
*therefore* emits an explicit unsupported diagnostic". The D12 runtime probe
falsified the premise on Devin 3000.3.27 and it was reproduced independently:
`devin rules paths` advertises only `.windsurf/rules/*.md (always-on)` and
`.cursor/rules/*.md (conditional)`, but a file written to
`.devin/rules/<name>.md` **is** discovered — `devin rules list` prints
`probe-rule [Devin] manual`.

**Why rule support was still not implemented.** The exclusion never rested on
that claim. Rules are not marketplace-selectable at all — `internal/config.AllKinds`
has no rule kind, for any harness — and "marketplace-selectable rules" is an
explicit non-goal of the promoted Devin plan, which is a statement about the
selection model that no runtime observation can falsify. Emitting into
`.devin/rules` would additionally mean depending on a path Devin does not
advertise, which is the same undocumented dependency `mcp.devin.transport-unsupported`
and `ingest.devin.tool-unreversible` exist to refuse. If Devin ever starts
advertising a `.devin` rules path, the probe fails loudly and the decision is
reopened.

**Why the severity changed.** ERROR suppresses the resource and makes `sync`
exit non-zero; that is the right signal for the rest of the Devin error family,
every member of which marks output that would be actively *wrong* if emitted (a
broadened privilege, an unenforced restriction, a silent transport fallback, a
shadowed built-in). An out-of-scope rule is none of those: nothing is written,
no aggregate entry is at risk, and the loss is the same class the Claude and
Gemini rule codes report as warnings. `rule.devin.unsupported` now sits with
`rule.claude.no-primitive` and `rule.gemini.no-primitive` at `SeverityWarning`.
Devin is in fact the least lossy of the three — a user can place the file by
hand and Devin will read it — so an error there while Claude only warned was
backwards.

**Blast radius.** No golden tree and no `testdata/matrix` fixture contains a
rule, so no fixture bytes changed. `field-matrix.md` gained a Rule row (it had
none), `diag/codes.md` gained a dedicated section explaining the distinction,
and the code moved from the Error table to the Warning table in both `codes.go`
and `codes.md`. Two tests that had used a Devin rule as a convenient *error*
resource now use a Devin subagent that shadows a built-in, and a new
`TestDevinRuleIsALossyButCleanCompile` runs one rule through Devin, Claude, and
Gemini so the three cannot drift apart again.

### `jsonc` is now the consumer's marker-prune write-back parser too

No library code changed, but the `jsonc` contract gained its second consumer and
`diag/codes.md` records it. `agent-marketplace`'s `stripManagedEntriesFromFile`
(the path that deletes `_managed_by` entries from a user's `mcp.json`,
`hooks.json`, `plugin.json`, or `settings.json` and writes the file back) used
`encoding/json` and therefore collapsed duplicate object keys last-value-wins
**and persisted the collapse**. It now goes through `jsonc.Parse`, so:

- a duplicate key is rejected with the same `json.duplicate-key`
  `*diag.CodedError` the loaders emit, and the destination file is not written;
- comments, key order, and the exact bytes of untouched members survive the
  patch, which is what `jsonc` exists to guarantee.

`jsonc` remains an exported package precisely because `internal/jsonutil` is not
importable from outside this library; no new surface was exported for this.
The `json.duplicate-key` catalog entry now names the write-back path explicitly
as the case where a collapse would have been written into a user's own file.

### Devin hook and MCP entry-skip diagnostics are errors

The eight `hook.devin.*` / `mcp.devin.*` codes that REMOVE a desired aggregate
entry are now `SeverityError`:

| Code | Why an error |
|---|---|
| `hook.devin.event-unmappable` | no rule is emitted for the hook |
| `hook.devin.unsupported-type` | no rule is emitted for the hook |
| `hook.devin.matcher-unsupported-event` | no rule is emitted; emitting it without its matcher would widen activation |
| `hook.devin.payload-missing` | no rule is emitted; a typed hook with no payload is not a hook |
| `hook.devin.timeout-invalid` | no rule is emitted; a negative timeout could read as unbounded |
| `mcp.devin.transport-ambiguous` | no server entry is emitted |
| `mcp.devin.transport-incomplete` | no server entry is emitted |
| `mcp.devin.transport-unsupported` | no server entry is emitted; dropping the transport would silently default it |

This completes the promoted plan's sentence, not just its first half: "invalid
or conflicting desired entries are skipped while other valid entries are patched
atomically; **sync still exits non-zero**". Warning severity delivered the skip
and swallowed the signal.

The four sibling codes that still EMIT their entry deliberately stay warnings:
`hook.devin.payload-dropped` and `mcp.devin.field-dropped` drop an inert
sub-field the selected hook type or server form would never have used, and
`hook.devin.plugin-root-unresolved` / `mcp.devin.plugin-root-unresolved` leave a
`${CLAUDE_PLUGIN_ROOT}` token unexpanded in a value that is otherwise emitted
verbatim. Escalating them would suppress an entry that is otherwise exactly what
the source authorized.

`mcp.interpolation-unsupported` is a shared VALUE-level warning that omits an
unrepresentable interpolation from an otherwise emitted entry. The required
`command` or `url` of a Devin MCP server is not optional: if interpolation would
leave either empty, the whole entry is skipped with ERROR
`mcp.devin.interpolation-unrepresentable`. The shared warning is deliberately
untouched because five other targets emit it for value-level losses only.

Blast radius is bounded on both sides. Compile-side suppression has been
per-resource since the previous release note below, so an error costs one hook's
or one server's fragment. Consumer-side prune suppression was narrowed first
(see the consumer repository's changelog): `internal/materialize` blocks pruning
per output COMPONENT, and a Devin hook or MCP server writes to a shared
aggregate FILE, so an erroring hook now suppresses pruning of `hooks.v1.json`
alone. `Materialize`'s bundle-wide `WriteReport.PruneSuppressed` is unchanged; it
governs the one-shot `convert`/`hcc` path, not `sync`.

The rationale paragraphs in `diag/codes.go` and `diag/codes.md` were rewritten
from "pending severity review" to the landed state, `field-matrix.md` now marks
each skipping case ERROR, and the diagnostics' `slog` records for the escalated
branches moved from `Warn` to `Error`. No golden tree or matrix cell changed: no
fixture exercises a skipping Devin hook or MCP shape.


### Every golden tree is compared again, with no exemptions

`TestGolden_AllTargets` silently stopped comparing four of six trees at D2. The
shared bundle's subagent carried `disallowedTools` and `isolation`, which every
target except Claude suppresses with an error, and bundle compilation then
published nothing at all, so the test returned before reaching the comparison.
Factory, Cursor, Windsurf, and Gemini therefore had no byte-level regression
coverage through the entire Devin work sequence.

Per-resource error suppression made the trees reachable again; this change
removes the cause. The shared subagent fixture no longer carries restrictions
only one target can represent, so every compiler target compiles the shared
bundle cleanly. The exemption list is deleted, and so are the per-target bundle
override and the duplicated Devin fixture bundle that existed only to work
around the same problem.

Suppression behaviour did not move to nowhere: the subagent conversion matrix
input still carries `disallowedTools`, `isolation`, and `effort`, and every
`*-to-*` cell still declares them as expected losses, so a target that stops
suppressing them fails there.

One genuinely stale byte surfaced, exactly as predicted: the Cursor
`hooks/hooks.json` fixture was missing the `"type"` member the Cursor hook
compiler emits unconditionally. The remaining fixture changes all follow
mechanically from the subagent fixture edit.

### D12 release certification and the Devin runtime probe harness

The consumer repository gained `internal/devinprobe`, a local-only, opt-in
runtime probe suite for the Devin CLI compatibility contract, plus a
certification document (`docs/devin-verification-2026-08-04.md`) that grades all
17 of the promoted plan's acceptance criteria. The library itself is unchanged
by this work; what follows is the part of the result that constrains this
library's mapping tables and evidence comments.

Four assumptions this library encodes were confirmed against Devin 3000.3.27:

- **The `allowed-tools` vocabulary is exactly `read`, `edit`, `grep`, `glob`,
  `exec`.** `compile/mapping/tools.go` pinned this against 3000.2.17 and flagged
  3000.3.27 re-verification as required; it is now re-verified. The probe tested
  a 22-name candidate superset and found that every other name is *silently
  dropped* by Devin rather than rejected — which is the concrete justification
  for `skill.devin.tool-unknown` being an error that suppresses the artifact
  instead of a warning that narrows the list.

  **The observation stands; the conclusion drawn from it was overturned by bead
  `agent-marketplace-pu6b` (see the Unreleased entry at the top of this file).**
  Silent dropping still forbids passing an unmapped name through to the artifact,
  and that part of the compiler is unchanged. What the observation never
  established is the *second* half of the sentence — that losing the whole
  resource is the proportionate response. Read together with CONTROL E (a
  representable list IS enforced), what it actually shows is that the restriction
  in question cannot be represented at all, so every possible output is
  unfaithful and suppression buys the user nothing. The code is now a WARNING
  that omits the key and publishes the resource;
  `subagent.devin.tool-unknown` moved with it, and the Factory, Gemini, and
  Cursor siblings did not.
- **The `x-agent-marketplace-kind: command` extension is accepted**, and
  `triggers: [user]` really does suppress model invocation. The evidence
  snapshot graded this Inferred.
- **`triggers: []` is accepted and means an empty trigger set.** `compile/devin`
  emits the explicit empty list rather than omitting the key, because omission
  inherits Devin's `user` + `model` default and would broaden activation. That
  fail-closed choice is now backed by an observation instead of an argument.
- **A comment-bearing `mcp_config.json` with unknown members still loads**, so
  the `jsonc` package's syntax-preserving output is accepted for that file.
  This does **not** transfer to `hooks.v1.json` or `config.json`, which have no
  observable surface in 3000.3.27.

Three evidence defects were recorded rather than papered over:

- `compile/mapping/models.go` comments claim the `sonnet`/`opus`/`haiku` aliases
  were "verified on 3000.3.27 (`devin models list`)", but that command requires
  authentication and cannot be run by the probe suite. The underlying capture is
  real and recorded in the consumer's evidence snapshot; the comments should
  cite that capture rather than imply local re-verifiability.
- `compile/devin/mcp.go` treats `{http, sse}` as the closed documented remote
  transport set, but the literal string `sse` is inferred from prose ("may fall
  back to SSE") and never appears as a documented value. Probing found the
  question **unobservable** on the read-only surface: `devin mcp list`/`get`
  never echo `transport`, so a garbage transport loads indistinguishably from a
  valid one. The compiler's skip-on-undocumented-transport behavior stays as the
  fail-closed choice.
- Devin 3000.3.27 **does** read `.devin/rules/*.md`, contradicting the recorded
  justification for `rule.devin.unsupported`. The diagnostic itself is still
  correct — rules are an explicit non-goal of the Devin target — but its
  rationale in `diag/codes.md` and `field-matrix.md` describes a capability
  limit that does not exist and must be restated as a scope decision.

Still awaiting a live probe and unchanged by this work: every hook question
(per-event loading, `PermissionRequest` matcher acceptance, the default `type`
when absent, and `timeout` units and zero-semantics), flat-subagent discovery
and file-symlink following, and `${env:VAR}` interpolation. Devin 3000.3.27
exposes no read-only hook or subagent surface, and a malformed `hooks.v1.json`
produces no error from any command, so "no error" is not evidence of loading.
The `timeout` gap is the sharpest of these: the compiler passes `0` through
unexamined, and if Devin reads `0` as unbounded, an immediate bound could become
no bound.

Bundle-compile benchmarks were re-verified on the current tree
(`-benchtime 2s -count 5 -benchmem`, darwin/arm64). Allocation counts are stable
to within one allocation across all five samples on all eight `CompileBundle*`
sub-benchmarks, matching the "allocations unchanged" claim recorded when
per-resource error suppression landed. Full numbers are in the certification
document.

### Devin becomes selectable in consumer UI (target registry)

`target.Definition` for `devin` now declares `UI: true, UIOrder: 5`. The field
was reserved for work package D11 and nothing else about the target changed:
`UI` drives the consumer's TUI checkbox list only, so Devin is now offer**able**
without being default anywhere. The plan's "Existing and new default harness
lists remain unchanged" is unaffected — this registry has never contained a
default harness list.

### Devin doctor and repair diagnostic codes

Four new consumer-side codes back work package D11.
`doctor.devin.version-unsupported` and `doctor.devin.version-unknown` are
warnings and neither blocks anything, because the plan states "Sync does not
require a Devin binary. `doctor` detects an installed binary and warns when its
version is below 3000.3." `doctor.devin.recommended` is info: it implements
"recommends selecting the harness when installed but unconfigured" and never
edits a harness list itself. `doctor.repair.failed` is the only repair error and
maps to one half of the plan's exit contract, "exit 1 means corrupt state/file
or partial repair failure"; declining a prompt and running off-TTY are clean.

Like the `ownership.*` and `materialize.devin.*` codes they are emitted by the
consumer rather than by a compiler, and are catalogued here because `codes.md`
is the one stable vocabulary consumer surfaces key off. None of their messages,
hints, or log attributes may carry user file content, an aggregate payload, a
hook payload, a matcher, an MCP env value, a header, or an OAuth secret.

### Devin materialization diagnostic codes

Six new codes back work package D10's consumer-side Devin aggregate
reconciliation. `materialize.devin.aggregate-malformed`,
`materialize.devin.aggregate-shape`, and `materialize.devin.aggregate-write` are
errors that block exactly ONE aggregate file and leave it byte-for-byte as found,
implementing the plan's "Aggregate updates are atomic per file, not per target".
`materialize.devin.symlink-required` is an error because the plan states there is
no real-file fallback, so refusing is the only alternative to writing an unproven
shape. `materialize.devin.hook-satisfied-by-unmanaged` and
`materialize.devin.aggregate-prune-postponed` are warnings; neither describes a
failure, only why no managed entry appeared and why a stale entry survived.

Like the `ownership.*` codes they are emitted by the consumer
(`internal/materialize`) rather than by a compiler, and they are catalogued here
because `codes.md` is the one stable vocabulary consumer surfaces key off. None
of their messages, hints, or log attributes may carry a hook payload, matcher,
timeout, MCP env value, header, OAuth secret, or aggregate file content.

### A single concrete-syntax JSON/JSONC layer for aggregate files

The new `jsonc` package parses shared harness aggregate files while preserving
comments, key order, and handwritten entry order, and patches them without
re-serializing any untouched member, so handwritten formatting inside an
unmanaged entry survives byte for byte.

It is deliberately one parse pass. A comment/order-preserving parser layered on
top of `stripJSONComments` plus `internal/jsonutil` would have been a third
pass and could let a comment hide a duplicate key, so `Parse` performs the
duplicate-key scan itself and reports the same stable coded diagnostics
(`json.duplicate-key`, `json.malformed`) the strict decoder already produced.

Trailing commas are rejected. Devin's own parser behaviour for them is
unverified, and emitting a document we cannot prove Devin reads would be worse
than refusing to touch the file.

### Devin is a full conversion target

The Devin ingester (`ingest/devin`) reads a canonical Devin root and Devin is
now registered with `Ingester: true, Conversion: true`, so `Convert`,
`IngestBundle`, and `Autodetect` all accept it and the 6x6 conversion matrix
covers it for all five resource kinds.

Ingestion reads exactly one explicit root. A workspace `.devin` directory is
autodetected; a direct user root must be selected explicitly and is never
autodetected. The ingester never discovers or merges an ancestor or nested
`.devin` root, so what is read is always the root the caller named.

Autodetection probes the `.devin` workspace marker before every other marker,
including the explicit plugin manifests: a canonical `.devin` directory can only
have been written for Devin, whereas the other markers are layouts Devin merely
reads as compatibility input. Detection reads a fixed, ordered list of literal
marker paths, so the winner never depends on directory iteration order.

`ReverseHookEvent` gained a Devin table written out explicitly rather than
derived from the forward table. The arm D3 deleted did a forward lookup with a
Devin event name in the canonical column, which only worked because every Devin
event happens to be spelled like its canonical row. `PreCompact` is deliberately
absent in both directions: it and `PostCompaction` are opposite sides of
compaction, so reversing one into the other would produce a hook that fires at
the wrong moment.

Round-trip equality is semantic, not byte-for-byte, and every irreversible case
is a declared entry in the loss matrix rather than a silent pass.

### Bundle compilation skips failing resources instead of zeroing the bundle

`compileBundleFromIR` discarded EVERY artifact in a bundle as soon as any
resource produced an error diagnostic, and `Materialize` refused such a result
outright. The promoted Devin plan states the opposite contract: "Resources with
error diagnostics are skipped, valid resources continue, and sync exits non-zero
if any error occurred." One unmappable tool restriction on one skill therefore
silently zeroed an entire Devin materialization, while `internal/materialize`
— which compiles per resource — already behaved per resource
(`agent-marketplace-5exj.17`, P0).

- **Suppression is per resource.** A resource whose compiler returns an error, or
  which emits any error diagnostic, contributes no artifacts: not its own files
  and not its fragments of a shared aggregate (`hooks.v1.json`,
  `mcp_config.json`, `plugin.json`). Every other resource in the bundle is
  published normally, so a surviving aggregate contains exactly the entries of
  the resources that compiled and each missing entry has its own error
  diagnostic naming the resource that lost it.
- **Non-attributable errors suppress at the point of emission.** Load/ingest
  failures describe a resource that never reached the compiler, so they suppress
  nothing further. `bundle.conflict.duplicate-artifact-path` and
  `bundle.conflict.cross-kind-artifact-path` suppress both named producers as
  WHOLE resources; the duplicate-path case previously kept both colliding
  artifacts (harmless only because the bundle was zeroed anyway) and the
  cross-kind case dropped only the colliding file. Fragments are dropped too,
  because a manifest that advertises an agent whose `.md` was never written is
  worse than no entry.
- **New `Result.Skipped []SkippedResource` and `Result.HasErrors()`.**
  `SkippedResource` carries kind, name, and diagnostic codes only — never a
  resource body, hook payload, MCP env value, header, OAuth secret, or permission
  value.
- **`Materialize` no longer refuses a result with error diagnostics.** It writes
  the clean resources' artifacts, sets `WriteReport.Partial` plus
  `WriteReport.SkippedResources`, and returns an error wrapping the new
  `ErrPartialResult`. Report *and* error are both populated on purpose: the write
  really happened, and a caller that ignores the error cannot mistake a partial
  run for a clean one. A partial result never prunes
  (`WriteReport.PruneSuppressed`), because a resource skipped this run is
  indistinguishable from one that was removed — the same rule
  `internal/materialize` already applies via its `blocked` handling.
- `cmd/hcc`, `cmd/agent-marketplace convert`, and the integration example now
  print what reached disk and the skipped resources, then exit non-zero.
- Structured `slog` for every skip decision (`bundle resource skipped`) with
  target, kind, resource name, codes, suppressed-artifact count, and reason.
- `hook.devin.*` and `mcp.devin.*` are now errors. This supersedes the earlier
  warning classification; `codes.go`/`codes.md` document the new severity and
  the artifact-suppression outcome.
- Golden suite: the four `testdata/expected/` trees dead since D2 became
  comparable again. Windsurf compares CLEAN and is no longer exempt. Factory,
  Cursor, and Gemini remain exempt with the exact diffs recorded in
  `golden_test.go` (leftover suppressed-subagent files in all three; one genuine
  stale byte in `expected/cursor/hooks/hooks.json`, missing `"type": "command"`)
  for bead `agent-marketplace-5exj.19`, which owns the shared fixture refresh.
  The exemption is no longer a blanket skip: those targets still assert the
  per-resource contract and skip only the byte comparison.
- New `BenchmarkCompileBundlePartialSkips` guards the partial-bundle compile
  path, and `TestMain` silences the default logger for tests and benchmarks so
  benchmark output stays parseable by `benchstat`.

### Documented root Load* APIs and self-contained registration

The README and design doc described root-package `LoadSkill`, `LoadCommand`,
`LoadSubagent`, `LoadHooks`, and `LoadMcpServers` APIs that the root package did
not expose, and the root package did not register any compiler or ingester — so
importing only the documented package could fail at runtime (the shipped
`examples/agent-marketplace-integration` program did exactly that: every
`CompileBundle` call failed with "source harness not implemented").

- The root package now exposes `LoadSkill`, `LoadCommand`, `LoadSubagent`,
  `LoadHooks`, and `LoadMcpServers` as thin facades over the `bundle` loaders,
  plus `Skill`, `Command`, `Subagent`, `Hook`, `McpServer`, and `Rule` type
  aliases so a consumer can name every returned value through the root import.
- **Registration ownership moved to the root package** (`register.go`): it
  blank-imports every built-in `compile/<target>` and `ingest/<target>`.
  Consumers need exactly one import and no blank imports; the per-target
  packages keep their `init()` self-registration so each can still be built and
  tested in isolation. The redundant blank imports were removed from `cmd/hcc`,
  the integration example, and the agent-marketplace consumers
  (`internal/catalog`, `internal/materialize`, `cmd/agent-marketplace/convert`).
- `LoadHooks` returns `([]*ir.Hook, []Diagnostic, error)` rather than the
  documented two-value form: hook flattening emits warning diagnostics for
  non-command hook types, and a two-value facade would either drop them
  (violating the warn-and-skip lossy policy) or duplicate loader logic. The
  design doc and README were corrected to the three-value contract.
- `ValidateTargetRegistrations` now also checks the reverse direction: a
  compiler or ingester registered for a target the registry does not declare (or
  that declares the capability as false) is reported as a wiring error. New
  read-only `compile.RegisteredTargets()` and `ingest.RegisteredSources()`
  accessors back the check without exposing or mutating the registries.
- New `diag.HasCode` replaces hand-rolled per-test diagnostic-code scans.
- New tests: `consumer_api_test.go` (package `harnesscompile_test`) imports only
  the root package and loads, compiles, materializes, ingests, and autodetects
  every declared target — the regression guard for the consumer contract.
  `target_registry_test.go` now drives completeness in both directions from the
  registry instead of a hardcoded list, with no global-registry mutation.

### Duplicate JSON keys are rejected with stable diagnostics

`encoding/json` accepts duplicate object keys with last-value-wins semantics,
which can silently change MCP commands, headers, env, disabled state, hooks, or
existing aggregate configuration. Every JSON loader and the merge-with-existing
path now rejects such documents before decoding them.

- New `diag.CodedError` carries a stable diagnostic code plus a `SourceRef` on
  the error return path, so consumer UIs can key off a code instead of matching
  free-text messages. `diag.AsCodedError` unwraps it; `CodedError.Diagnostic`
  converts it into a catalog-consistent `diag.Diagnostic`.
- New diagnostic codes: `json.duplicate-key`, `json.malformed`. Both are
  fail-closed errors and report the source file, the 1-based line of the
  offending key, and the JSON key path — never the duplicated value.
- New `jsonutil.Unmarshal` is the single validate-then-decode entry point for
  user- or registry-supplied JSON, replacing eight hand-written
  validate + `json.Unmarshal` pairs.
- The `.claude-plugin/plugin.json`, `.cursor-plugin/plugin.json`, and
  `.factory-plugin/plugin.json` decodes are now guarded through the shared
  `ingest.LoadJSONManifest`. The Claude manifest parse error is no longer
  swallowed: a present-but-invalid manifest fails instead of silently yielding
  an empty manifest.
- New shared `ingest.LoadFailure` replaces four near-copies of the load-failure
  diagnostic. Every ingester now emits the same shape (code, severity, resource,
  `SourceRef`), and a coded JSON failure overrides the per-harness code so
  duplicate keys are keyed identically across source harnesses.
- `artifact.MergeFragments` now classifies a fragment and validates its keys in
  a single token pass instead of a full decode plus a separate validation pass:
  -38.8% sec/op, -26.5% B/op, -68.7% allocs/op geomean on
  `BenchmarkMergeFragments`.
- `jsonutil` validation now has an explicit nesting cap (1000) instead of
  relying on `encoding/json`'s 10000-deep scanner limit.

### Devin target foundation (compile-only, opt-in)

The library now declares **five conversion targets plus Devin (compile-only,
opt-in)**. Devin has a registered compiler but no ingester, so it is excluded
from `Convert`, `IngestBundle`, and autodetection.

- New `TargetDevin` / `HarnessDevin` target with canonical roots `.devin`
  (project) and `~/.config/devin` (user).
- New `compile/devin` compiler package. Rules emit the stable
  `rule.devin.unsupported` error; all other kinds emit the transient
  `compile.devin.resource-pending` warning until the per-kind Devin compilers
  land.
- Devin cells added to the model, tool, hook-event, MCP-interpolation, and env
  token mapping tables, with per-row provenance. Only stable Devin model aliases
  are mapped; versioned model IDs are explicitly unmappable (omit + warn) rather
  than passed through. The Devin `allowed-tools` vocabulary is exactly
  `read`/`edit`/`grep`/`glob`/`exec`; everything else is unsupported.
- New diagnostic codes: `subagent.devin.tool-unknown`, `rule.devin.unsupported`,
  `compile.devin.resource-pending`, `subagent.devin.model-unmappable`,
  `subagent.devin.model-default`, `subagent.devin.effort-dropped`,
  `hook.devin.event-unmappable`, `hook.devin.unsupported-type`.
- `AllCodes()` is now derived from `codeCatalog` instead of being a third
  hand-maintained list.
- New `mapping.McpInterpolationSupported` distinguishes "unmappable" from an
  empty interpolation pattern.
- New shared `compile.DiagSource` / `compile.SourceOf` helpers replace the
  per-target source-reference conversions.

### Devin skills and converted commands

Devin now compiles the two name-sharing resource kinds natively; subagents,
hooks, and MCP servers stay on the deferred path.

- Skills compile to `skills/<name>/SKILL.md`. Commands compile to the same
  skill-directory shape with the stable `x-agent-marketplace-kind: command`
  provenance marker and user-only activation (`triggers: [user]`), so the
  conversion is semantically round-trippable by the Devin ingester.
- `allowed-tools` is strictly mapped into Devin's closed
  `read`/`edit`/`grep`/`glob`/`exec` vocabulary. Any unmappable explicitly
  allowed tool suppresses the artifact with the new `skill.devin.tool-unknown`
  error: the restriction is never narrowed, broadened, or dropped. An
  `allowed-tools` extension field on a command is mapped through the same path
  instead of being passed through verbatim. `permissions` is Devin-native and is
  emitted verbatim.

  **Superseded by bead `agent-marketplace-pu6b` (see the Unreleased entry).**
  "Never narrowed, broadened, or dropped" no longer holds for this field on this
  target: an unmappable or scoped entry now DROPS the whole `allowed-tools` key
  with a `skill.devin.tool-unknown` **warning** and still emits the artifact,
  which broadens the emitted restriction to Devin's default tool set. Strict
  mapping itself is unchanged — an unmapped identifier is still never written —
  and `permissions` is untouched by that change.
- Triggers are restricted to Devin's documented `user`/`model` vocabulary;
  anything else is dropped with the new `skill.devin.trigger-unmappable`
  warning. An explicit skill model that is not a stable Devin alias is omitted
  with the new `skill.devin.model-unmappable` warning.
- Bundle compilation detects cross-kind output-path collisions with the new
  `bundle.conflict.cross-kind-artifact-path` error, which names both resources
  and drops both colliding artifacts; the existing duplicate-path diagnostic now
  also names both producing resources.
- `compile.devin.resource-pending` is narrowed to subagents, hooks, and MCP
  servers.
- New shared `compile.Frontmatter` ordered frontmatter renderer.

### Devin flat custom subagents

Devin now compiles subagents natively to the canonical flat profile
`agents/<name>.md`; only hooks and MCP servers remain on the deferred path.
Devin also accepts `agents/<name>/AGENT.md`, but the flat file is the canonical
compiler output and the shape materialization symlinks as a file.

- `model` is mapped through the stable-alias table only. The two model losses are
  deliberately distinct codes: an explicit non-alias model is omitted with
  `subagent.devin.model-unmappable`, while an **empty** model is omitted with
  `subagent.devin.model-default`, because Devin reads absence as its default
  subagent model rather than as parent-model inheritance. Collapsing them would
  hide the case a user migrating from an inheriting harness gets wrong.
- `tools` is strictly mapped into Devin's closed
  `read`/`edit`/`grep`/`glob`/`exec` vocabulary and emitted as `allowed-tools`.
  An unmappable tool, or a scoped Claude specifier such as `Bash(git diff:*)`,
  suppresses the artifact with the `subagent.devin.tool-unknown` error: a partial
  list would narrow intended capability and omitting the restriction would
  broaden privilege. An explicit empty list stays `allowed-tools: []` rather than
  being omitted, which would restore Devin's full default tool set.

  **Superseded by bead `agent-marketplace-pu6b` (see the Unreleased entry).**
  Both halves of that reasoning survived; the conclusion did not. A partial list
  is still never emitted, and omitting the restriction *does* broaden privilege —
  but that is now the accepted outcome rather than a reason to suppress the
  profile, because Devin cannot REPRESENT this restriction at all, so no emitted
  profile can carry it and suppression buys the user nothing. Do not shorten that
  to "Devin does not enforce `allowed-tools`": the Unreleased entry records that a
  list Devin *can* represent is genuinely enforced (3000.3.27 CONTROL E).
  The code is a **warning** and the profile is emitted without the key. The
  explicit-empty behaviour in the last sentence is unchanged.
- `isolation` and `disallowedTools` suppress the artifact with the existing
  `subagent.security-restriction-unsupported` error. Devin has no field for
  either, and emitting the profile without them would publish a subagent that is
  strictly less restricted than the source asked for.
- `effort` is dropped with the `subagent.devin.effort-dropped` warning. It
  selects reasoning spend, not capability, so it cannot widen the privilege or
  activation surface.
- `max-nesting` is emitted from the typed `MaxNesting` IR field and round-trips,
  including an explicit `0` — the strictest value, which absence would relax to
  Devin's default nesting budget.
- New `subagent.devin.builtin-name-conflict` error: a subagent named
  `subagent_explore` or `subagent_general` shadows a Devin built-in that "cannot
  be replaced", so the profile would be ignored while the user believes the
  restricted one is active.
- New `subagent.devin.nested-name-unsupported` error: a name that expresses
  directory nesting is refused at the flat-layout boundary rather than flattened,
  because flattening can silently overwrite an unrelated subagent's profile.
  `ir.ValidateName` is the outer gate today; this is the layer-local invariant.
- Reserved subagent extension keys (`model`, `max-nesting`, `tools`,
  `disallowedTools`, `isolation`, `effort`) are never passed through verbatim, so
  an unverified extension value cannot raise a limit or advertise a restriction
  Devin does not enforce. `allowed-tools` is reserved only when the typed field is
  set; otherwise it is consumed and strictly mapped, so no conflict warning fires
  for a field that was honored.
- The strict `allowed-tools` resolver and the stable-alias model resolver moved
  to `compile/devin/fields.go` and are now shared by skills, commands, and
  subagents with a per-kind diagnostic code, so a rule such as "never emit an
  unmapped tool identifier" cannot drift between kinds.
- `compile.devin.resource-pending` is narrowed to hooks and MCP servers.
- `TestGolden_AllTargets` gained a named per-target source-bundle override
  (`goldenBundleFixtures`). The shared `testdata/resources` bundle's subagent
  carries `isolation` and `disallowedTools`, which are now hard Devin errors, and
  one error zeroes the bundle — so Devin needs its own bundle to keep a live
  golden tree instead of joining `staleFixtureTargets`. The new
  `testdata/resources-devin` bundle copies the shared skill, command, hook, and
  MCP inputs byte-for-byte and differs only in the subagent; the existing Devin
  golden files are unchanged.

### Devin MCP servers

Devin now compiles the last marketplace-selectable kind. **Every kind is native;
the transient `compile.devin.resource-pending` code, its catalog entry, its
`codes.md` row, and its dispatch branch are deleted.**

- MCP servers compile to an `mcp_config.json` fragment whose only top-level
  member is `mcpServers`, keyed by server name. This is the 3000.3+ dedicated
  file layout; the pre-3000.3 `config.json#mcpServers` layout is never emitted.
- The path is the same for both scopes. The evidence baseline gives
  `.devin/mcp_config.json` and `~/.config/devin/mcp_config.json` — the same
  relative path and the same envelope under two roots the caller selects — so
  unlike hooks there is deliberately no `WithScope` branch.
- Devin's two documented server objects are treated as disjoint. A `command`
  selects the stdio form (`command`, `args`, `env`, `disabled`); a `url` selects
  the remote form (`url`, `transport`, `headers`, `oauthClientId`,
  `oauthClientSecret`, `oauthResource`, `disabled`). A modeled field belonging to
  the other form is dropped with the new `mcp.devin.field-dropped` warning, which
  names the field and never its value.
- The OAuth trio is emitted from the **typed** `ir.McpServer` fields only. All
  ten documented member names are reserved extension keys unconditionally, so an
  untyped `Extra` map can never reintroduce an OAuth client secret, redirect the
  endpoint, re-enable a disabled server, or bypass the transport and
  literal-credential checks.
- Interpolation is mapped through the Devin column of `compile/mapping/mcp.go`:
  canonical inline `${VAR}` and `${env:VAR}` both become `${env:VAR}`, and the
  unmappable `${file:PATH}` style omits the value (never writes it empty) with
  `mcp.interpolation-unsupported`. `${CLAUDE_PLUGIN_ROOT}` resolves from the
  configured plugin root and is never rewritten into an env lookup when
  unresolved. The shared plugin-root expansion moved to `compile/devin/fields.go`
  so hooks and MCP servers cannot drift.
- Literal credentials in env values, header values, and the OAuth client secret
  are **preserved** and reported with the existing
  `mcp.credentials.literal-secret` warning. No `mcp.devin.*` message, hint, or
  `slog` attribute ever contains an env value, header value, OAuth client
  id/secret/resource, command, argument, or URL.
- New Devin MCP diagnostic codes. The transport diagnostics are ERRORs that
  skip exactly one entry:
  `mcp.devin.transport-ambiguous` (both a command and a url),
  `mcp.devin.transport-incomplete` (neither), `mcp.devin.transport-unsupported`
  (a transport outside the documented `http`/`sse` vocabulary — skipped rather
  than dropped, because dropping it would silently fall back to Devin's
  Streamable HTTP default), and `mcp.devin.interpolation-unrepresentable`
  (a required `command` or `url` uses an interpolation style Devin cannot
  represent). The value-level `mcp.devin.field-dropped` and
  `mcp.devin.plugin-root-unresolved` codes remain warnings, because the emitted
  entry is still usable. Skipping one bad server instead of zeroing the whole
  bundle is preserved (bead `agent-marketplace-5exj.17`).
- The compiler deliberately does **not** set
  `artifact.Artifact.DedupeFragmentEntries`: MCP identity is the server object
  key, and `artifact.mergeMCPServers` already collapses identical keyed
  duplicates and rejects conflicting ones.
- `compile/devin`'s dispatch now returns an error for an unrecognized kind
  instead of a catch-all warning, matching the sibling targets.
- The five existing targets already mapped every rich MCP field in D2; no
  sibling target output changed. `testdata/expected/devin/mcp_config.json` is the
  only new golden file.

## v0.3.0 — 2026-04-24

### N-to-N conversion

The library now supports **full N-to-N conversion** across all five supported
harnesses (Claude Code, Factory Droid, Cursor, Windsurf, Gemini CLI). Any
harness format can be used as both source and target.

- `Convert(srcFS, from, to)` now works for all 5 registered source harnesses,
  not just Claude. Previously, non-Claude sources returned `errSourceNotImplemented`.
- `IngestBundle(srcFS, from)` now works for all 5 registered ingesters.
- New `compileBundleFromIR()` helper compiles an `ingest.Bundle` to any target.
- Backward-compatible: `CompileBundle(src, target)` delegates to `Convert(src, Claude, target)`.

### CLI: `hcc convert` subcommand

- New `hcc convert --from <src> --to <dst> <dir>` subcommand for one-shot
  harness conversion from the command line.
- Supports all 5 harness names: `claude`, `factory`, `cursor`, `windsurf`, `gemini`.
- `--autodetect` flag to auto-detect source harness instead of specifying `--from`.
- `--out <dir>` for output directory (defaults to `<dir>-<target>`).
- Identity conversion (`--from == --to`) is rejected with an error message.
- Existing top-level flags are now under `hcc compile` subcommand.

### Ingest layer (Phase 3)

- Factory ingester (`ingest/factory/`): reads `.factory-plugin/` and `.factory/` layouts.
- Cursor ingester (`ingest/cursor/`): reads `.cursor-plugin/` and `.cursor/` layouts.
- Claude ingester (`ingest/claude/`): wraps existing `bundle/` loaders.
- Gemini ingester (`ingest/gemini/`): reads `gemini-extension.json`, `.gemini/`, TOML commands, settings.json hooks.
- Windsurf ingester (`ingest/windsurf/`): reads `.windsurf/`, workflows as commands, split-per-tool hook events.

### Superset IR with `ext.*` sidecars

- All IR resource types now carry `Ext ext.Bag` for typed per-harness sidecar data.
- `Ext.Factory`, `Ext.Cursor`, `Ext.Windsurf`, `Ext.Gemini`, `Ext.Claude` sidecars
  preserve harness-specific fields for lossless identity round-trips.
- Emitter precedence: use `Ext.<target>` when non-nil, fall back to canonical fields.

### Diagnostic codes

- New `ingest.*` code family for ingestion-side diagnostics (info + warning + error).
- 11 info codes, 3 warning codes, 9 error codes added for the ingest layer.
- `diag/codes.md` now includes examples for key diagnostic codes.

### Design docs

- N-to-N design: `docs/plans/2026-04-24-harness-compilation-n-to-n-design.md` (Accepted).
- Original design status updated to "Superseded by N-to-N Design".

---

## v0.2.0 — 2026-04-23

### Initial 1-to-N compilation

- Claude → 5 targets (Claude, Factory, Cursor, Windsurf, Gemini) compilation.
- `Compile()`, `CompileBundle()`, `Materialize()` public API.
- Per-resource compilation with structured diagnostics.
- Golden-file tested, deterministic output.
