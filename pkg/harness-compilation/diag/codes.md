# Diagnostic Code Catalog

Every diagnostic code emitted by the harness-compilation library is listed here.
The test `TestDiagnosticCodes_Documented` enforces that each constant in `codes.go`
has a corresponding entry in this file.

## Model selection

| Code | Meaning | Remediation |
|---|---|---|
| `model.unresolved` | Model omitted without suppressing the resource; target defaults apply | Update the shipped harness catalog or provide an explicit model override |
| `model.reasoning-omitted` | Requested reasoning cannot be represented for the selected target/model | Choose a supported model and reasoning combination |
| `model.inheritance-mismatch` | Source inheritance/default semantics differ from target omission | Specify a supported explicit model |
| `model.reasoning-conflict` | Explicit reasoning overrides a conflicting model-encoded preference | Specify the reasoning preference once |
| `model.selector-pinned` | A floating family selector was pinned by target catalog policy (info) | Use an exact model when version identity matters |
| `model.provenance-conflict` | Conflicting legacy native sidecars were ignored in favor of canonical fields | Supply model source provenance and authoritative fields |

Model and reasoning conversion is resolved jointly from shipped catalogs. These losses are warnings, except informational family pinning; none suppresses the resource. Existing target-specific Devin and native-Codex loss codes remain available for equivalent losses. Same-target native preservation does not certify current account availability.

## Severity ordinals count DOWN, so never compare them

`SeverityError` is the **zero value** of `Severity`: error 0, warning 1, info 2.
The ordinals are wire-visible and this catalog is what consumer UIs key off, so
they will not be renumbered. That makes a magnitude comparison against
`SeverityError` a guaranteed logic error in either direction:

```go
d.Severity >= diag.SeverityError // TRUE for every severity
d.Severity <  diag.SeverityError // TRUE for no severity
```

Ask the named predicates instead — they are the only place the direction is
written:

```go
d.IsError()                             // this diagnostic is a fail-closed error
d.Severity.AtLeastAsSevereAs(threshold) // at least as grave as threshold
diag.HasErrors(diagnostics)             // any fail-closed error in a slice
```

`TestNoRawSeverityErrorComparisonsInProductionCode` fails on a reintroduced raw
comparison in non-test code.

## Info

| Code | Summary | Hint |
|------|---------|------|
| `ingest.codex.bundle-manifest-synthesized` | Codex workspace has no bundle manifest; manifest synthesized | Codex bundle metadata is synthesized from the native layout |
| `subagent.cursor.model-passthrough` | Cursor model not in mapping table; passed through unchanged | Add the model to the mapping table if a Cursor-specific ID exists |
| `ingest.claude.bundle-manifest-synthesized` | Workspace form has no plugin.json; manifest synthesized | Add a .claude-plugin/plugin.json to the source if a manifest is needed |
| `ingest.factory.bundle-manifest-synthesized` | Workspace form has no plugin.json; manifest synthesized | Add a .factory-plugin/plugin.json to the source if a manifest is needed |
| `ingest.factory.reasoning-effort-mapped-to-model` | Factory reasoningEffort preserved while model was canonicalized | Compile back to Factory to restore the concrete Factory model identifier |
| `ingest.cursor.bundle-manifest-synthesized` | Workspace form has no plugin.json; manifest synthesized | Add a .cursor-plugin/plugin.json to the source if a manifest is needed |
| `ingest.gemini.bundle-manifest-synthesized` | Workspace form has no gemini-extension.json; manifest synthesized | Add gemini-extension.json to the source if bundle metadata is needed |
| `ingest.gemini.toml-command-canonicalized` | Gemini TOML command metadata was normalized into canonical IR fields and extras | Only name, description, and prompt map directly into canonical command fields in v0 |
| `ingest.windsurf.bundle-manifest-synthesized` | Windsurf has no plugin manifest; manifest synthesized | Windsurf bundles are synthesized from the workspace layout during ingestion |
| `ingest.windsurf.workflow-assumed-command` | Windsurf workflow was ingested as a command | Workflows map to ir.Command because Windsurf already has a native skill primitive |
| `ingest.windsurf.literal-path-preserved` | Literal Windsurf path preserved during MCP ingestion | Literal path values remain unchanged because there is no Claude-side canonical token for them |
| `ingest.windsurf.mcp-env-normalized` | Windsurf MCP env interpolation normalized to canonical form | Windsurf ${env:VAR} was converted to canonical ${VAR} during ingestion so the IR is harness-neutral |
| `ingest.windsurf.subagent-via-agents-md-skipped` | AGENTS.md was not synthesized into a Windsurf subagent | Windsurf has no subagent primitive; AGENTS.md remains external project guidance |
| `ingest.devin.bundle-manifest-synthesized` | Devin has no bundle manifest file; manifest synthesized | Devin roots carry no plugin manifest; bundle metadata is synthesized from the layout during ingestion |
| `ingest.devin.local-override-ignored` | A Devin project-local personal override file was found and not ingested | Agent Marketplace neither reads nor writes .devin/config.local.json or .devin/mcp_config.local.json; move shared configuration into the non-local file |
| `ingest.devin.mcp-env-normalized` | Devin MCP env interpolation normalized to canonical form | Devin ${env:VAR} was converted to canonical ${VAR} during ingestion so the IR is harness-neutral; compiling back to Devin restores ${env:VAR} |
| `doctor.devin.recommended` | A Devin CLI is installed but no harness list selects the devin target | Select devin in the TUI harness modal (H) or add it to a harnesses list |
| `ownership.lock-contended` | Another agent-marketplace process held this target root's ownership lock, so this run waited for it | No action needed; ownership mutations are serialized per canonical target root so two writers cannot publish an aggregate file and its ownership record at different instants |

## Warning

| Code | Summary | Hint |
|------|---------|------|
| `ingest.claude.unconsumed-config` | A recognized MCP or hook configuration file was not read by the selected Claude layout | Follow the file-specific hint to select the matching source layout or merge definitions into the selected layout without overwriting entries. |
| `ingest.claude.duplicate-root-skill` | A plugin-form bundle's root SKILL.md declared a name already loaded from skills/; the skills/ entry is kept | Rename the root skill or remove the duplicate; skills/ entries load first and win deterministically |
| `ingest.factory.unconsumed-config` | A recognized MCP or hook configuration file was not read by the selected Factory layout | Follow the file-specific hint to select the matching source layout or merge definitions into the selected layout without overwriting entries. |
| `ingest.cursor.unconsumed-config` | A recognized MCP or hook configuration file was not read by the selected Cursor layout | Follow the file-specific hint to select the matching source layout or merge definitions into the selected layout without overwriting entries. |
| `ingest.gemini.unconsumed-config` | A recognized MCP or hook configuration file was not read by the selected Gemini layout | Follow the file-specific hint to select the matching source layout or merge definitions into the selected layout without overwriting entries. |
| `ingest.windsurf.unconsumed-config` | A recognized MCP or hook configuration file was not read by the selected Windsurf layout | Follow the file-specific hint to select the matching source layout or merge definitions into the selected layout without overwriting entries. |
| `ingest.devin.unconsumed-config` | A recognized MCP or hook configuration file was not read by the selected Devin layout | Follow the file-specific hint to select the matching source layout or merge definitions into the selected layout without overwriting entries. |
| `ingest.codex.unconsumed-config` | A recognized MCP or hook configuration file was not read by the selected Codex layout | Follow the file-specific hint to select the matching source layout or merge definitions into the selected layout without overwriting entries. |
| `ingest.factory.resource-shadowed` | Factory resource was shadowed by an earlier resource of the same kind and name | Use distinct resource names or remove the lower-priority definition |
| `subagent.windsurf.no-primitive` | Windsurf has no subagent primitive; resource dropped | Consider expressing the subagent as a Windsurf workflow instead |
| `hook.claude.unsupported-type` | Unsupported hook type in Claude format; only command-type is universally supported in v0 | Convert the hook to a command type or remove it |
| `hook.windsurf.env-token-expanded` | Windsurf expanded env token to a literal path | Verify the expanded path is correct for the target environment |
| `ingest.gemini.hook-event-partial-mapping` | Gemini hook event has no exact Claude equivalent; original event preserved | Use BeforeTool, AfterTool, or PreCompress for lossless cross-harness mapping |
| `ingest.windsurf.hook-event-unmappable` | Windsurf hook event has no Claude equivalent; original event preserved | Use a mappable Windsurf event or preserve the native event through the Windsurf sidecar |
| `mcp.interpolation-unsupported` | One MCP value uses an interpolation style the target cannot represent; that value was omitted and the entry was still emitted | Replace the interpolated value with a literal value, or use an interpolation style the target documents |
| `rule.claude.no-primitive` | Claude Code has no rules directory primitive; rule dropped | Claude reads project guidance from CLAUDE.md; reference the rule content there instead |
| `rule.gemini.no-primitive` | Gemini CLI has no rules directory primitive; rule dropped | Gemini reads project guidance from GEMINI.md; reference the rule content there instead |
| `rule.devin.unsupported` | Rules are out of scope for the Devin target; no rule artifact was emitted | This is a scope decision, not a Devin limitation: on 3000.3.27 a rule placed at `.devin/rules/<name>.md` is discovered even though `devin rules paths` does not advertise it. Compile the rule to a target with a supported rules primitive (factory, cursor, windsurf), or place it under `.devin/rules` by hand |
| `rule.codex.unsupported` | Rules are out of scope for the Codex target; no rule or AGENTS.md content was emitted | Use a supported resource kind or manage AGENTS.md separately |
| `compile.codex.field-dropped` | A benign IR field has no documented Codex equivalent and was omitted | Remove the field or choose a target that represents it |
| `subagent.codex.model-dropped` | A Codex-only subagent model was omitted for a non-Codex target | Compile to Codex to preserve the native-only model, or choose a portable canonical model |
| `subagent.codex.effort-dropped` | A Codex-only subagent reasoning effort was omitted for a non-Codex target | Compile to Codex to preserve the native-only effort, or use a portable canonical effort |
| `hook.codex.plugin-root-unresolved` | The hook command references the plugin root but no plugin root was configured; the token was left unexpanded | Compile with a plugin root so the command resolves, or use an absolute command path |
| `mcp.codex.plugin-root-unresolved` | An MCP value references the plugin root but no plugin root was configured; the token was left unexpanded | Compile with a plugin root so the value resolves, or use an absolute path |
| `ingest.codex.prompt-deprecated` | A deprecated Codex prompt was ingested as a command | Move the prompt to a command skill under `.agents/skills` |
| `ingest.codex.field-dropped` | A benign native Codex field has no canonical equivalent and was omitted | Remove the field or keep the resource in Codex format |
| `ingest.codex.hook-event-unmappable` | Codex hook event is outside the documented Codex vocabulary; the original event name was preserved | Use a documented Codex event (PreToolUse, PostToolUse, UserPromptSubmit, SessionStart, SessionEnd); an undocumented event cannot be translated |
| `rule.factory.metadata-dropped` | Factory rules are plain markdown; description/globs/alwaysApply metadata dropped | Factory does not support rule frontmatter (description or conditional glob-scoped application); the rule body is emitted verbatim |
| `compile.canonical-field-overrides-extra` | Canonical field overrides a conflicting preserved extra | The canonical modeled value wins; rename the extension field to preserve both values |
| `hook.timeout-rounded` | Timeout unit conversion to seconds rounded up to the next whole second | Use a whole-second timeout or accept the rounded value |
| `hook.timeout-capped` | Timeout exceeded the documented per-event limit and was lowered to the limit | Set a timeout within the documented limit |
| `hook.duplicate-execution-risk` | Two selected harness targets load the same hook files by default, so the hook may run twice | Deselect one of the targets for this scope, or accept the duplicate execution |
| `materialize.hook.review` | An owned hook command or prompt was added, changed, or removed in either scope, and the exact values were printed to stdout for review | Review the printed hook changes before relying on them |
| `materialize.hook.review-pending` | A check found a pending hook change without mutating files | A later sync will apply the pending hook change |
| `materialize.hook.satisfied-by-unmanaged` | On a target without a target-specific code, a semantically identical handwritten hook already exists; no entry was added | No action needed; the handwritten entry satisfies the desired hook and is not adopted |
| `skill.support-files-deferred` | Skill support file names were preserved but bytes were not materialized | Materialize source-relative support files in the owning layer |
| `mcp.credentials.literal-secret` | MCP configuration contains a literal credential-like value | Use environment interpolation or an external secret reference |
| `skill.devin.model-unmappable` | Explicit skill model has no resolved Devin representation; model omitted | Update the shipped Devin model catalog or use an explicit model override |
| `skill.devin.trigger-unmappable` | Trigger is outside Devin's user/model activation vocabulary; the trigger was dropped | Use the user or model trigger, or select a target that represents this trigger |
| `subagent.devin.model-unmappable` | Explicit model has no resolved Devin representation; model omitted | Update the shipped Devin model catalog or use an explicit model override |
| `subagent.devin.model-default` | Parent inheritance becomes Devin's default subagent model | Choose a supported explicit model when inheritance cannot be represented |
| `subagent.devin.effort-dropped` | Requested reasoning has no supported Devin encoding and was omitted | Use a cataloged model and reasoning combination |
| `skill.devin.tool-unknown` | An explicitly allowed skill or command tool has no Devin equivalent; the `allowed-tools` key was omitted and the resource was still emitted | Use one of Devin's allowed tools (read, edit, grep, glob, exec) to keep the restriction, or accept Devin's default tool set for this resource |
| `subagent.devin.tool-unknown` | An explicitly allowed subagent tool has no Devin equivalent; the `allowed-tools` key was omitted and the profile was still emitted | Use one of Devin's allowed tools (read, edit, grep, glob, exec) to keep the restriction, or accept Devin's default tool set for this subagent |
| `hook.devin.payload-dropped` | The hook carries the payload its type does not select; that payload was dropped | Split the hook into one command hook and one prompt hook if both payloads are required |
| `hook.devin.plugin-root-unresolved` | The hook command references the plugin root but no plugin root was configured; the token was left unexpanded | Compile with a plugin root so the command resolves, or use an absolute command path |
| `mcp.devin.field-dropped` | A modeled MCP field belongs to the documented server form this server did not select and was dropped | Move the field to a server of the form that documents it |
| `mcp.devin.plugin-root-unresolved` | An MCP value references the plugin root but no plugin root was configured; the token was left unexpanded | Compile with a plugin root so the value resolves, or use an absolute path |
| `ingest.devin.hook-event-unmappable` | Devin hook event is outside the documented Devin vocabulary; the original event name was preserved | Use a documented Devin event (PreToolUse, PostToolUse, PermissionRequest, UserPromptSubmit, Stop, SessionStart, SessionEnd, PostCompaction); an undocumented event cannot round-trip |
| `ingest.devin.model-not-canonical` | Devin model alias has no canonical equivalent; the value was preserved verbatim | Use sonnet, opus, or haiku so the model round-trips; other Devin aliases are dropped when the resource is compiled again |
| `ingest.devin.command-field-dropped` | A command-provenance Devin skill carried a field the canonical command IR cannot hold; the field was dropped | Remove the field, or remove x-agent-marketplace-kind: command so the resource is ingested as a skill |
| `ingest.devin.reserved-field-dropped` | A frontmatter field Devin does not read was dropped instead of being preserved as an extra | Devin subagents declare tool restrictions with allowed-tools; the Claude-style tools key is neither read by Devin nor re-emitted |
| `ingest.devin.subagent-directory-form-skipped` | A Devin subagent in the agents/&lt;name&gt;/AGENT.md directory form was not ingested | Canonical Devin output is the flat agents/&lt;name&gt;.md form; convert the profile to a flat file so it is ingested |
| `ingest.devin.legacy-mcp-location-ignored` | A pre-3000.3 config.json#mcpServers block was found and deliberately not read | Devin 3000.3 migrated MCP configuration into mcp_config.json; move the servers there so they are ingested |
| `ownership.state-missing` | No durable ownership record for this target root; every existing aggregate entry is treated as unmanaged | Nothing is pruned or replaced without proof of ownership; run doctor --repair to adopt exact matches |
| `ownership.entry-drift` | A proven owned aggregate entry was edited by hand and was replaced from marketplace configuration | Edit the marketplace resource instead: an owned entry is regenerated on every sync |
| `ownership.unmanaged-key` | A desired aggregate key already exists and is not proven owned; the existing entry was preserved and only the generated entry failed | Run doctor --repair: it adopts the existing entry when it matches exactly, and offers to replace it when the value differs; renaming the marketplace resource also resolves it |
| `ownership.recovered` | An incomplete journaled generation was recovered on startup | No action needed; the recorded journal phase decided whether the generation rolled forward or back |
| `ownership.lock-unavailable` | This filesystem or platform does not support advisory locking, so ownership mutations for this target root are not serialized against other processes | Concurrent writers on this root can still race; move XDG_STATE_HOME to a local filesystem if two agent-marketplace processes ever run at once |
| `materialize.devin.hook-satisfied-by-unmanaged` | A handwritten Devin hook is semantically identical to a selected marketplace hook, so no generated entry was added | No action needed; the existing handwritten entry already provides the behaviour and is neither adopted nor duplicated |
| `materialize.devin.aggregate-prune-postponed` | A resource was skipped this run, so stale owned aggregate entries were not pruned | Fix the reported resource errors and re-run sync; a skipped resource is indistinguishable from a removed one, so pruning would delete valid output |
| `materialize.codex.hook-satisfied-by-unmanaged` | A handwritten Codex hook is semantically identical to a selected marketplace hook, so no generated entry was added | No action needed; the existing handwritten entry already provides the behaviour and is neither adopted nor duplicated |
| `materialize.codex.aggregate-prune-postponed` | A Codex resource was skipped this run, so stale owned aggregate entries were not pruned | Fix the reported resource errors and re-run sync; a skipped resource is indistinguishable from a removed one, so pruning would delete valid output |
| `materialize.codex.aggregate-malformed` | A Codex aggregate could not be parsed or safely reconciled, so that file was left exactly as found | Fix the reported hooks.json or config.toml error by hand; the malformed file is preserved |
| `materialize.codex.aggregate-unpatchable` | An existing Codex MCP server uses an alternate TOML form that cannot be safely patched, so that entry was preserved | Rewrite the server as a standard [mcp_servers.&lt;name&gt;] table or edit it by hand before syncing |
| `materialize.destination.overlap` | Resolved Codex destinations overlap in a way that cannot be safely coalesced, so materialization was refused | Use compatible repository and machine modes, resolve conflicting resources, or separate the repository and CODEX_HOME roots |
| `materialize.codex.trust-review` | Repository Codex hooks or MCP configuration changed and may require project trust review | Review or re-approve the repository in Codex before using the changed configuration |
| `materialize.codex.trust-review-pending` | `sync --check` found a pending repository Codex hooks or MCP change without mutating files or trust state | A later sync may require reviewing or re-approving the repository in Codex |
| `doctor.devin.version-unsupported` | The installed Devin CLI is older than the 3000.3 compatibility floor | Sync does not require a Devin binary and is not blocked; upgrade the Devin CLI to 3000.3 or newer before relying on the generated output |
| `doctor.devin.version-unknown` | A Devin CLI is installed but its version could not be established | The version probe failed, timed out, or printed no recognizable version; nothing is blocked, but 3000.3 compatibility could not be confirmed |
| `doctor.repair.conflict-replaceable` | An existing unmanaged aggregate entry has the same key as a desired entry but a different value, so it is neither an exact adoption match nor an evidence-backed orphan | Run doctor --repair on a terminal to confirm replacing it with the generated entry; the existing entry is preserved until you confirm, a fresh safety backup is taken first, and --yes never replaces it |

### Hook review output

`materialize.hook.review` is a warning about a completed publication in either
repository or user scope. The separate stdout review lists the changed entry's
target, scope, file, event, matcher, and exact old and new command or prompt.
Metadata-only updates and no-op syncs emit no listing or review diagnostic.
`materialize.hook.review-pending` is the check-mode counterpart: it reports the
same pending listing while writing no native file, configuration, ownership
state, or backup. Review output is not written to application logs or
telemetry; caller redirection that captures stdout is outside those value-free
channels.

### Unconsumed configuration audits

Unconsumed-configuration diagnostics use this fixed, ordered inventory of recognized MCP and hook paths: root `.mcp.json`, `mcp.json`, and `hooks/hooks.json`; Claude `.claude/.mcp.json`, `.claude.json`, `.claude/hooks/hooks.json`, and `.claude/settings.json`; Factory `.factory/hooks.json`; Cursor `.cursor/hooks.json`; the shared `.devin/hooks.json`; Factory `.factory/mcp.json` and `.factory/hooks/hooks.json`; Cursor `.cursor/mcp.json` and `.cursor/hooks/hooks.json`; Gemini `gemini-extension.json`, `.gemini/mcp.json`, `.gemini/settings.json`, and `.gemini/gemini-extension.json`; Windsurf `mcp_config.json`, `.windsurf/mcp_config.json`, `hooks.json`, and `.windsurf/hooks.json`; Devin `hooks.v1.json`, `.devin/hooks.v1.json`, `.devin/config.json`, and `.devin/mcp_config.json`; and Codex `.codex/hooks.json` and `.codex/config.toml`. The audit is file-level rather than member-level: a recognized file is considered consumed when its selected loader reads it, regardless of which entries it contains. Empty valid aggregates and files that were read but failed validation are therefore not unconsumed; empty bytes that fail parsing remain load failures. Generic root `config.json`, arbitrary JSON/support files, and personal override files governed by dedicated diagnostics are excluded. The audit never reads configuration contents and never moves files automatically; use the actionable diagnostic hint to select the matching source layout or merge definitions without overwriting existing entries. Audit warnings are non-owning traversal diagnostics: diagnostic-only containers retain their warnings while nested resources are discovered, whereas actual resource and load diagnostics still bound traversal.

### Codex diagnostics are evidence-bound and fail closed on authority loss

The Codex catalog entries are tied to the native schemas verified on 2026-08-19:
[skills](https://developers.openai.com/codex/skills),
[subagents](https://developers.openai.com/codex/subagents),
[hooks](https://developers.openai.com/codex/hooks), and
[MCP](https://developers.openai.com/codex/mcp), with `oauth_resource` verified
in the [config reference](https://developers.openai.com/codex/config-file/config-reference).
Their fixture coverage lives in
`compile/codex/translator_test.go`, `ingest/codex/codex_test.go`, and the Codex
cells of `testdata/matrix`.

`compile.codex.field-dropped` and `ingest.codex.field-dropped` are warnings only
for benign metadata loss. Codex-specific `*unsupported` errors and the shared
`subagent.security-restriction-unsupported` error suppress the affected skill,
subagent, hook entry, or MCP server when translation would lose a tool/sandbox
restriction, executable behavior, authentication, transport, or interpolation
semantics. They never guess an approximation and never suppress an independent
valid resource.

`materialize.codex.*` codes belong to the consumer-owned reconciliation layer:
they preserve malformed or unpatchable shared files, refuse duplicate desired
identities, and report repository trust review without changing Codex trust
state. Check-only drift uses `materialize.codex.trust-review-pending`; only an
actual repo hooks/MCP mutation uses `materialize.codex.trust-review`.

### `rule.devin.unsupported` is a scope decision, not a capability limit

`rule.claude.no-primitive` and `rule.gemini.no-primitive` say what their names
say: those harnesses have no rules directory at all, so a rule can only be
dropped. `rule.devin.unsupported` shares their severity and their outcome but
**not** their reason, and the difference is deliberate.

The claim this code used to carry — "Devin has no rules directory primitive" —
is false on Devin 3000.3.27. The D12 runtime probe
(`internal/devinprobe.TestProbeRulesDiscoveryUnderDevinRoots`, reproduced
independently of the suite) observed both halves of the situation:

- `devin rules paths` advertises only `.windsurf/rules/*.md (always-on)` and
  `.cursor/rules/*.md (conditional)`. It does **not** mention `.devin/rules`.
- A file written to `.devin/rules/<name>.md` **is** discovered: `devin rules
  list` reports it as `<name> [Devin] manual`.

Emitting nothing is still correct, for two reasons that no runtime observation
can falsify. First, rules are not marketplace-selectable at all
(`internal/config.AllKinds` has no rule kind) and "marketplace-selectable rules"
is an explicit non-goal of the promoted Devin plan; that non-goal is about the
selection model, not about Devin. Second, `.devin/rules` is undocumented and
unadvertised, and this target's standing rule is to refuse dependence on
undocumented Devin behaviour — the same reason `mcp.devin.transport-unsupported`
skips an undocumented transport instead of accepting Devin's silent default, and
the same reason `ingest.devin.tool-unreversible` refuses an unenforced tool
restriction.

Severity is a **warning**, matching the Claude and Gemini siblings, because the
outcome is the same class of loss: one rule is dropped, nothing unsafe is
written, no aggregate entry is at risk, and every other resource compiles. It
does *not* belong to the Devin error family below, every member of which marks
output that would be actively wrong if emitted (a hook that runs something else,
a server contacted without authorization, a restriction advertised through a
field Devin does not have, a silent transport fallback, a shadowed built-in).
"A broadened privilege" used to be the first item in that list; since bead
`agent-marketplace-pu6b` it is not, because the two Devin `allowed-tools` codes
broaden the emitted restriction and are warnings (see their section below).
Failing a whole `sync` or `convert` over a scope decision would also overstate
the situation, given that a user can place the same file by hand and Devin will
read it. If Devin ever starts advertising a `.devin` rules path, the probe fails
loudly and this decision must be revisited as rule support.

### `skill.devin.tool-unknown` / `subagent.devin.tool-unknown` omit the key instead of suppressing the resource

**These two codes were errors that suppressed the artifact. Bead
`agent-marketplace-pu6b` reversed that, deliberately.** The previous contract is
not restated here as if it were still true, and it is not deleted either: what
follows is the record of why it changed, so a future reader does not "fix" this
back.

**What they mean now.** An `allowed-tools` entry Devin cannot express — a scoped
Claude specifier such as `Bash(openspec:*)`, a name outside the closed
`read`/`edit`/`grep`/`glob`/`exec` vocabulary such as `Write`, `WebFetch`,
`WebSearch`, or `mcp__*`, or an `allowed-tools` value that is not a list shape at
all — causes the **`allowed-tools` key to be omitted** from an artifact that is
**still emitted**. One warning is reported per offending entry, naming the
resource. `sync` exits zero and the resource materializes.

All three failure modes behind these codes take that outcome. There is no
half-mapped middle: if any entry fails, the whole key is omitted rather than the
mappable subset being emitted, because a partial list is narrower than the source
asked for and can leave the resource unable to do its job.

**Two behaviours are unchanged.** An explicitly empty restriction still renders
`allowed-tools: []`, and a fully mappable restriction still renders the mapped
list. `compile.WithToolOverride` still maps names the table does not know.

**The empty-vs-absent boundary is decided by content, not by YAML shape.**
Because "key omitted" now means "Devin's default tool set" rather than "not
installed", the difference between an explicitly empty restriction and an
unrepresentable one is a privilege difference, and an adversarial review of this
bead found it turning on the shape the author happened to write. Every accepted
shape is therefore normalized identically before anything is classified: each
entry is split on commas, trimmed, and dropped when nothing is left. So
`allowed-tools: ""`, `"  "`, `","`, `[]`, `[""]`, `["", " "]` and `[","]` all
mean "no tools" and all render `allowed-tools: []`, and `" Read "` and
`[" Read "]` both render `- read`. Previously only the string spellings did:
`[""]` had one blank entry that matched no tool, so it omitted the key and
inherited every default tool, and `[" Read "]` did the same to a read-only
restriction. Normalization only ever removes blanks, and never invents a name the
source did not write, so it cannot broaden a restriction. A `Tool(a, b)` fragment
still carries a parenthesis after splitting and is still classified as a scoped
specifier that drops the key whole.

**An explicit `compile.WithToolOverride` entry outranks the scoped-specifier
rule.** An override keyed on the exact entry — including `Bash(openspec:*)` — is
the consumer stating per invocation which Devin tool that entry means, so it is
consulted before the parenthesis check. Ignoring it would discard a restriction
the caller asked for in favour of Devin's default tool set, which is strictly
wider. Without such an override, a scoped specifier still drops the key.

**A resource suppressed for another reason does not claim Devin's defaults
apply.** A subagent that also fails on `isolation`, `disallowedTools`, or a
built-in name conflict still emits no artifact, and its `allowed-tools` warning
still rides along so one compile shows every finding — but its wording says the
profile was not written, because there is no installed artifact for a default
tool set to apply to. The code and severity are identical either way.

**Why.** Scoped `Bash(...)` is what the openspec skill generator emits, so three
skills selected by `agent-marketplace`'s own committed `.agents/config.yaml`
produced three errors, a non-zero `sync`, and nothing installed — and the
non-zero exit additionally gated off unrelated `--prune-stale` cleanup. The
user's only available fix was editing a registry they may not own.

Omitting the key inherits Devin's default tool set. **That is a real widening
relative to the source's stated restriction, and it is the deliberate point of
the decision**, not an oversight.

The supporting evidence is narrower than "Devin ignores `allowed-tools`", and
this section deliberately does not overstate it. Devin **does** enforce a list it
can represent: CONTROL E on 3000.3.27, recorded in
`docs/devin-cli-harness-spec-3000.3.md`, verified *on disk* that a profile
compiled with `allowed-tools: [read, grep]` could neither overwrite a file nor
run a shell command. What Devin cannot do is represent the restriction these
codes are about — it has five bare tool names, no per-command scoping, and the
D12 probe (`internal/devinprobe.TestProbeAllowedToolVocabulary`, a 22-name
candidate superset) found every other name is *silently dropped* rather than
rejected.

So the choice is never "enforced versus unenforced". Every available output is an
unfaithful representation of the source's restriction, and the decision is which
one to write: no resource at all, a restriction narrower than the source asked
for, or Devin's default set stated honestly by the absence of the key. The same
CONTROL E also found that even a representable list bounds the profile's **own**
toolset and not what it can reach through nesting, so `allowed-tools` is not a
hard privilege boundary on Devin in any case.

**Two alternatives were rejected.** Keeping the error leaves the resource simply
unavailable with no fix in the user's hands. Emitting `allowed-tools: []` is the
narrowest representable value and never broadens — the `mapTriggers` move — but
the analogy breaks: `triggers: []` only stops auto-activation and leaves the
skill usable, whereas `allowed-tools: []` means the resource may use **no** tools
at all, and `openspec-apply-change` needs `exec` to run `openspec`. That trades a
loud absence for a silent, inert install, which is worse.

**Factory fails closed for `subagent.factory.tool-unknown`.** An unmappable
explicit tool suppresses the droid with an error instead of inheriting Factory's
default tool set. Valid native `Create` restrictions and explicit empty lists
remain representable. `disallowedTools` and `isolation` also remain fail-closed.
The Devin-specific omission policy above is unchanged.

**The ingest direction is deliberately not reversed.**
`ingest.devin.tool-unreversible` stays an error that skips the resource, because
ingestion writes the harness-neutral IR that all seven compilers read, so a
rewritten restriction there propagates to targets that do enforce it. Compiling
to Devin writes only Devin's own artifact. Same field, different blast radius.

**No diagnostic, hint, or log attribute quotes the offending specifier**, and
that property is unchanged and tested: `allowed-tools` entries carry command
strings such as `Bash(git push --force:*)`. The resource name and source location
localize the loss instead. The structured log adds `unmappable_count`,
`scoped_count`, and `outcome=allowed-tools key omitted` — counts, not values.

Every `ingest.devin.*` warning describes something a canonical Devin root expressed that canonical IR cannot hold. Dropping it can only *narrow* what a later compile emits, so none of them fails the resource. The one ingest situation that can change a privilege decision is `ingest.devin.tool-unreversible`, which is an error and skips the resource. No `ingest.devin.*` message, hint, or log attribute ever contains a command string, prompt body, matcher value, timeout value, env value, header value, OAuth client id/secret/resource, permission value, or resource body — only field names, the fixed Devin event/tool vocabulary, the resource name, and the source location.

The `hook.devin.*` and `mcp.devin.*` families are split by OUTCOME, not by kind.

Read that split as being about the outcome alone. It was once also readable as a
general severity rule — "a loss that broadens privilege is an error; a loss that
narrows or is inert is a warning" — and bead `agent-marketplace-pu6b` made that
reading false: the two Devin `allowed-tools` codes broaden the emitted restriction
to Devin's default tool set and are warnings. The rule that holds across the whole
Devin family is narrower: **a loss is an error when the emitted output would
misrepresent the target's own behaviour**, and a warning when the emitted output
is a truthful, if lossy, statement about what the target will do — because
suppressing the resource then costs the user the resource without buying them a
correct one. Do not read the second clause as "widening is fine": it applies to a
field whose ABSENCE has a documented meaning on the target, which is why an
omitted `allowed-tools` key qualifies and an omitted `isolation` mode does not.

A code that REMOVES the desired aggregate entry is an **error**: `hook.devin.event-unmappable`, `hook.devin.unsupported-type`, `hook.devin.matcher-unsupported-event`, `hook.devin.payload-missing`, `hook.devin.timeout-invalid`, `mcp.devin.transport-ambiguous`, `mcp.devin.transport-incomplete`, `mcp.devin.transport-unsupported`, and `mcp.devin.interpolation-unrepresentable`. This is both halves of the plan's sentence: "invalid or conflicting desired entries are skipped while other valid entries are patched atomically; sync still exits non-zero." Skipping keeps the run fail-closed — no hook entry means Devin never runs something other than what the source asked for, and no server entry means Devin never launches a process or contacts an endpoint the source did not authorize — and the error severity is what makes `sync` report the loss with a non-zero exit instead of a warning an operator can miss.

A code that still EMITS the entry stays a **warning**: `hook.devin.payload-dropped` and `mcp.devin.field-dropped` drop an inert sub-field the selected hook type or server form would never have used, and `hook.devin.plugin-root-unresolved` / `mcp.devin.plugin-root-unresolved` leave a `${CLAUDE_PLUGIN_ROOT}` token unexpanded in a value that is otherwise emitted verbatim. Escalating those four would suppress an entry that is otherwise exactly what the source authorized, which contradicts the per-resource suppression contract below.

The escalation landed in bead `agent-marketplace-5exj.20`, after two prerequisites. `agent-marketplace-5exj.17` made compile-side suppression per-resource, so an error costs one hook's or one server's fragment rather than the bundle. `.20` then narrowed consumer-side prune suppression before flipping any severity: `internal/materialize`'s `blocked` map is keyed by (output key, output COMPONENT) instead of by output key alone, and a Devin hook or MCP server writes to a shared aggregate FILE rather than to a component directory, so an erroring hook suppresses pruning of `hooks.v1.json` alone (through `devinAggregateWork.incomplete`) and leaves `skills/` and `agents/` pruning intact. `harnesscompile.Materialize`'s `WriteReport.PruneSuppressed` is still bundle-wide, but it governs the one-shot `convert`/`hcc` path that writes a whole output tree at once, not `sync`.

No `hook.devin.*` or `mcp.devin.*` message, hint, or log attribute ever contains a command string, prompt body, matcher value, timeout value, env value, header value, OAuth client id/secret/resource, an argument, or a URL — only field names, the fixed Devin event vocabulary, the resource name, and the source location.

### `mcp.interpolation-unsupported` is value-level; `mcp.devin.interpolation-unrepresentable` is entry-level

These two codes describe the same *cause* — a value uses an interpolation style
the target has no syntax for — and two different *outcomes*, which is why they
are two codes and not one code with two severities (bead
`agent-marketplace-5exj.23`).

`mcp.interpolation-unsupported` is the shared, **value-level** case and stays a
**warning**. One argument, one env value, one header value, or one OAuth member
is omitted — never written as an empty string, because a defined-but-blank
variable, header, or argument is a *different* input from the one the author
wrote — and the server entry is emitted with everything else the source
authorized. It sits with `mcp.devin.field-dropped` and the two plugin-root codes
in the emit-the-entry family: escalating it would suppress a working server over
one dropped value, which is exactly the trade `.20` rejected for that family.

`mcp.devin.interpolation-unrepresentable` is the **entry-level** case and is an
**error**. On Devin the unrepresentable value can be the one field the selected
server form cannot do without: `command` on a stdio server, `url` on a remote
server. Omitting it is not a partial loss — there is nothing left to launch or
contact, so the whole keyed entry disappears from `mcp_config.json`. That is the
same outcome as `mcp.devin.transport-ambiguous`, `-incomplete`, and
`-unsupported`, so it carries the same severity and the same consequence: the
entry is skipped, siblings are patched atomically, and `sync` exits non-zero.

Why a split rather than the two alternatives that were considered:

- **Escalating the shared code** would have been wrong even ignoring the other
  targets. A resource that emits any error diagnostic contributes *no* artifacts
  (see "What an error suppresses"), so an unrepresentable value in a single
  `env` entry would have started deleting the whole server — a behaviour
  regression dressed up as a severity fix, and the precise inversion of the
  emit-the-entry rule.
- **A per-target severity mechanism** would buy nothing here and cost the
  catalogue's central property: that a code means one thing wherever it appears.
  Consumer surfaces (`status`, `doctor`, the TUI) key off the code; a code whose
  severity depends on the target forces every one of them to re-derive the
  outcome. Splitting by outcome is also the pattern already in the catalogue —
  `subagent.security-restriction-unsupported` is shared while
  `subagent.factory.disallowed-tools-dropped` is per-target.

**The other five targets are unchanged, and that is deliberate rather than
incidental.** `mcp.interpolation-unsupported` is a shared code by *namespace*,
but Devin is its only emitter: Claude, Factory, and Cursor write MCP values
through verbatim with no interpolation mapping at all
(`compile/{claude,factory,cursor}/translator.go`, `translateMcpServer`), Gemini
rewrites env tokens through its own token table with no unmappable outcome
(`compile/gemini/translator.go`, `replaceEnvTokens`), and Windsurf calls
`mapping.McpInterpolation`, whose Windsurf column in `compile/mapping/mcp.go`
declares a pattern for all three source styles and therefore never reports
`ok=false`. None of the five can reach an entry-level interpolation loss, so
none of them needs the new error code, and the warning they never emit keeps its
value-level meaning for whichever of them acquires a genuinely unmappable cell
first. If a future mapping table marks a cell `mcpUnmappable` for one of them,
that target must make the same value-level/entry-level distinction before
emitting anything.

### Shared-aggregate ownership codes

Every `ownership.*` code is emitted by the consumer-side durable ownership layer
(`internal/ownership`), not by a compiler or ingester. They are catalogued here
because `codes.md` is the one stable diagnostic vocabulary consumer surfaces key
off, and because `doctor --repair` has to distinguish them by code rather than by
message text.

Severity follows the plan's recovery contract: *missing* state is a warning
(entries become unmanaged, nothing is pruned, `sync` can still complete), while
*corrupt* state, a state-key collision, a refused unsafe value, a failed
mandatory backup, and a foreign change under a staged generation are errors
(`doctor` exit 1). `ownership.recovered` is a warning: recovery is a success
path, decided by the recorded journal phase and never by a guess about which
entries look generated.

`ownership.state-missing` is only EMITTED when a record was expected: a root that
has a durable ownership directory or a retained safety backup, but no record. A
first-ever run has no record because nothing has ever been recorded for the root
— an absence that is unavoidable and harmless — so it fails closed in exactly the
same way but is logged at debug instead of reported. The classification comes from
durable state (`ownership.Store.EverRecorded`), never from aggregate bytes.

An `ownership.*` diagnostic about a shared aggregate names the AGGREGATE it
concerns (its install root plus its output-relative path), not the durable state
file: one root's ownership record covers several aggregates, so a diagnostic
built from the state path alone cannot be told apart from the same diagnostic
about a sibling file.

No `ownership.*` message, hint, or log attribute ever contains a hook payload,
matcher, timeout value, MCP env value, header value, OAuth client id/secret/
resource, command string, or aggregate file content. Only file paths, the
non-secret entry identity (an MCP server object key or a hook event name),
content digests, and generation numbers are reported.

`ownership.generation-conflict` and `ownership.foreign-change` are deliberately
separate codes. `ownership.generation-conflict` is the live compare-and-swap
refusal inside `Store.Apply`: the aggregate or the durable ownership record
changed after this run read it, so the mutation was refused and the file was
left as found. `ownership.foreign-change` is recovery-only: a staged generation
was interrupted, and when recovery ran the live file was neither the recorded
pre-mutation content nor the recorded staged content, so the generation was
abandoned. One is a live race prevented by the CAS; the other is evidence that
something outside `agent-marketplace` rewrote the file while a generation was
in flight.

On a filesystem where advisory locking is unavailable, this precondition is the
last line of defense that keeps the unserialized path safe: the mutation is
refused and the aggregate is left as found. With locking enabled it should never
fire.

### `ownership.lock-*`: serialization, and why there is no stale-lock code

Ownership mutations are serialized per canonical target root by an advisory
`flock(2)` on `$XDG_STATE_HOME/agent-marketplace/locks/<rootKey>.lock`
(`internal/ownership/lock.go`). Without it, two writers on one root read the
aggregate and the ownership record at one instant and publish them at another,
so the durable record ends up describing a file it does not match. The lock file
is a SIBLING of `ownership/` and `safety-backups/`, never a child, because
`ownership.Store.EverRecorded` reads those two directories as durable evidence
and a lock file inside them would turn every first run's silent first-use path
into an `ownership.state-missing` warning.

The three codes divide by outcome, not by cause:

- `ownership.lock-contended` (info) — the run WAITED and then succeeded. A save
  hook racing a human is the mechanism working as designed, so it must not
  inflate the warning count that `--strict` keys off. Only reported when the
  wait is long enough for a user to have noticed it.
- `ownership.lock-timeout` (error) — the run did NOT reconcile that root and left
  its aggregates exactly as found. Only that root is skipped; other roots and
  other targets continue, and the run exits non-zero.
- `ownership.lock-unavailable` (warning) — the platform or filesystem refuses
  advisory locks (`ENOTSUP`/`EOPNOTSUPP`, realistically an NFS-mounted state
  root, or a non-unix build), so the mutation proceeds unserialized. `ENOLCK`
  is transient lock-table exhaustion and is retried like contention; it only
  degrades here if it persists to the lock timeout. That is byte-for-byte the
  behaviour that predates locking, on exactly the filesystems where it is
  unavoidable, but it is reported rather than silent.

The compare-and-swap precondition in `Store.Apply` — reported as
`ownership.generation-conflict` — is what makes the degraded path safe: it
refuses the mutation when another writer or an external tool changed the
aggregate or the durable record after this run read it, so the unserialized path
cannot commit a record that does not describe the file.

The wait is bounded by `AGENT_MARKETPLACE_OWNERSHIP_LOCK_TIMEOUT`, a Go duration
whose default (30 s) is `config.DefaultOwnershipLockTimeout`. It is the escape
hatch `ownership.lock-timeout`'s hint names, so it is implemented rather than
merely promised: `0` means fail fast (one non-blocking attempt), a value that does
not parse or is negative is refused loudly instead of ignored, and precedence is
env var > in-process configuration > code default. A legitimate section is
milliseconds long, so raising it is for CI runners that deliberately queue two
syncs against one machine root, not routine tuning.

The section is held across a whole canonical root — `Recover` plus every one of
that root's aggregate files, INCLUDING the reads the patch is computed from — so a
contended root waits once per run rather than once per file, and a wait or a
timeout is reported once naming the root rather than once per aggregate. A DRY RUN
(`sync --check`, `status`, `list`, the TUI refresh, `doctor`'s preview) takes no
lock and creates nothing under the state root.

There is deliberately **no `ownership.lock-stale-reclaimed` code**, and its
absence is a decision, not an oversight. A `flock` lock belongs to the open file
description, so the kernel releases it on `Close`, exit, `panic`, `SIGKILL`,
OOM-kill, and container teardown: a crashed process cannot wedge a future sync,
so there is no reclamation event to report. Every mitigation an `O_EXCL`
lock-file design would need is also wrong here — a PID liveness check is
meaningless under PID namespaces and PID reuse, a timestamp threshold is wrong
under clock skew and under a legitimately long section, and a break protocol is
itself racy. The lock file is likewise never unlinked, because
unlink-and-recreate splits the lock across two inodes and silently permits two
holders.

### Devin materialization codes

Every `materialize.devin.*` code is emitted by `internal/materialize`'s Devin
aggregate reconciler, the D10 layer that patches `hooks.v1.json`,
`config.json#hooks`, and `mcp_config.json` through
`pkg/harness-compilation/jsonc` under `internal/ownership` authority.

Severity follows the plan's per-file atomicity rule: "Aggregate updates are
atomic per file, not per target ... Malformed destination JSON blocks that file
only." A malformed destination, an unpatchable shape, and a failed journaled
mutation are therefore errors that block exactly one file and leave it byte-for-
byte as found, while every other file, skill, and subagent in the same run is
still materialized. `materialize.devin.symlink-required` is retired and no longer
emitted. Its original restriction reflected integration delivery scope, not a
Devin runtime limitation. All targets now use the generic copy/link materializer;
the exported diagnostic constant remains for compatibility with older consumers.

`materialize.devin.hook-satisfied-by-unmanaged` is a warning, not an error,
because the plan says "One semantically identical unmanaged hook may satisfy any
number of selected marketplace resources without duplication or adoption" — the
desired behaviour is present, so nothing failed; the warning exists only so the
user learns why no managed entry appeared.

`materialize.devin.aggregate-prune-postponed` is a warning that implements "A
partial run does not prune: a resource skipped this run is indistinguishable
from a resource that was removed". It is scoped to the ONE aggregate file whose
desired entry set was incomplete, so an erroring hook cannot suppress pruning of
`mcp_config.json` or of the `skills/` directory.

No `materialize.devin.*` message, hint, or log attribute ever contains a hook
payload, matcher, timeout value, MCP env value, header value, OAuth
client id/secret/resource, command string, or aggregate file content. Only the
output-relative file path, the non-secret entry identity (an MCP server object
key or a hook event name), the resource name, and content digests are reported.

## Error

| Code | Summary | Hint |
|------|---------|------|
| `materialize.codex.aggregate-malformed` | A Codex aggregate could not be parsed or safely reconciled, so that file was left exactly as found | Fix the reported hooks.json or config.toml error by hand; the malformed file is preserved |
| `materialize.codex.aggregate-unpatchable` | An existing Codex MCP server uses an alternate TOML form that cannot be safely patched, so that entry was preserved | Rewrite the server as a standard [mcp_servers.&lt;name&gt;] table or edit it by hand before syncing |
| `materialize.destination.overlap` | Resolved Codex destinations overlap in a way that cannot be safely coalesced, so materialization was refused | Use compatible repository and machine modes, resolve conflicting resources, or separate the repository and CODEX_HOME roots |
| `subagent.factory.tool-unknown` | Factory cannot map an explicit subagent tool; subagent artifact suppressed | Use a supported tool mapping or remove the restriction explicitly |
| `skill.codex.security-restriction-unsupported` | Codex skills cannot enforce an explicit security restriction; artifact suppressed | Remove the restriction explicitly or use a target that represents it |
| `hook.event-unmappable` | Target cannot preserve this hook event; hook suppressed | Use a hook event documented by the target |
| `hook.gemini.event-unmappable` | Gemini has no equivalent hook event; hook suppressed | Use a Gemini native event such as BeforeTool, AfterTool, or PreCompress |
| `hook.windsurf.event-unmappable` | Claude hook event has no Windsurf equivalent; hook suppressed | Consider using a mappable event (PreToolUse with a known tool matcher, UserPromptSubmit, or Stop) |
| `hook.matcher-untranslatable` | No exact tool-matcher coverage exists for the target; hook suppressed | Rewrite the matcher with tools the target's catalog covers, or remove it |
| `hook.env-unrepresentable` | The hook command references a variable with no target equivalent; hook suppressed | Rewrite the command with variables the target documents, or use an absolute path |
| `hook.timeout-unsupported` | Target cannot represent an explicit hook timeout; hook suppressed | Remove the timeout explicitly or use a target that supports it |
| `hook.type-unsupported` | The target's catalog does not declare this hook type for the event; hook suppressed | Use a hook type the target documents for this event |
| `hook.support-path-unrepresentable` | The hook command references the plugin root but the target declares no repository base; hook suppressed | Use an absolute command path or a target with a repository base |
| `hook.prompt.unsupported` | Target cannot represent prompt hooks; hook suppressed | Use a command hook on this target |
| `hook.factory.non-command-type-dropped` | Factory only supports command-type hooks; hook suppressed | Convert the hook to a command type or remove it |
| `hook.cursor.unsupported-type` | Cursor does not declare this hook type for the event; hook suppressed | Use a command or prompt type; the Cursor catalog declares both on every event |
| `hook.gemini.unsupported-type` | Gemini cannot represent this hook type; hook suppressed | Use a command hook on Gemini |
| `hook.windsurf.unsupported-type` | Windsurf cannot represent this hook type; hook suppressed | Use a command hook on Windsurf |
| `materialize.hook.legacy-malformed` | A legacy hook file could not be parsed or reconciled safely; both files were left unchanged | Fix the legacy hook file by hand; the migration runs again on the next sync |
| `hook.native-literal-unsupported` | Target would reinterpret a native Codex hook literal; hook suppressed | Remove the target-specific interpolation token or keep the hook in Codex format |
| `hook.codex.native-fields-unsupported` | Target cannot preserve native Codex hook execution or context fields; hook suppressed | Remove async, commandWindows, and additionalContextLimit explicitly or keep the hook in Codex format |
| `hook.codex.unsupported` | Codex does not execute this hook event or handler; hook suppressed | Use a documented Codex command hook event |
| `hook.windsurf.native-fields-unsupported` | Windsurf cannot preserve conflicting native hook fields; hook suppressed | Remove the conflicting native fields explicitly or use a target that represents them |
| `hook.windsurf.timeout-unsupported` | Windsurf cannot represent an explicit hook timeout; hook artifact suppressed | Remove the timeout explicitly or use a target that supports it |
| `materialize.hook.support-conflict` | Conflicting desired support files block affected hooks rather than selecting a winner | Rename the conflicting support files or exclude one hook provider |
| `materialize.hook.support-refused` | Unsafe or unreadable support sources block affected hooks while installed content is retained | Fix or remove the unsafe support source and re-run sync |
| `mcp.security-restriction-unsupported` | Target cannot preserve native MCP authority or literal semantics; server suppressed | Keep the server in its native format or remove the unsupported security field explicitly |
| `mcp.codex.security-unsupported` | Codex MCP output cannot preserve the requested authentication or transport semantics; server suppressed | Use documented Codex MCP fields or remove the unsupported setting explicitly |
| `ingest.codex.load-resource-failed` | One malformed Codex resource or aggregate was skipped | Fix the reported native file; independent valid resources were retained |
| `ingest.codex.duplicate-identity` | Duplicate Codex resource identity was suppressed without choosing a winner | Rename one duplicate native resource |
| `ingest.codex.hook-unsupported` | An unsupported native Codex hook event or handler was skipped | Use a documented Codex event with a command handler |
| `ingest.codex.mcp-security-unsupported` | Security-sensitive native Codex MCP fields cannot be represented safely | Remove the unsupported restriction or keep the server in Codex format |
| `ingest.codex.subagent-security-unsupported` | Authority-bearing native Codex subagent fields cannot be represented safely | Remove the unsupported restriction or keep the subagent in Codex format |
| `materialize.codex.duplicate-identity` | Duplicate desired Codex aggregate identity was suppressed without choosing a winner | Rename one duplicate selected resource |
| `subagent.factory.disallowed-tools-dropped` | Factory cannot represent disallowedTools; artifact suppressed | Use a target that supports disallowedTools or remove the restriction explicitly |
| `subagent.factory.isolation-dropped` | Factory cannot represent isolation; artifact suppressed | Use a target that supports isolation or remove the restriction explicitly |
| `subagent.gemini.tool-unknown` | Gemini tool is not in the mapping table; artifact suppressed | Add the tool to the mapping table or remove the explicit tool |
| `subagent.cursor.tool-unknown` | Cursor cannot represent the authored subagent tool restriction; artifact suppressed | Remove the restriction explicitly or use a target whose subagent format can express it |
| `subagent.claude.tool-unknown` | Claude has no tool covering a foreign-source tool name; artifact suppressed | Use a tool name the Claude tool catalog covers, or remove the restriction explicitly |
| `subagent.security-restriction-unsupported` | Unsupported subagent security restriction suppresses the artifact | Use a target that represents the restriction or remove it explicitly |
| `bundle.conflict.duplicate-artifact-path` | Two resources produce the same artifact output path; neither resource is emitted | Rename one of the conflicting resources or adjust the output path |
| `bundle.conflict.cross-kind-artifact-path` | Resources of different kinds produce the same artifact output path; neither artifact is emitted | Rename one of the conflicting resources: on targets that compile commands as skills, a skill and a command cannot share a name |
| `bundle.compile.resource-failed` | A resource compiler failed | Fix the resource compiler error before materializing the bundle |
| `ingest.claude.load-skill-failed` | Failed to load a skill from the Claude source layout | Check the skill directory structure and SKILL.md frontmatter |
| `ingest.claude.load-command-failed` | Failed to load a command from the Claude source layout | Check the command .md file structure and frontmatter |
| `ingest.claude.load-subagent-failed` | Failed to load a subagent from the Claude source layout | Check the agent .md file structure and frontmatter |
| `ingest.factory.load-skill-failed` | Failed to load a skill from the Factory source layout | Check the skill directory structure and SKILL.md contents |
| `ingest.factory.load-command-failed` | Failed to load a command from the Factory source layout | Check the command .md file structure |
| `ingest.factory.load-subagent-failed` | Failed to load a subagent from the Factory source layout | Check the droid .md file structure and frontmatter |
| `ingest.factory.load-mcp-servers-failed` | Failed to load MCP servers from the Factory source layout | Check the mcp.json file structure |
| `ingest.cursor.load-skill-failed` | Failed to load a skill from the Cursor source layout | Check the skill directory structure and SKILL.md frontmatter |
| `ingest.cursor.load-command-failed` | Failed to load a command from the Cursor source layout | Check the command .md file structure and frontmatter |
| `ingest.cursor.load-subagent-failed` | Failed to load a subagent from the Cursor source layout | Check the agent .md file structure and frontmatter |
| `ingest.gemini.load-skill-failed` | Failed to load a skill from the Gemini source layout | Check the .gemini/skills/<name>/SKILL.md layout |
| `ingest.gemini.load-command-failed` | Failed to load a command from the Gemini source layout | Check the commands/*.toml file structure |
| `ingest.gemini.load-subagent-failed` | Failed to load a subagent from the Gemini source layout | Check the .gemini/agents/*.md frontmatter |
| `ingest.gemini.load-hook-failed` | Failed to load a hook from the Gemini source layout | Check the settings.json hook structure |
| `ingest.factory.load-rule-failed` | Failed to load a rule from the Factory source layout | Check the .factory/rules/*.md file structure |
| `ingest.cursor.load-rule-failed` | Failed to load a rule from the Cursor source layout | Check the .cursor/rules/*.mdc file structure and frontmatter |
| `subagent.devin.builtin-name-conflict` | Subagent name collides with a Devin built-in subagent, which cannot be replaced; artifact suppressed | Rename the subagent so it does not shadow a Devin built-in such as subagent_explore or subagent_general |
| `subagent.devin.nested-name-unsupported` | Subagent name expresses nesting, which Devin's flat agents directory cannot represent; artifact suppressed | Rename the subagent to a flat name: Devin discovers only `agents/<name>.md`, and flattening the name could collide with another subagent |
| `hook.devin.event-unmappable` | Hook event is not documented by Devin; the hook entry was skipped and sync exits non-zero | Use a Devin-documented hook event: PreToolUse, PostToolUse, PermissionRequest, UserPromptSubmit, Stop, SessionStart, SessionEnd, or PostCompaction |
| `hook.devin.unsupported-type` | Devin does not represent this hook type; the hook entry was skipped and sync exits non-zero | Use a command or prompt hook supported by Devin |
| `hook.devin.matcher-unsupported-event` | Devin documents matchers for tool events only; the hook entry was skipped rather than emitted without its matcher | Remove the matcher, or move the hook to PreToolUse or PostToolUse |
| `hook.devin.payload-missing` | The hook type selects a payload the resource does not carry; the hook entry was skipped | Set the command payload on a command hook or the prompt payload on a prompt hook |
| `hook.devin.timeout-invalid` | Hook timeout is negative, which Devin does not define; the hook entry was skipped | Use a non-negative timeout or remove the timeout |
| `mcp.devin.transport-ambiguous` | The MCP server declares both a stdio command and a remote url, which Devin's two documented server forms cannot express together; the server entry was skipped | Split the definition into one stdio server and one remote server |
| `mcp.devin.transport-incomplete` | The MCP server declares neither a stdio command nor a remote url; the server entry was skipped | Set a command for a stdio server or a url for a remote server |
| `mcp.devin.transport-unsupported` | The MCP server requests a transport Devin does not document; the server entry was skipped rather than defaulted | Devin documents the http and sse remote transports |
| `mcp.devin.interpolation-unrepresentable` | The `command` or `url` the server's documented form cannot do without uses an interpolation style Devin has no syntax for; the server entry was skipped | Devin documents `${env:VAR}` interpolation only; move the value into an environment variable, or use a literal command or url |
| `ingest.devin.load-skill-failed` | Failed to load a skill directory from the Devin source layout | Check the skills/&lt;name&gt;/SKILL.md layout and its YAML frontmatter |
| `ingest.devin.load-subagent-failed` | Failed to load a flat subagent profile from the Devin source layout | Check the agents/&lt;name&gt;.md frontmatter |
| `ingest.devin.load-hook-failed` | Failed to load hooks from the Devin source layout | Check the hooks.v1.json event object or the hooks member of config.json |
| `ingest.devin.load-mcp-servers-failed` | Failed to load MCP servers from the Devin source layout | Check the mcpServers object in mcp_config.json |
| `ingest.devin.tool-unreversible` | An allowed-tools entry is outside Devin's closed read/edit/grep/glob/exec vocabulary; the resource was not ingested | Devin silently ignores unknown tool names, so the restriction is not enforced; fix the allowed-tools list rather than letting an unenforced restriction become canonical IR |
| `json.duplicate-key` | JSON object contains the same key more than once; the document was rejected before it could be decoded | Remove the duplicate key: encoding/json would silently keep the last value and change commands, headers, env, disabled state, or hooks |
| `json.malformed` | JSON document is not well-formed and was rejected before it could be decoded | Fix the JSON syntax reported in the message |
| `ownership.state-corrupt` | The durable ownership record could not be read or failed validation; every existing aggregate entry is treated as unmanaged | Nothing is pruned or replaced; run doctor --repair to re-establish ownership from exact matches |
| `ownership.state-root-mismatch` | An ownership record was found under this root key but records a different canonical target root | This is a state-key collision and is refused rather than resolved; report it, and move or remove the conflicting record |
| `ownership.state-unsafe-value` | Refused to record an ownership value that is not a non-secret identity | Ownership state stores object keys, event names, and content digests only; never a payload, matcher, env value, header, or secret |
| `ownership.safety-backup` | The mandatory raw-byte safety backup could not be written; the aggregate mutation was refused | Safety backups are mandatory even when configured backups are disabled; fix the XDG state directory permissions |
| `ownership.foreign-change` | The aggregate file changed underneath a staged generation; the generation was abandoned and the file left as found | Re-run sync: the file on disk is neither the pre-mutation nor the staged content, so it was written by something else |
| `ownership.lock-timeout` | This target root's ownership lock could not be acquired before the timeout, so its aggregates were left exactly as found | Another agent-marketplace process is still holding it; wait for that run to finish, or raise AGENT_MARKETPLACE_OWNERSHIP_LOCK_TIMEOUT |
| `ownership.generation-conflict` | The aggregate file or ownership generation changed underneath this mutation; the aggregate was left as found | Another writer, an external tool, or an unserialized run on a filesystem without advisory locking changed the file or the ownership record after this run read it; re-run sync |
| `materialize.devin.aggregate-malformed` | The destination Devin aggregate JSON could not be parsed, so that file was left exactly as found | Fix the reported parse location by hand; a safety backup of the raw bytes is retained under the Agent Marketplace state root |
| `materialize.devin.aggregate-shape` | The destination Devin aggregate has a shape this target cannot patch, so that file was left exactly as found | The aggregate root and its hooks/mcpServers members must be JSON objects; move the conflicting value out of the way by hand |
| `materialize.devin.aggregate-write` | The journaled Devin aggregate mutation failed, so that file was left exactly as found | Nothing was partially written; check the reported cause and re-run sync |
| `materialize.devin.symlink-required` | Retired; older versions refused file mode for Devin | No longer emitted; Devin supports both file and symlink modes through the generic materializer |
| `doctor.repair.failed` | An authorized `doctor --repair` action failed; the aggregate was left exactly as found | This is the "partial repair failure" half of the repair contract and exits 1; nothing was partially written, so re-running the repair is safe |
| `hook.native-tolerance-refused` | A native hook file was malformed, ambiguous, or unsafe to inspect, so the affected target was left exactly as found | Fix the reported native hook file by hand; no native or legacy hook bytes were changed |

### Devin doctor and repair codes

`doctor.devin.*` and `doctor.repair.*` are emitted by the consumer-side `doctor`
command (work package D11). They exist because `doctor --repair` "has to
distinguish them by code rather than by message text", exactly as the ownership
codes do.

The two version codes are WARNINGS and neither blocks anything, because the plan
states "Sync does not require a Devin binary. `doctor` detects an installed
binary and warns when its version is below 3000.3." Detection is best-effort by
construction: a missing binary, a non-zero exit, a probe that hangs past its
timeout, and output with no recognizable version are all reported rather than
raised, and `doctor` still exits 0 for all of them.

`doctor.devin.recommended` is INFO and implements "recommends selecting the
harness when installed but unconfigured". It is a recommendation only: "`doctor
--repair` owns aggregate-state recovery only; it does not silently edit harness
lists."

`doctor.repair.failed` is the only repair ERROR, and it maps to exactly one half
of the plan's exit contract: "exit 1 means corrupt state/file or partial repair
failure". Declining a prompt, having nothing to repair, and running off-TTY in
preview-only mode are all clean outcomes: "exit 0 means clean, repaired, or
declined without operational failure".

`doctor.repair.conflict-replaceable` is the THIRD repair class, and it exists
because the first two provably could not describe one real state: an entry whose
KEY matches a desired entry but whose VALUE differs. A collision needs only key
equality, so sync reported it forever; adoption needs value-digest equality, so
it was not adoptable; orphan nomination needs retained backup or journal
evidence, which only a completed mutation produces — and the colliding entry was
never written. `doctor --repair` therefore answered "nothing to repair" while
sync refused to install, with no way out but hand-editing the file the tool
refused to touch.

It is a WARNING, not an error: the existing entry is preserved and the offer is
optional. Acting on it inherits ORPHAN REMOVAL's safety contract rather than
adoption's, because it destroys user-visible content — interactive per-entry
confirmation, a fresh mandatory safety backup taken before the mutation, never
under `--yes`, and preview-only off a terminal. Declining leaves the file
byte-identical.

No `doctor.*` message, hint, or log attribute contains any user file content, an
aggregate payload, a hook payload, a matcher, an MCP env value, a header, or an
OAuth secret. Only the resolved binary path, the reported version banner excerpt,
the compatibility floor, output-relative aggregate paths, non-secret entry
identities, and digests are reported.

### What an error suppresses

Suppression is **per resource**, never bundle-wide. A resource that emits any error diagnostic contributes no artifacts at all — not its own files and not its fragments of a shared aggregate such as `hooks.v1.json`, `mcp_config.json`, or `plugin.json`. Every other resource in the same bundle is published normally, and the run is still reported as failed (`Result.HasErrors`, `Result.Skipped`, and `harnesscompile.ErrPartialResult` from `Materialize`).

`bundle.conflict.duplicate-artifact-path` and `bundle.conflict.cross-kind-artifact-path` are the only error codes not attributable to a single resource: neither producer is wrong on its own. They suppress **both** named producers as whole resources, for the same fragment reason — a manifest must never advertise an entry whose file was not written. Load/ingest failures (`ingest.*.load-*-failed`) describe a resource that never reached the compiler, so they suppress nothing beyond the resource they already lost. Any future bundle-level error code must state its own suppression rule explicitly.

**`skill.devin.tool-unknown` and `subagent.devin.tool-unknown` remain warnings.**
They omit the target-specific tools key and publish the artifact under the
Devin-specific policy above. Factory, Gemini and Cursor unknown-tool errors
suppress the affected artifact. Suppression remains per resource: independent
valid resources still compile.

## Examples

### Info examples

**`ingest.claude.bundle-manifest-synthesized`**
A `.claude/` workspace directory with skills/agents/commands but no `.claude-plugin/plugin.json`:
```
.claude/skills/deploy/SKILL.md
.claude/commands/deploy.md
```
The ingester synthesizes a default manifest since no plugin.json is present.

**`ingest.factory.bundle-manifest-synthesized`**
A `.factory/` workspace directory with no `.factory-plugin/plugin.json`:
```
.factory/skills/deploy/SKILL.md
.factory/commands/deploy.md
```
The Factory ingester synthesizes a default manifest.

**`ingest.factory.reasoning-effort-mapped-to-model`**
Factory droid with `reasoningEffort: "high"` and `model: "claude-sonnet-4-5-20250929"`:
```yaml
name: reviewer
model: claude-sonnet-4-5-20250929
reasoningEffort: high
```
The ingester canonicalizes `model` to `"sonnet"` and preserves `reasoningEffort` in `Ext.Factory.ReasoningEffort`.

**`ingest.gemini.toml-command-canonicalized`**
A Gemini TOML command file:
```toml
[command]
name = "deploy"
description = "Deploy the project"
prompt = "Run deployment"
timeout_minutes = 5
```
The `timeout_minutes` field is preserved in `Ext.Gemini` but does not exist in the canonical IR `Command`.

**`ingest.windsurf.workflow-assumed-command`**
A Windsurf workflow file:
```markdown
---
name: deploy
description: Deploy the project
---
Run deployment steps
```
Since Windsurf has a native skill primitive (`.windsurf/skills/`), workflows are ingested as `ir.Command`.

**`ingest.windsurf.literal-path-preserves`**
A Windsurf MCP config with a literal path:
```json
{"mcpServers":{"docs":{"command":"/usr/local/bin/docs-server"}}}
```
Unlike Claude's `${CLAUDE_PLUGIN_ROOT}`, Windsurf has no canonical env token, so literal paths are preserved as-is.

**`ingest.windsurf.mcp-env-normalized`**
A Windsurf MCP config with `${env:VAR}` interpolation:
```json
{"mcpServers":{"github":{"command":"npx","env":{"GITHUB_TOKEN":"${env:GITHUB_TOKEN}"}}}}
```
The `${env:GITHUB_TOKEN}` is normalized to `${GITHUB_TOKEN}` (canonical Claude-style form) during ingestion so the IR is harness-neutral. The Windsurf compile target will convert it back to `${env:GITHUB_TOKEN}` at output time.

**`ingest.windsurf.subagent-via-agents-md-skipped`**
A Windsurf workspace with an AGENTS.md file:
```
.windsurf/AGENTS.md
```
Windsurf has no subagent primitive, so AGENTS.md is not synthesized into a subagent resource.

### Warning examples

**`subagent.windsurf.no-primitive`**
Any subagent compiled to Windsurf:
```yaml
name: reviewer
model: sonnet
tools: [Read, Edit]
```
Windsurf has no subagent primitive; the entire resource is dropped.

**`hook.windsurf.event-unmappable`**
A Claude hook with event `Notification` compiled to Windsurf:
```json
{"hooks":{"Notification":[{"type":"command","command":"echo notify"}]}}
```
The `Notification` event has no Windsurf equivalent; the hook is dropped.

**`ingest.windsurf.hook-event-unmappable`**
A Windsurf hook with event `post_setup_worktree` ingested to Claude IR:
```json
{"hooks":{"post_setup_worktree":[{"command":"echo setup"}]}}
```
`post_setup_worktree` has no Claude equivalent; the native event is preserved in the Windsurf sidecar with a warning.

**`mcp.interpolation-unsupported`**
An MCP server whose `env` uses Windsurf's `${file:...}` style, compiled to Devin:

```json
{"mcpServers":{"docs":{"command":"npx","env":{"KEEP":"plain","TOKEN":"${file:/etc/secret}"}}}}
```

Devin has no `${file:...}` syntax, so the `TOKEN` key is omitted — never written
as an empty string — and the entry is still emitted with `command` and `KEEP`.
Only the field name reaches the diagnostic; the value never does.

### Error examples

**`subagent.factory.disallowed-tools-dropped`**
A Claude subagent with `disallowedTools`:

```yaml
name: reviewer
model: sonnet
disallowedTools:
  - Bash
  - Write
```

Factory does not support `disallowedTools`; the subagent artifact is suppressed
and `sync` exits non-zero.

**`subagent.factory.isolation-dropped`**
A Claude subagent with `isolation`:

```yaml
name: reviewer
model: sonnet
isolation: worktree
```

Factory handles isolation at the droid runtime level; the subagent artifact is
suppressed and `sync` exits non-zero.

**`mcp.devin.interpolation-unrepresentable`**
The same unrepresentable style, but in the field the stdio form cannot do without:

```json
{"mcpServers":{"docs":{"command":"${file:/etc/command}"}}}
```

There is nothing left to launch, so the whole `docs` entry is skipped rather
than written without a `command`, and `sync` exits non-zero. The remote-form
equivalent is a `url` that cannot be represented.

**`bundle.conflict.duplicate-artifact-path`**
Two resources that produce the same output file:

```text
skills/deploy/SKILL.md  →  skills/deploy/SKILL.md
commands/deploy.md      →  skills/deploy/SKILL.md  (hypothetical collision)
```

When two resources produce artifacts at the same path, an error diagnostic is emitted and neither colliding artifact is emitted.

**`bundle.conflict.cross-kind-artifact-path`**
A skill and a command with the same name on a target that compiles commands as
skills (Devin):

```text
skills/deploy/SKILL.md  →  skills/deploy/SKILL.md   (skill "deploy")
commands/deploy.md      →  skills/deploy/SKILL.md   (command "deploy")
```

The diagnostic names both resources and neither colliding artifact is emitted;
no kind silently wins.

**`ingest.claude.load-skill-failed`**
A skill directory with malformed frontmatter:

```markdown
---
name: my-skill
invalid yaml: [
---
Skill body
```

The YAML parser fails and an error diagnostic is emitted with the skill path.

**`json.duplicate-key`**
An MCP config whose auth header map repeats a key:

```json
{"mcpServers":{"docs":{"headers":{"Authorization":"a","Authorization":"b"}}}}
```

`encoding/json` accepts this with last-value-wins, silently changing the header
that is sent. Every canonical and native JSON loader, the merge-with-existing
path, the JSONC concrete-syntax layer, and the consumer-side marker-prune
write-back (`internal/materialize`, which rewrites a user's `mcp.json`,
`hooks.json`, `plugin.json`, or `settings.json` in place) all reject the document
before decoding it. The write-back case is the most severe one, because a
collapse there is PERSISTED into the user's own file, so it refuses the write and
leaves the destination byte-identical. The failure is fail-closed (never a
warning) and surfaces as a `*diag.CodedError` carrying this code, the source file,
and the 1-based line of the offending key. The message reports the JSON key path
(`$.mcpServers.docs.headers`) but never the duplicated value, so credentials in
`env`/`headers` maps are not echoed into diagnostics or logs.

**`json.malformed`**
Any JSON document that is not well-formed (truncated object, trailing data after
the top-level value, nesting beyond the supported depth). Rejected with the same
source-aware shape as `json.duplicate-key`.

## OpenCode

| Code | Severity | Outcome |
|---|---|---|
| `rule.opencode.unsupported` | warning | Rules and shared instructions are outside this target |
| `compile.opencode.field-dropped` | warning | Metadata has no native equivalent and was omitted |
| `skill.opencode.security-restriction-unsupported` | error | Skill execution restrictions cannot be represented; resource omitted |
| `command.opencode.restriction-unsupported` | error | Command execution restrictions cannot be represented; resource omitted |
| `compile.opencode.invalid-field` | error | A modeled native field has an invalid type; resource omitted |
| `subagent.opencode.restriction-unsupported` | error | Agent execution restrictions cannot be represented; resource omitted |
| `mcp.opencode.unrepresentable` | error | MCP transport, authentication, or reference fields cannot be represented; server omitted |
| `hook.opencode.unsupported` | error | Hook event, matcher, handler, or deadline is unsupported; hook omitted |
| `ingest.opencode.invalid-resource` | error | Native resource decoding failed; resource omitted |
| `ingest.opencode.duplicate-identity` | error | Distinct native files claim the same resource identity; resources omitted |
| `ingest.opencode.read-refused` | error | Native resource is not an admitted bounded regular file; resource omitted |
| `ingest.opencode.plugin-unsupported` | error | Arbitrary or altered executable plugin cannot be ingested |
| `ingest.autodetect.ambiguous` | warning | Source carries two harness layouts; the unselected layout's resources are not ingested |
| `compile.opencode.native-semantics-unsupported` | error | Native execution or reference semantics cannot be preserved by the destination |
| `materialize.opencode.aggregate-write` | error | Native MCP configuration could not be published |
| `materialize.opencode.aggregate-malformed` | error | Native MCP configuration is malformed or ambiguous; existing bytes preserved |
| `materialize.opencode.aggregate-prune-postponed` | warning | Incomplete MCP input postpones stale-entry removal |
| `materialize.opencode.aggregate-collision` | error | Selected MCP server conflicts with an unmanaged entry; existing bytes preserved |
