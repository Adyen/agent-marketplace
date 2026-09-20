package diag

// v0 diagnostic code constants. Every constant must have a corresponding
// entry in the codes.md catalog; TestDiagnosticCodes_Documented enforces this.

// Info codes.
const (
	CodeSubagentCursorModelPassthrough            = "subagent.cursor.model-passthrough"               // Cursor model not in mapping table; passed through
	CodeIngestClaudeManifestSynthesized           = "ingest.claude.bundle-manifest-synthesized"       // Workspace form has no plugin.json; manifest synthesized
	CodeIngestFactoryManifestSynthesized          = "ingest.factory.bundle-manifest-synthesized"      // Workspace form has no plugin.json; manifest synthesized
	CodeIngestFactoryReasoningEffortMappedToModel = "ingest.factory.reasoning-effort-mapped-to-model" // Factory reasoningEffort preserved while model canonicalized
	CodeIngestCursorManifestSynthesized           = "ingest.cursor.bundle-manifest-synthesized"       // Workspace form has no plugin.json; manifest synthesized
	CodeIngestGeminiManifestSynthesized           = "ingest.gemini.bundle-manifest-synthesized"       // Workspace form has no gemini-extension.json; manifest synthesized
	CodeIngestGeminiTOMLCommandCanonicalized      = "ingest.gemini.toml-command-canonicalized"        // TOML command metadata normalized into canonical IR
	CodeIngestWindsurfManifestSynthesized         = "ingest.windsurf.bundle-manifest-synthesized"     // Windsurf workspace form synthesized manifest
	CodeIngestWindsurfWorkflowAssumedCommand      = "ingest.windsurf.workflow-assumed-command"        // Windsurf workflow loaded as command
	CodeIngestWindsurfLiteralPathPreserved        = "ingest.windsurf.literal-path-preserved"          // Windsurf literal MCP path preserved
	CodeIngestWindsurfMcpEnvNormalized            = "ingest.windsurf.mcp-env-normalized"              // Windsurf MCP env interpolation normalized to canonical form
	CodeIngestWindsurfSubagentViaAgentsMDSkipped  = "ingest.windsurf.subagent-via-agents-md-skipped"  // AGENTS.md not synthesized as subagent
	CodeIngestDevinManifestSynthesized            = "ingest.devin.bundle-manifest-synthesized"        // Devin has no manifest file; manifest synthesized
	CodeIngestDevinLocalOverrideIgnored           = "ingest.devin.local-override-ignored"             // A .local.json personal override file was found and not ingested
	CodeIngestDevinMcpEnvNormalized               = "ingest.devin.mcp-env-normalized"                 // Devin ${env:VAR} interpolation normalized to the canonical ${VAR} form
	CodeIngestCodexManifestSynthesized            = "ingest.codex.bundle-manifest-synthesized"        // Codex workspace has no manifest

	// Devin DOCTOR codes (work package D11). Emitted by the consumer-side
	// `doctor` surface, which detects the locally installed Devin CLI and
	// recommends the harness. They carry only the binary path, the reported
	// version, and the compatibility floor — never a user file, a config value,
	// or aggregate content.
	CodeDoctorDevinRecommended = "doctor.devin.recommended" // Devin is installed but not selected in any harness list

	// Shared-aggregate ownership LOCKING code (bead agent-marketplace-9gmb).
	// Ownership mutations are serialized per canonical target root by an
	// advisory flock(2) on $XDG_STATE_HOME/agent-marketplace/locks/<rootKey>.lock,
	// so two processes cannot publish an aggregate file and its ownership record
	// at different instants. Waiting a few milliseconds for another process is
	// the mechanism working, not a problem, which is why this is Info: a save
	// hook racing a human must not inflate the warning count that --strict keys
	// off. It carries the root key and the wait duration only.
	CodeOwnershipLockContended = "ownership.lock-contended" // Another process held this root's ownership lock; this run waited for it
)

// Warning codes.
const (
	CodeSubagentWindsurfNoPrimitive         = "subagent.windsurf.no-primitive"           // Windsurf has no subagent primitive
	CodeHookClaudeUnsupportedType           = "hook.claude.unsupported-type"             // Claude hook with unsupported type (http/prompt/agent)
	CodeHookFactoryNonCommandTypeDropped    = "hook.factory.non-command-type-dropped"    // Factory dropped non-command hook type
	CodeHookCursorUnsupportedType           = "hook.cursor.unsupported-type"             // Cursor dropped non-command hook type
	CodeHookWindsurfEnvTokenExpanded        = "hook.windsurf.env-token-expanded"         // Windsurf expanded env token to literal path
	CodeHookWindsurfEventUnmappable         = "hook.windsurf.event-unmappable"           // Claude hook event has no Windsurf equivalent
	CodeIngestGeminiHookEventPartialMapping = "ingest.gemini.hook-event-partial-mapping" // Gemini hook event only partially maps to Claude vocabulary
	CodeIngestWindsurfHookEventUnmappable   = "ingest.windsurf.hook-event-unmappable"    // Windsurf hook event has no canonical equivalent
	CodeIngestFactoryResourceShadowed       = "ingest.factory.resource-shadowed"
	// CodeMcpInterpolationUnsupported is the shared, VALUE-LEVEL interpolation
	// loss: one env value, one header value, one argument, or one OAuth member
	// used an interpolation style the target cannot represent, so that value
	// was omitted and the surrounding entry was still emitted. It stays a
	// warning for exactly the reason the other emit-the-entry codes do
	// (bead agent-marketplace-5exj.23).
	//
	// The case where the unrepresentable value is the field the server's form
	// cannot do without — command or url — removes the whole entry and is a
	// DIFFERENT outcome, so it has its own error code
	// (CodeMcpDevinInterpolationUnrepresentable) rather than a second severity
	// for this one.
	CodeMcpInterpolationUnsupported    = "mcp.interpolation-unsupported"           // A single MCP value used an interpolation style the target cannot represent
	CodeRuleClaudeNoPrimitive          = "rule.claude.no-primitive"                // Claude has no rules directory primitive
	CodeRuleGeminiNoPrimitive          = "rule.gemini.no-primitive"                // Gemini has no rules directory primitive
	CodeRuleDevinUnsupported           = "rule.devin.unsupported"                  // Rules are out of scope for the Devin target (a scope decision, not a Devin limit)
	CodeRuleCodexUnsupported           = "rule.codex.unsupported"                  // Codex rules and AGENTS.md generation are out of scope
	CodeCodexFieldDropped              = "compile.codex.field-dropped"             // Benign IR field has no documented Codex equivalent
	CodeSubagentCodexModelDropped      = "subagent.codex.model-dropped"            // Codex-only model omitted for a non-Codex target
	CodeSubagentCodexEffortDropped     = "subagent.codex.effort-dropped"           // Codex-only reasoning effort omitted for a non-Codex target
	CodeHookCodexPluginRootUnresolved  = "hook.codex.plugin-root-unresolved"       // Command carries ${CLAUDE_PLUGIN_ROOT} but no plugin root was configured
	CodeMcpCodexPluginRootUnresolved   = "mcp.codex.plugin-root-unresolved"        // Value carries ${CLAUDE_PLUGIN_ROOT} but no plugin root was configured
	CodeIngestCodexPromptDeprecated    = "ingest.codex.prompt-deprecated"          // Deprecated prompt ingested as command
	CodeIngestCodexFieldDropped        = "ingest.codex.field-dropped"              // Benign native Codex field omitted
	CodeRuleFactoryMetadataDropped     = "rule.factory.metadata-dropped"           // Factory rules are plain markdown; globs/alwaysApply dropped
	CodeCanonicalFieldOverridesExtra   = "compile.canonical-field-overrides-extra" // Canonical field wins over conflicting preserved extra
	CodeHookPromptUnsupported          = "hook.prompt.unsupported"                 // Target cannot represent prompt hooks
	CodeHookGeminiEventUnmappable      = "hook.gemini.event-unmappable"            // Gemini has no equivalent event
	CodeHookGeminiUnsupportedType      = "hook.gemini.unsupported-type"            // Gemini cannot represent hook type
	CodeHookWindsurfUnsupportedType    = "hook.windsurf.unsupported-type"          // Windsurf cannot represent hook type
	CodeHookWindsurfTimeoutUnsupported = "hook.windsurf.timeout-unsupported"
	CodeSkillSupportFilesDeferred      = "skill.support-files-deferred"   // Support file bytes are unavailable to this layer
	CodeMcpLiteralCredential           = "mcp.credentials.literal-secret" // Literal credential-like value detected

	CodeSkillDevinModelUnmappable    = "skill.devin.model-unmappable"    // Explicit skill model is not a stable Devin alias
	CodeSkillDevinTriggerUnmappable  = "skill.devin.trigger-unmappable"  // Trigger is outside Devin's user/model vocabulary
	CodeSubagentDevinModelUnmappable = "subagent.devin.model-unmappable" // Explicit model is not a stable Devin alias
	CodeSubagentDevinModelDefault    = "subagent.devin.model-default"    // Empty model uses Devin's default subagent model
	CodeSubagentDevinEffortDropped   = "subagent.devin.effort-dropped"   // Devin has no effort field

	// Devin allowed-tools codes. These two were ERRORS that suppressed the
	// artifact until bead agent-marketplace-pu6b moved them here. The reversal
	// is deliberate and the reasoning is recorded rather than deleted, because
	// the invariant it amends is stated a few lines below (see the Devin INGEST
	// block) and in the error block's Devin family.
	//
	// What they now mean: an allowed-tools entry Devin cannot express — a scoped
	// Claude specifier such as `Bash(openspec:*)`, a name outside the closed
	// read/edit/grep/glob/exec vocabulary, or a value that is not a list shape
	// at all — causes the `allowed-tools` KEY TO BE OMITTED from an artifact that
	// is still published. Explicitly empty and fully mappable restrictions are
	// unchanged.
	//
	// Omitting the key inherits Devin's default tool set, so this IS a real
	// widening relative to the source's stated restriction. It is accepted for
	// three reasons:
	//
	//   - The restriction at issue is one Devin cannot REPRESENT, so no emitted
	//     artifact can carry it. State this precisely, because the evidence does
	//     not support the shorter "Devin ignores allowed-tools": Devin DOES
	//     enforce a list it can represent (CONTROL E on 3000.3.27 verified on
	//     disk that `allowed-tools: [read, grep]` blocked both edit and exec),
	//     but it has five bare names and no per-command scoping, and any other
	//     name is SILENTLY DROPPED rather than rejected (D12,
	//     TestProbeAllowedToolVocabulary). Every available output is therefore
	//     an unfaithful representation; the question is only which one.
	//   - The cost of the error fell on people who could not pay it. Scoped
	//     `Bash(...)` is what the openspec skill generator emits, so three
	//     skills selected by this repo's own committed .agents/config.yaml
	//     produced three errors, a non-zero `sync`, and nothing installed. The
	//     user's only fix was editing a registry they may not own, and the
	//     non-zero exit also gated off unrelated `--prune-stale` cleanup.
	//   - The narrowest representable value is worse. `allowed-tools: []` never
	//     broadens, but it means the resource may use NO tools, so an openspec
	//     skill would install and then be unable to run `openspec`. A loud
	//     absence beats a silently inert install.
	//
	// Factory now uses the same warning policy for subagent.tool-unknown: its
	// droid format gives an omitted `tools` key a documented default meaning.
	// Gemini and Cursor still fail closed because their unknown-tool diagnostics
	// suppress the artifact.
	//
	// Neither message, hint, nor slog attribute quotes the offending specifier:
	// allowed-tools entries carry command strings such as
	// `Bash(git push --force:*)`. Only the resource name and source location
	// localize the loss.
	CodeSkillDevinToolUnknown      = "skill.devin.tool-unknown"      // Devin skill/command allowed tool not representable; allowed-tools key omitted
	CodeSubagentDevinToolUnknown   = "subagent.devin.tool-unknown"   // Devin subagent tool not representable; allowed-tools key omitted
	CodeSubagentFactoryToolUnknown = "subagent.factory.tool-unknown" // Factory subagent tool not representable; artifact suppressed

	// Devin hook and MCP codes that still EMIT the entry.
	//
	// These two families are split by OUTCOME, not by kind: the codes here
	// describe a loss inside an entry that is still written, and they are
	// warnings. The codes that remove an entry are errors and live in the error
	// block below (bead agent-marketplace-5exj.20).
	//
	// hook.devin.payload-dropped drops the payload the hook TYPE does not
	// select — Devin would never have run it — and mcp.devin.field-dropped
	// drops a modeled field belonging to the documented server form this server
	// did not select. Both plugin-root codes leave a ${CLAUDE_PLUGIN_ROOT}
	// token unexpanded in a value that is otherwise emitted verbatim.
	//
	// Escalating any of these four would suppress an entry that is otherwise
	// exactly what the source authorized, which contradicts the per-resource
	// suppression contract: an error costs the whole resource's output, and
	// "the field I dropped was inert" is not a reason to withhold a working
	// hook or server. None of them can widen anything either — a dropped field
	// and an unexpanded token can only ever give the runtime LESS than the
	// source asked for.
	//
	// AMENDED by bead agent-marketplace-pu6b. This block used to read as the
	// general rule "a loss that broadens privilege is an error, a loss that
	// narrows or is inert is a warning", and that reading is now WRONG for two
	// codes. skill.devin.tool-unknown and subagent.devin.tool-unknown omit the
	// `allowed-tools` key, which DOES widen the emitted restriction to Devin's
	// default tool set, and they are warnings anyway. The honest rule is
	// narrower than "never widen":
	//
	//   A loss is an error when the emitted output would MISREPRESENT the target's
	//   own behaviour — a hook that runs something other than what the source
	//   asked for, a server contacted without authorization, a subagent profile
	//   advertising a sandbox or denylist Devin has no field for, a profile
	//   silently shadowed by a built-in. It is a warning when the emitted output
	//   is a TRUTHFUL, if lossy, statement about what the target will do, and
	//   suppressing the resource would therefore cost the user the resource
	//   without buying them a correct one.
	//
	// Every error code in the block below still satisfies the first clause. The
	// allowed-tools pair falls under the second: an artifact with no
	// allowed-tools key honestly says "Devin's default tool set", whereas Devin
	// cannot be made to say the source's actual restriction at all — it has five
	// bare names, no per-command scoping, and silently drops anything else (D12,
	// 3000.3.27). Do not read the second clause as "widening is fine"; it is
	// specifically about a field whose absence has a documented meaning on the
	// target.
	CodeHookDevinPayloadDropped       = "hook.devin.payload-dropped"        // The payload the hook type does not select was dropped
	CodeHookDevinPluginRootUnresolved = "hook.devin.plugin-root-unresolved" // Command carries ${CLAUDE_PLUGIN_ROOT} but no plugin root was configured
	CodeMcpDevinFieldDropped          = "mcp.devin.field-dropped"           // Modeled field belongs to the other documented server form
	CodeMcpDevinPluginRootUnresolved  = "mcp.devin.plugin-root-unresolved"  // Value carries ${CLAUDE_PLUGIN_ROOT} but no plugin root was configured

	// Devin INGEST codes (work package D8). Every one of them is a warning
	// because it describes something the source layout expressed that canonical
	// IR cannot hold, and dropping it can only NARROW what a later compile
	// emits. The one ingest situation that can change a privilege decision —
	// an allowed-tools entry outside Devin's closed vocabulary — is an ERROR
	// (ingest.devin.tool-unreversible) and skips the resource, because keeping
	// it would broaden or silently rewrite an explicit tool restriction.
	//
	// That ingest error is deliberately NOT reversed alongside its compile-side
	// counterparts (bead agent-marketplace-pu6b), and the asymmetry is the
	// point: ingestion writes the harness-neutral IR that all six compilers
	// then read, so a rewritten restriction there propagates to targets that DO
	// enforce it. Compiling to Devin writes only Devin's own artifact, where the
	// restriction is advisory. Same field, different blast radius.
	//
	// No message, hint, or slog attribute for any of these codes carries a
	// command string, a prompt body, a matcher value, a timeout value, an env
	// value, a header value, an OAuth client id/secret/resource, a permission
	// value, or a resource body. Only FIELD NAMES, the fixed event/tool
	// vocabulary, the resource name, and the source location are reported.
	CodeIngestDevinHookEventUnmappable          = "ingest.devin.hook-event-unmappable"           // Devin hook event is outside the documented vocabulary; name preserved
	CodeIngestDevinModelNotCanonical            = "ingest.devin.model-not-canonical"             // Devin model alias has no canonical equivalent; value preserved verbatim
	CodeIngestDevinCommandFieldDropped          = "ingest.devin.command-field-dropped"           // Command-provenance skill carried a field ir.Command cannot hold
	CodeIngestDevinReservedFieldDropped         = "ingest.devin.reserved-field-dropped"          // A field Devin does not read was dropped instead of preserved as an extra
	CodeIngestDevinSubagentDirectoryFormSkipped = "ingest.devin.subagent-directory-form-skipped" // agents/<name>/AGENT.md is not ingested by this target
	CodeIngestDevinLegacyMcpLocationIgnored     = "ingest.devin.legacy-mcp-location-ignored"     // Pre-3000.3 config.json#mcpServers was found and not read

	// Shared-aggregate OWNERSHIP codes (work package D9).
	//
	// These are emitted by the consumer-side durable ownership layer
	// (internal/ownership) rather than by a compiler or ingester, because
	// ownership state, journals, and safety backups are distribution concerns
	// this library deliberately does not own. They live in this catalog anyway:
	// codes.md is the ONE stable diagnostic vocabulary that consumer UIs
	// (`status`, `doctor`, the TUI) key off, and an ownership failure has to be
	// machine-readable in exactly the same way a compile failure is.
	//
	// No message, hint, or slog attribute for any of these codes carries a
	// hook payload, matcher, MCP env value, header value, OAuth secret, command
	// string, or aggregate file content. Only the output-relative file path, the
	// non-secret entry identity (an MCP server object key or a hook event name),
	// content DIGESTS, and generation numbers are reported.
	CodeOwnershipStateMissing = "ownership.state-missing" // No durable ownership record; every existing aggregate entry is unmanaged
	CodeOwnershipEntryDrift   = "ownership.entry-drift"   // A proven owned entry was edited by hand and is replaced from marketplace configuration
	CodeOwnershipUnmanagedKey = "ownership.unmanaged-key" // A desired aggregate key already exists unmanaged; only the generated entry fails
	CodeOwnershipRecovered    = "ownership.recovered"     // An incomplete journaled generation was rolled forward or rolled back on startup

	// CodeOwnershipLockUnavailable is the DEGRADED half of per-root
	// serialization: the platform or the filesystem refuses advisory locks
	// (ENOTSUP/EOPNOTSUPP, realistically an NFS-mounted state root, or a build
	// for a non-unix GOOS), so ownership mutations for this root proceed exactly
	// as they did before locking existed. ENOLCK is retried like contention and
	// only degrades here if it persists to the lock timeout. That is byte-for-
	// byte today's behaviour on the only filesystems where today's behaviour is
	// unavoidable, so it is a warning rather than an error — but it is reported,
	// because a silent loss of serialization is how a corruption becomes
	// unexplainable.
	//
	// There is deliberately NO ownership.lock-stale-reclaimed code, and adding
	// one would be a design error rather than an omission. A flock(2) lock is
	// owned by the open file description, so the kernel drops it on Close, exit,
	// panic, SIGKILL, OOM-kill, and container teardown: a stale ownership lock
	// is not an event that can happen, so there is nothing to reclaim and
	// nothing to report. The reasoning, and why a PID check, a timestamp
	// threshold, and a break protocol are each individually wrong here, is in
	// internal/ownership/lock.go and in
	// docs/plans/2026-08-06-ownership-locking-design.md section 4.
	CodeOwnershipLockUnavailable = "ownership.lock-unavailable" // Advisory locking is unavailable here, so ownership mutations for this root are not serialized

	// Owned aggregate MATERIALIZATION codes. Emitted by internal/materialize's
	// target adapters over the shared reconciler. Like the ownership
	// codes they are consumer-side, and they carry only the output-relative
	// aggregate path, the non-secret entry identity (an MCP server object key or
	// a hook event name), and resource names.
	CodeMaterializeDevinHookSatisfiedByUnmanaged = "materialize.devin.hook-satisfied-by-unmanaged" // A semantically identical handwritten hook already exists; no entry was added
	CodeMaterializeDevinAggregatePrunePostponed  = "materialize.devin.aggregate-prune-postponed"   // A resource was skipped this run, so stale owned entries were not pruned
	CodeMaterializeCodexHookSatisfiedByUnmanaged = "materialize.codex.hook-satisfied-by-unmanaged" // A semantically identical handwritten hook already exists; no entry was added
	CodeMaterializeCodexAggregatePrunePostponed  = "materialize.codex.aggregate-prune-postponed"   // A resource was skipped this run, so stale owned entries were not pruned
	CodeMaterializeCodexAggregateMalformed       = "materialize.codex.aggregate-malformed"         // A Codex aggregate could not be parsed or safely reconciled
	CodeMaterializeCodexAggregateUnpatchable     = "materialize.codex.aggregate-unpatchable"       // A Codex aggregate entry uses a native form that cannot be safely patched
	CodeMaterializeDestinationOverlap            = "materialize.destination.overlap"               // Resolved Codex destinations overlap in a way that cannot be safely coalesced
	CodeMaterializeCodexTrustReview              = "materialize.codex.trust-review"                // Repository Codex hooks or MCP configuration changed and may require trust review
	CodeMaterializeCodexTrustReviewPending       = "materialize.codex.trust-review-pending"        // A check found a pending repository Codex change without mutating files or trust state

	// Devin DOCTOR warning codes (work package D11). Both are warnings and
	// NEITHER blocks anything: "Sync does not require a Devin binary. `doctor`
	// detects an installed binary and warns when its version is below 3000.3."
	CodeDoctorDevinVersionUnsupported = "doctor.devin.version-unsupported" // The installed Devin CLI is older than the 3000.3 compatibility floor
	CodeDoctorDevinVersionUnknown     = "doctor.devin.version-unknown"     // A Devin CLI is installed but its version could not be established

	// Devin REPAIR warning code: the THIRD repair class. An existing unmanaged
	// aggregate entry has the same key as a desired entry but a DIFFERENT value,
	// so it is neither an exact adoption match nor an evidence-backed orphan.
	// Without this class the entry is reported as a collision by every sync and
	// repairable by nothing, which is a state the product cannot get out of.
	//
	// It is a WARNING because it describes an offer, not a failure, and because
	// acting on it is optional: the existing entry is preserved until the user
	// confirms a replacement interactively. It never carries either value —
	// only the entry kind, the non-secret identity, the resource name, the
	// aggregate path, and the two value DIGESTS.
	CodeDoctorRepairConflictReplaceable = "doctor.repair.conflict-replaceable" // An unmanaged entry has a desired key with a different value; replacing it needs interactive confirmation
)

// Error codes.
const (
	CodeSkillCodexSecurityRestrictionUnsupported = "skill.codex.security-restriction-unsupported" // Codex skill cannot enforce an explicit restriction
	CodeHookEventUnmappable                      = "hook.event-unmappable"                        // Target cannot preserve this hook event
	CodeHookNativeLiteralUnsupported             = "hook.native-literal-unsupported"              // Native Codex hook literal would be reinterpreted
	CodeHookCodexNativeFieldsUnsupported         = "hook.codex.native-fields-unsupported"         // Target cannot preserve native Codex hook fields
	CodeHookCodexUnsupported                     = "hook.codex.unsupported"                       // Codex does not execute this hook event or handler
	CodeHookWindsurfNativeFieldsUnsupported      = "hook.windsurf.native-fields-unsupported"
	CodeMcpSecurityRestrictionUnsupported        = "mcp.security-restriction-unsupported"       // Target cannot preserve native MCP authority or literal semantics
	CodeMcpCodexSecurityUnsupported              = "mcp.codex.security-unsupported"             // Codex MCP cannot preserve authentication or transport semantics
	CodeIngestCodexLoadResourceFailed            = "ingest.codex.load-resource-failed"          // One malformed Codex resource was skipped
	CodeIngestCodexDuplicateIdentity             = "ingest.codex.duplicate-identity"            // Duplicate native identity suppressed
	CodeIngestCodexHookUnsupported               = "ingest.codex.hook-unsupported"              // Unsupported native hook skipped
	CodeIngestCodexMcpSecurityUnsupported        = "ingest.codex.mcp-security-unsupported"      // Security-sensitive native MCP fields cannot be represented
	CodeIngestCodexSubagentSecurityUnsupported   = "ingest.codex.subagent-security-unsupported" // Authority-bearing native subagent fields cannot be represented
	CodeMaterializeCodexDuplicateIdentity        = "materialize.codex.duplicate-identity"       // Duplicate desired Codex aggregate identity suppressed
	CodeMaterializeHookSupportConflict           = "materialize.hook.support-conflict"
	CodeSubagentFactoryDisallowedToolsDropped    = "subagent.factory.disallowed-tools-dropped" // Factory cannot represent disallowedTools
	CodeSubagentFactoryIsolationDropped          = "subagent.factory.isolation-dropped"        // Factory cannot represent isolation
	CodeSubagentGeminiToolUnknown                = "subagent.gemini.tool-unknown"              // Gemini tool not in mapping table; artifact suppressed
	CodeSubagentCursorToolUnknown                = "subagent.cursor.tool-unknown"              // Cursor tool not in mapping table; artifact suppressed
	CodeSubagentSecurityRestrictionUnsupported   = "subagent.security-restriction-unsupported" // Unsupported security restriction suppresses artifact
	CodeBundleConflictDuplicateArtifactPath      = "bundle.conflict.duplicate-artifact-path"   // Two resources produce the same artifact path
	CodeBundleConflictCrossKindArtifactPath      = "bundle.conflict.cross-kind-artifact-path"  // Two resources of different kinds produce the same artifact path
	CodeBundleCompileResourceFailed              = "bundle.compile.resource-failed"            // A resource compiler returned an error
	CodeIngestClaudeLoadSkillFailed              = "ingest.claude.load-skill-failed"           // Failed to load a skill from the source layout
	CodeIngestClaudeLoadCommandFailed            = "ingest.claude.load-command-failed"         // Failed to load a command from the source layout
	CodeIngestClaudeLoadSubagentFailed           = "ingest.claude.load-subagent-failed"        // Failed to load a subagent from the source layout
	CodeIngestFactoryLoadSkillFailed             = "ingest.factory.load-skill-failed"          // Failed to load a skill from the source layout
	CodeIngestFactoryLoadCommandFailed           = "ingest.factory.load-command-failed"        // Failed to load a command from the source layout
	CodeIngestFactoryLoadSubagentFailed          = "ingest.factory.load-subagent-failed"       // Failed to load a subagent from the source layout
	CodeIngestFactoryLoadMcpServersFailed        = "ingest.factory.load-mcp-servers-failed"    // Failed to load MCP servers from the source layout
	CodeIngestCursorLoadSkillFailed              = "ingest.cursor.load-skill-failed"           // Failed to load a skill from the source layout
	CodeIngestCursorLoadCommandFailed            = "ingest.cursor.load-command-failed"         // Failed to load a command from the source layout
	CodeIngestCursorLoadSubagentFailed           = "ingest.cursor.load-subagent-failed"        // Failed to load a subagent from the source layout
	CodeIngestGeminiLoadSkillFailed              = "ingest.gemini.load-skill-failed"           // Failed to load a skill from the source layout
	CodeIngestGeminiLoadCommandFailed            = "ingest.gemini.load-command-failed"         // Failed to load a command from the source layout
	CodeIngestGeminiLoadSubagentFailed           = "ingest.gemini.load-subagent-failed"        // Failed to load a subagent from the source layout
	CodeIngestGeminiLoadHookFailed               = "ingest.gemini.load-hook-failed"            // Failed to load a hook from the source layout
	CodeIngestFactoryLoadRuleFailed              = "ingest.factory.load-rule-failed"           // Failed to load a rule from the source layout
	CodeIngestCursorLoadRuleFailed               = "ingest.cursor.load-rule-failed"            // Failed to load a rule from the source layout
	// skill.devin.tool-unknown, subagent.devin.tool-unknown, and
	// subagent.factory.tool-unknown are warnings and live in the warning block
	// above, which carries the rationale. Gemini and Cursor siblings still fail
	// closed.
	CodeSubagentDevinBuiltinNameConflict   = "subagent.devin.builtin-name-conflict"   // Subagent name collides with a Devin built-in subagent
	CodeSubagentDevinNestedNameUnsupported = "subagent.devin.nested-name-unsupported" // Subagent name expresses nesting the flat Devin layout cannot represent

	// Devin hook and MCP ENTRY-SKIP codes.
	//
	// Every code in this group removes the desired aggregate entry, and each
	// one is an ERROR because the promoted plan's error-behavior section asks
	// for both halves of one sentence: "Within valid aggregate JSON, invalid or
	// conflicting desired entries are skipped while other valid entries are
	// patched atomically; sync still exits non-zero." Warning severity
	// delivered the first half only — a sync that silently dropped a hook
	// exited 0 — so the operator-visible signal was missing.
	//
	// D6 and D7 originally recorded these as warnings for a blast-radius reason
	// that no longer exists. Two beads removed it, in this order:
	//
	//   - agent-marketplace-5exj.17 made compile-side suppression PER RESOURCE.
	//     An error now costs exactly the offending hook's or server's fragment;
	//     sibling resources still compile and are still published.
	//   - agent-marketplace-5exj.20 (this change) narrowed consumer-side prune
	//     suppression before flipping the severities. internal/materialize's
	//     `blocked` map is now keyed by (output key, output COMPONENT) instead
	//     of by output key alone, and Devin hooks and MCP servers write to
	//     shared aggregate FILES rather than to a component directory, so an
	//     erroring hook suppresses pruning of hooks.v1.json alone (through
	//     devinAggregateWork.incomplete) and leaves skills/ and agents/ pruning
	//     untouched.
	//
	// The blast radius of one of these errors is therefore: one aggregate
	// entry not written, one aggregate file not pruned, one non-zero exit.
	//
	// The fail-closed guarantee is unchanged and is the reason skipping is
	// right in the first place: a hook that hits one of these codes produces NO
	// artifact, so Devin never runs something other than what the source asked
	// for; an MCP server that hits one produces no entry, so Devin never
	// launches a process or contacts an endpoint the source did not authorize.
	//
	// The four sibling codes that still EMIT their entry stay warnings; see the
	// warning block above for why escalating them would be wrong.
	CodeHookDevinEventUnmappable         = "hook.devin.event-unmappable"          // Hook event is not documented by Devin; entry skipped
	CodeHookDevinUnsupportedType         = "hook.devin.unsupported-type"          // Hook type is not supported by Devin; entry skipped
	CodeHookDevinMatcherUnsupportedEvent = "hook.devin.matcher-unsupported-event" // Matcher on an event Devin does not document matchers for; entry skipped
	CodeHookDevinPayloadMissing          = "hook.devin.payload-missing"           // Hook type selects a payload the resource does not carry; entry skipped
	CodeHookDevinTimeoutInvalid          = "hook.devin.timeout-invalid"           // Hook timeout is negative; entry skipped
	CodeMcpDevinTransportAmbiguous       = "mcp.devin.transport-ambiguous"        // Server declares both a stdio command and a remote url; entry skipped
	CodeMcpDevinTransportIncomplete      = "mcp.devin.transport-incomplete"       // Server declares neither a stdio command nor a remote url; entry skipped
	CodeMcpDevinTransportUnsupported     = "mcp.devin.transport-unsupported"      // Explicit remote transport is outside Devin's documented vocabulary; entry skipped
	// CodeMcpDevinInterpolationUnrepresentable is the same outcome reached
	// through interpolation rather than through the form checks: the value in
	// the one field the selected server form cannot do without — `command` on
	// a stdio server, `url` on a remote server — uses an interpolation style
	// Devin has no syntax for, so the value is omitted and the whole entry
	// disappears with it.
	//
	// It exists (bead agent-marketplace-5exj.23) because the shared
	// mcp.interpolation-unsupported code was being used for two different
	// outcomes at once: omitting one env value from an entry that is still
	// written, and removing the entry entirely. Severity follows the OUTCOME,
	// so two outcomes need two codes; giving one code two severities would
	// break the guarantee that a catalogued code means one thing. The shared
	// code keeps the value-level meaning and its warning severity.
	CodeMcpDevinInterpolationUnrepresentable = "mcp.devin.interpolation-unrepresentable" // The command or url of the selected server form is unrepresentable; entry skipped

	CodeIngestDevinLoadSkillFailed      = "ingest.devin.load-skill-failed"       // Failed to load a skill directory from the source layout
	CodeIngestDevinLoadSubagentFailed   = "ingest.devin.load-subagent-failed"    // Failed to load a flat subagent profile from the source layout
	CodeIngestDevinLoadHookFailed       = "ingest.devin.load-hook-failed"        // Failed to load hooks from the source layout
	CodeIngestDevinLoadMcpServersFailed = "ingest.devin.load-mcp-servers-failed" // Failed to load MCP servers from the source layout
	CodeIngestDevinToolUnreversible     = "ingest.devin.tool-unreversible"       // allowed-tools entry is outside Devin's closed vocabulary; resource skipped
	CodeJSONDuplicateKey                = "json.duplicate-key"                   // JSON object contains the same key twice
	CodeJSONMalformed                   = "json.malformed"                       // JSON document is not well-formed

	// Shared-aggregate OWNERSHIP error codes (work package D9). See the note on
	// the ownership warning codes above for why they live in this catalog and
	// what they are forbidden from carrying.
	CodeOwnershipStateCorrupt      = "ownership.state-corrupt"       // The durable ownership record could not be read or failed validation
	CodeOwnershipStateRootMismatch = "ownership.state-root-mismatch" // A record was found under this root key for a different canonical root
	CodeOwnershipStateUnsafeValue  = "ownership.state-unsafe-value"  // Refused to record a value that is not a non-secret identity
	CodeOwnershipSafetyBackup      = "ownership.safety-backup"       // The mandatory raw-byte safety backup failed; the mutation was refused
	CodeOwnershipForeignChange     = "ownership.foreign-change"      // The aggregate file changed underneath a staged generation; the generation was abandoned

	// CodeOwnershipGenerationConflict is the live compare-and-swap refusal: the
	// aggregate file or the durable ownership generation changed after this run
	// read it, so the mutation was refused and the file was left as found. On a
	// filesystem without advisory locking this precondition is the last line of
	// defense that keeps the unserialized path safe.
	CodeOwnershipGenerationConflict = "ownership.generation-conflict" // The aggregate or ownership generation changed underneath a mutation; the file was left as found

	// CodeOwnershipLockTimeout is an ERROR because the outcome is that a target
	// root was NOT reconciled: another process still held its ownership lock at
	// the deadline, so this run wrote nothing for that root and left its
	// aggregates exactly as found. Per-root failure isolation applies — other
	// roots and other targets continue — but the run exits non-zero, because a
	// legitimate critical section is milliseconds long and an expiry is
	// therefore always a real anomaly.
	CodeOwnershipLockTimeout = "ownership.lock-timeout" // The root's ownership lock could not be acquired before the timeout; its aggregates were left as found

	// Devin MATERIALIZATION error codes (work package D10).
	CodeMaterializeDevinAggregateMalformed = "materialize.devin.aggregate-malformed" // The destination aggregate JSON could not be parsed; that file was left unchanged
	CodeMaterializeDevinAggregateShape     = "materialize.devin.aggregate-shape"     // The destination aggregate has a shape this target cannot patch; that file was left unchanged
	CodeMaterializeDevinAggregateWrite     = "materialize.devin.aggregate-write"     // The journaled aggregate mutation failed; that file was left unchanged
	CodeMaterializeDevinSymlinkRequired    = "materialize.devin.symlink-required"    // Retired: retained for compatibility; all targets now support generic file-copy materialization

	// Devin REPAIR error code (work package D11). `doctor --repair` exits 1 on
	// "corrupt state/file or partial repair failure"; this is the code for the
	// partial-repair half of that rule. A refused or declined action is NOT an
	// error: "exit 0 means clean, repaired, or declined without operational
	// failure".
	CodeDoctorRepairFailed = "doctor.repair.failed" // An authorized repair action failed; the aggregate was left exactly as found
)

// CodeDoc provides structured documentation for a diagnostic code.
type CodeDoc struct {
	Code     string
	Severity Severity
	Summary  string
	Hint     string
}

// codeCatalog is the single source of truth for all v0 diagnostic codes.
var codeCatalog = []CodeDoc{
	// Info
	{CodeSubagentCursorModelPassthrough, SeverityInfo, "Cursor model not in mapping table; passed through unchanged", "Add the model to the mapping table if a Cursor-specific ID exists"},
	{CodeIngestClaudeManifestSynthesized, SeverityInfo, "Workspace form has no plugin.json; manifest synthesized", "Add a .claude-plugin/plugin.json to the source if a manifest is needed"},
	{CodeIngestFactoryManifestSynthesized, SeverityInfo, "Workspace form has no plugin.json; manifest synthesized", "Add a .factory-plugin/plugin.json to the source if a manifest is needed"},
	{CodeIngestFactoryReasoningEffortMappedToModel, SeverityInfo, "Factory reasoningEffort preserved while model was canonicalized", "Compile back to Factory to restore the concrete Factory model identifier"},
	{CodeIngestCursorManifestSynthesized, SeverityInfo, "Workspace form has no plugin.json; manifest synthesized", "Add a .cursor-plugin/plugin.json to the source if a manifest is needed"},
	{CodeIngestGeminiManifestSynthesized, SeverityInfo, "Workspace form has no gemini-extension.json; manifest synthesized", "Add gemini-extension.json to the source if bundle metadata is needed"},
	{CodeIngestGeminiTOMLCommandCanonicalized, SeverityInfo, "TOML command metadata normalized into canonical IR", "The command was successfully canonicalized from TOML format"},
	{CodeIngestWindsurfManifestSynthesized, SeverityInfo, "Windsurf has no plugin manifest; manifest synthesized", "Windsurf bundles are synthesized from the workspace layout during ingestion"},
	{CodeIngestWindsurfWorkflowAssumedCommand, SeverityInfo, "Windsurf workflow was ingested as a command", "Workflows map to ir.Command because Windsurf already has a native skill primitive"},
	{CodeIngestWindsurfLiteralPathPreserved, SeverityInfo, "Literal Windsurf path preserved during MCP ingestion", "Literal path values remain unchanged because there is no Claude-side canonical token for them"},
	{CodeIngestWindsurfMcpEnvNormalized, SeverityInfo, "Windsurf MCP env interpolation normalized to canonical form", "Windsurf ${env:VAR} was converted to canonical ${VAR} during ingestion so the IR is harness-neutral"},
	{CodeIngestWindsurfSubagentViaAgentsMDSkipped, SeverityInfo, "AGENTS.md was not synthesized into a Windsurf subagent", "Windsurf has no subagent primitive; AGENTS.md remains external project guidance"},
	{CodeIngestDevinManifestSynthesized, SeverityInfo, "Devin has no bundle manifest file; manifest synthesized", "Devin roots carry no plugin manifest; bundle metadata is synthesized from the layout during ingestion"},
	{CodeIngestDevinLocalOverrideIgnored, SeverityInfo, "A Devin project-local personal override file was found and not ingested", "Agent Marketplace neither reads nor writes .devin/config.local.json or .devin/mcp_config.local.json; move shared configuration into the non-local file"},
	{CodeIngestDevinMcpEnvNormalized, SeverityInfo, "Devin MCP env interpolation normalized to canonical form", "Devin ${env:VAR} was converted to canonical ${VAR} during ingestion so the IR is harness-neutral; compiling back to Devin restores ${env:VAR}"},
	{CodeIngestCodexManifestSynthesized, SeverityInfo, "Codex workspace has no bundle manifest; manifest synthesized", "Codex bundle metadata is synthesized from the native layout"},
	{CodeDoctorDevinRecommended, SeverityInfo, "A Devin CLI is installed but no harness list selects the devin target", "Select devin in the TUI harness modal (H) or add it to a harnesses list"},
	{CodeOwnershipLockContended, SeverityInfo, "Another agent-marketplace process held this target root's ownership lock, so this run waited for it", "No action needed; ownership mutations are serialized per canonical target root so two writers cannot publish an aggregate file and its ownership record at different instants"},

	// Warning
	{CodeIngestFactoryResourceShadowed, SeverityWarning, "Factory resource was shadowed by an earlier resource of the same kind and name", "Use distinct resource names or remove the lower-priority definition"},
	{CodeSubagentWindsurfNoPrimitive, SeverityWarning, "Windsurf has no subagent primitive; resource dropped", "Consider expressing the subagent as a Windsurf workflow instead"},
	{CodeHookClaudeUnsupportedType, SeverityWarning, "Unsupported hook type in Claude format; only command-type is universally supported in v0", "Convert the hook to a command type or remove it"},
	{CodeHookFactoryNonCommandTypeDropped, SeverityWarning, "Factory only supports command-type hooks; non-command type dropped", "Convert the hook to a command type or remove it"},
	{CodeHookCursorUnsupportedType, SeverityWarning, "Cursor only supports command-type hooks; non-command type dropped", "Convert the hook to a command type or remove it"},
	{CodeHookWindsurfEnvTokenExpanded, SeverityWarning, "Windsurf expanded env token to a literal path", "Verify the expanded path is correct for the target environment"},
	{CodeHookWindsurfEventUnmappable, SeverityWarning, "Claude hook event has no Windsurf equivalent; hook dropped", "Consider using a mappable event (PreToolUse with a known tool matcher, UserPromptSubmit, or Stop)"},
	{CodeIngestGeminiHookEventPartialMapping, SeverityWarning, "Gemini hook event only partially maps to Claude vocabulary", "The Gemini event was preserved but cannot be fully represented in canonical IR"},
	{CodeIngestWindsurfHookEventUnmappable, SeverityWarning, "Windsurf hook event has no Claude equivalent; hook skipped", "Use a mappable Windsurf event or accept that the hook cannot round-trip through Claude IR"},
	{CodeMcpInterpolationUnsupported, SeverityWarning, "One MCP value uses an interpolation style the target cannot represent; that value was omitted and the entry was still emitted", "Replace the interpolated value with a literal value, or use an interpolation style the target documents"},
	{CodeRuleClaudeNoPrimitive, SeverityWarning, "Claude Code has no rules directory primitive; rule dropped", "Claude reads project guidance from CLAUDE.md; reference the rule content there instead"},
	{CodeRuleGeminiNoPrimitive, SeverityWarning, "Gemini CLI has no rules directory primitive; rule dropped", "Gemini reads project guidance from GEMINI.md; reference the rule content there instead"},
	// rule.devin.unsupported sits with its Claude and Gemini siblings on
	// purpose. It is the same class of loss — the run drops a rule and
	// continues — but the reason is different and must not be conflated: this
	// is a SCOPE decision, not a Devin capability limit. On 3000.3.27 a rule at
	// .devin/rules/<name>.md IS discovered even though `devin rules paths` does
	// not advertise it
	// (internal/devinprobe.TestProbeRulesDiscoveryUnderDevinRoots).
	{CodeRuleDevinUnsupported, SeverityWarning, "Rules are out of scope for the Devin target; no rule artifact was emitted", "This is a scope decision, not a Devin limitation: on 3000.3.27 a rule placed at .devin/rules/<name>.md is discovered even though `devin rules paths` does not advertise it. Compile the rule to a target with a supported rules primitive (factory, cursor, windsurf), or place it under .devin/rules by hand"},
	{CodeRuleCodexUnsupported, SeverityWarning, "Rules are out of scope for the Codex target; no rule or AGENTS.md content was emitted", "Use a supported resource kind or manage AGENTS.md separately"},
	{CodeCodexFieldDropped, SeverityWarning, "A benign IR field has no documented Codex equivalent and was omitted", "Remove the field or choose a target that represents it"},
	{CodeSubagentCodexModelDropped, SeverityWarning, "A Codex-only subagent model was omitted for a non-Codex target", "Compile to Codex to preserve the native-only model, or choose a portable canonical model"},
	{CodeSubagentCodexEffortDropped, SeverityWarning, "A Codex-only subagent reasoning effort was omitted for a non-Codex target", "Compile to Codex to preserve the native-only effort, or use a portable canonical effort"},
	{CodeHookCodexPluginRootUnresolved, SeverityWarning, "The hook command references the plugin root but no plugin root was configured; the token was left unexpanded", "Compile with a plugin root so the command resolves, or use an absolute command path"},
	{CodeMcpCodexPluginRootUnresolved, SeverityWarning, "An MCP value references the plugin root but no plugin root was configured; the token was left unexpanded", "Compile with a plugin root so the value resolves, or use an absolute path"},
	{CodeIngestCodexPromptDeprecated, SeverityWarning, "A deprecated Codex prompt was ingested as a command", "Move the prompt to a command skill under .agents/skills"},
	{CodeIngestCodexFieldDropped, SeverityWarning, "A benign native Codex field has no canonical equivalent and was omitted", "Remove the field or keep the resource in Codex format"},
	{CodeRuleFactoryMetadataDropped, SeverityWarning, "Factory rules are plain markdown; globs/alwaysApply metadata dropped", "Factory does not support conditional (glob-scoped) rule application; the rule body is emitted verbatim"},
	{CodeCanonicalFieldOverridesExtra, SeverityWarning, "Canonical field overrides a conflicting preserved extra", "The canonical modeled value wins; rename the extension field to preserve both values"},
	{CodeHookPromptUnsupported, SeverityWarning, "Target cannot represent prompt hooks; hook dropped", "Use a command hook on this target"},
	{CodeHookGeminiEventUnmappable, SeverityWarning, "Gemini has no equivalent hook event; hook dropped", "Use a Gemini-supported event such as PreToolUse, PostToolUse, or PreCompact"},
	{CodeHookGeminiUnsupportedType, SeverityWarning, "Gemini cannot represent this hook type; hook dropped", "Use a command or prompt hook on Gemini"},
	{CodeHookWindsurfUnsupportedType, SeverityWarning, "Windsurf cannot represent this hook type; hook dropped", "Use a command hook on Windsurf"},
	{CodeHookWindsurfTimeoutUnsupported, SeverityError, "Windsurf cannot represent an explicit hook timeout; hook artifact suppressed", "Remove the timeout explicitly or use a target that supports it"},
	{CodeSkillSupportFilesDeferred, SeverityWarning, "Skill support file names were preserved but bytes were not materialized", "Materialize source-relative support files in the owning layer"},
	{CodeMcpLiteralCredential, SeverityWarning, "MCP configuration contains a literal credential-like value", "Use environment interpolation or an external secret reference"},
	{CodeSkillDevinModelUnmappable, SeverityWarning, "Explicit skill model is not a stable Devin alias; model was omitted", "Use sonnet, opus, or haiku for a stable Devin model alias"},
	{CodeSkillDevinTriggerUnmappable, SeverityWarning, "Trigger is outside Devin's user/model activation vocabulary; the trigger was dropped", "Use the user or model trigger, or select a target that represents this trigger"},
	{CodeSubagentDevinModelUnmappable, SeverityWarning, "Explicit model is not a stable Devin alias; model was omitted", "Use sonnet, opus, or haiku for a stable Devin model alias"},
	{CodeSubagentDevinModelDefault, SeverityWarning, "Empty subagent model uses Devin's default subagent model", "Set an explicit stable Devin model alias when a fixed model is required"},
	{CodeSubagentDevinEffortDropped, SeverityWarning, "Devin does not represent subagent effort", "Remove effort or use a target that supports it"},
	// Warnings since bead agent-marketplace-pu6b (Devin) and the Factory
	// unknown-tool policy change; these used to suppress the artifact. See the
	// rationale on the constants above and the dedicated section in codes.md.
	{CodeSkillDevinToolUnknown, SeverityWarning, "An explicitly allowed skill or command tool has no Devin equivalent; the allowed-tools key was omitted and the resource was still emitted", "Use one of Devin's allowed tools (read, edit, grep, glob, exec) to keep the restriction, or accept Devin's default tool set for this resource"},
	{CodeSubagentDevinToolUnknown, SeverityWarning, "An explicitly allowed subagent tool has no Devin equivalent; the allowed-tools key was omitted and the profile was still emitted", "Use one of Devin's allowed tools (read, edit, grep, glob, exec) to keep the restriction, or accept Devin's default tool set for this subagent"},
	{CodeHookDevinPayloadDropped, SeverityWarning, "The hook carries the payload its type does not select; that payload was dropped", "Split the hook into one command hook and one prompt hook if both payloads are required"},
	{CodeHookDevinPluginRootUnresolved, SeverityWarning, "The hook command references the plugin root but no plugin root was configured; the token was left unexpanded", "Compile with a plugin root so the command resolves, or use an absolute command path"},
	{CodeMcpDevinFieldDropped, SeverityWarning, "A modeled MCP field belongs to the documented server form this server did not select and was dropped", "Move the field to a server of the form that documents it"},
	{CodeMcpDevinPluginRootUnresolved, SeverityWarning, "An MCP value references the plugin root but no plugin root was configured; the token was left unexpanded", "Compile with a plugin root so the value resolves, or use an absolute path"},
	{CodeIngestDevinHookEventUnmappable, SeverityWarning, "Devin hook event is outside the documented Devin vocabulary; the original event name was preserved", "Use a documented Devin event (PreToolUse, PostToolUse, PermissionRequest, UserPromptSubmit, Stop, SessionStart, SessionEnd, PostCompaction); an undocumented event cannot round-trip"},
	{CodeIngestDevinModelNotCanonical, SeverityWarning, "Devin model alias has no canonical equivalent; the value was preserved verbatim", "Use sonnet, opus, or haiku so the model round-trips; other Devin aliases are dropped when the resource is compiled again"},
	{CodeIngestDevinCommandFieldDropped, SeverityWarning, "A command-provenance Devin skill carried a field the canonical command IR cannot hold; the field was dropped", "Remove the field, or remove x-agent-marketplace-kind: command so the resource is ingested as a skill"},
	{CodeIngestDevinReservedFieldDropped, SeverityWarning, "A frontmatter field Devin does not read was dropped instead of being preserved as an extra", "Devin subagents declare tool restrictions with allowed-tools; the Claude-style tools key is neither read by Devin nor re-emitted"},
	{CodeIngestDevinSubagentDirectoryFormSkipped, SeverityWarning, "A Devin subagent in the agents/<name>/AGENT.md directory form was not ingested", "Canonical Devin output is the flat agents/<name>.md form; convert the profile to a flat file so it is ingested"},
	{CodeIngestDevinLegacyMcpLocationIgnored, SeverityWarning, "A pre-3000.3 config.json#mcpServers block was found and deliberately not read", "Devin 3000.3 migrated MCP configuration into mcp_config.json; move the servers there so they are ingested"},
	{CodeOwnershipStateMissing, SeverityWarning, "No durable ownership record for this target root; every existing aggregate entry is treated as unmanaged", "Nothing is pruned or replaced without proof of ownership; run doctor --repair to adopt exact matches"},
	{CodeOwnershipEntryDrift, SeverityWarning, "A proven owned aggregate entry was edited by hand and was replaced from marketplace configuration", "Edit the marketplace resource instead: an owned entry is regenerated on every sync"},
	{CodeOwnershipUnmanagedKey, SeverityWarning, "A desired aggregate key already exists and is not proven owned; the existing entry was preserved and only the generated entry failed", "Run doctor --repair: it adopts the existing entry when it matches exactly, and offers to replace it when the value differs; renaming the marketplace resource also resolves it"},
	{CodeOwnershipRecovered, SeverityWarning, "An incomplete journaled generation was recovered on startup", "No action needed; the recorded journal phase decided whether the generation rolled forward or back"},
	{CodeOwnershipLockUnavailable, SeverityWarning, "This filesystem or platform does not support advisory locking, so ownership mutations for this target root are not serialized against other processes", "Concurrent writers on this root can still race; move XDG_STATE_HOME to a local filesystem if two agent-marketplace processes ever run at once"},
	{CodeMaterializeDevinHookSatisfiedByUnmanaged, SeverityWarning, "A handwritten Devin hook is semantically identical to a selected marketplace hook, so no generated entry was added", "No action needed; the existing handwritten entry already provides the behaviour and is neither adopted nor duplicated"},
	{CodeMaterializeDevinAggregatePrunePostponed, SeverityWarning, "A resource was skipped this run, so stale owned aggregate entries were not pruned", "Fix the reported resource errors and re-run sync; a skipped resource is indistinguishable from a removed one, so pruning would delete valid output"},
	{CodeMaterializeCodexHookSatisfiedByUnmanaged, SeverityWarning, "A handwritten Codex hook is semantically identical to a selected marketplace hook, so no generated entry was added", "No action needed; the existing handwritten entry already provides the behaviour and is neither adopted nor duplicated"},
	{CodeMaterializeCodexAggregatePrunePostponed, SeverityWarning, "A Codex resource was skipped this run, so stale owned aggregate entries were not pruned", "Fix the reported resource errors and re-run sync; a skipped resource is indistinguishable from a removed one, so pruning would delete valid output"},
	{CodeDoctorDevinVersionUnsupported, SeverityWarning, "The installed Devin CLI is older than the 3000.3 compatibility floor", "Sync does not require a Devin binary and is not blocked; upgrade the Devin CLI to 3000.3 or newer before relying on the generated output"},
	{CodeDoctorDevinVersionUnknown, SeverityWarning, "A Devin CLI is installed but its version could not be established", "The version probe failed, timed out, or printed no recognizable version; nothing is blocked, but 3000.3 compatibility could not be confirmed"},
	{CodeDoctorRepairConflictReplaceable, SeverityWarning, "An existing unmanaged aggregate entry has the same key as a desired entry but a different value, so it is neither an exact adoption match nor an evidence-backed orphan", "Run doctor --repair on a terminal to confirm replacing it with the generated entry; the existing entry is preserved until you confirm, a fresh safety backup is taken first, and --yes never replaces it"},

	// Error
	{CodeSubagentFactoryToolUnknown, SeverityError, "Factory cannot map an explicit subagent tool; subagent artifact suppressed", "Use a supported tool mapping or remove the restriction explicitly"},
	{CodeSkillCodexSecurityRestrictionUnsupported, SeverityError, "Codex skills cannot enforce an explicit security restriction; artifact suppressed", "Remove the restriction explicitly or use a target that represents it"},
	{CodeHookEventUnmappable, SeverityError, "Target cannot preserve this hook event; hook suppressed", "Use a hook event documented by the target"},
	{CodeHookNativeLiteralUnsupported, SeverityError, "Target would reinterpret a native Codex hook literal; hook suppressed", "Remove the target-specific interpolation token or keep the hook in Codex format"},
	{CodeHookCodexNativeFieldsUnsupported, SeverityError, "Target cannot preserve native Codex hook execution or context fields; hook suppressed", "Remove async, commandWindows, and additionalContextLimit explicitly or keep the hook in Codex format"},
	{CodeHookCodexUnsupported, SeverityError, "Codex does not execute this hook event or handler; hook suppressed", "Use a documented Codex command hook event"},
	{CodeHookWindsurfNativeFieldsUnsupported, SeverityError, "Windsurf cannot preserve conflicting native hook fields; hook suppressed", "Remove the conflicting native fields explicitly or use a target that represents them"},
	{CodeMcpSecurityRestrictionUnsupported, SeverityError, "Target cannot preserve native MCP authority or literal semantics; server suppressed", "Keep the server in its native format or remove the unsupported security field explicitly"},
	{CodeMcpCodexSecurityUnsupported, SeverityError, "Codex MCP output cannot preserve the requested authentication or transport semantics; server suppressed", "Use documented Codex MCP fields or remove the unsupported setting explicitly"},
	{CodeIngestCodexLoadResourceFailed, SeverityError, "One malformed Codex resource or aggregate was skipped", "Fix the reported native file; independent valid resources were retained"},
	{CodeIngestCodexDuplicateIdentity, SeverityError, "Duplicate Codex resource identity was suppressed without choosing a winner", "Rename one duplicate native resource"},
	{CodeIngestCodexHookUnsupported, SeverityError, "An unsupported native Codex hook event or handler was skipped", "Use a documented Codex event with a command handler"},
	{CodeIngestCodexMcpSecurityUnsupported, SeverityError, "Security-sensitive native Codex MCP fields cannot be represented safely", "Remove the unsupported restriction or keep the server in Codex format"},
	{CodeIngestCodexSubagentSecurityUnsupported, SeverityError, "Authority-bearing native Codex subagent fields cannot be represented safely", "Remove the unsupported restriction or keep the subagent in Codex format"},
	{CodeMaterializeCodexDuplicateIdentity, SeverityError, "Duplicate desired Codex aggregate identity was suppressed without choosing a winner", "Rename one duplicate selected resource"},
	{CodeSubagentFactoryDisallowedToolsDropped, SeverityError, "Factory cannot represent disallowedTools; artifact suppressed", "Use a target that supports disallowedTools or remove the restriction explicitly"},
	{CodeSubagentFactoryIsolationDropped, SeverityError, "Factory cannot represent isolation; artifact suppressed", "Use a target that supports isolation or remove the restriction explicitly"},
	{CodeSubagentGeminiToolUnknown, SeverityError, "Gemini tool is not in the mapping table; artifact suppressed", "Add the tool to the mapping table or remove the explicit tool"},
	{CodeSubagentCursorToolUnknown, SeverityError, "Cursor tool is not in the mapping table; artifact suppressed", "Add the tool to the mapping table or remove the explicit tool"},
	{CodeSubagentSecurityRestrictionUnsupported, SeverityError, "Unsupported subagent security restriction suppresses the artifact", "Use a target that represents the restriction or remove it explicitly"},
	{CodeBundleConflictDuplicateArtifactPath, SeverityError, "Two resources produce the same artifact output path", "Rename one of the conflicting resources or adjust the output path"},
	{CodeBundleConflictCrossKindArtifactPath, SeverityError, "Resources of different kinds produce the same artifact output path; neither artifact is emitted", "Rename one of the conflicting resources: on targets that compile commands as skills, a skill and a command cannot share a name"},
	{CodeBundleCompileResourceFailed, SeverityError, "A resource compiler failed", "Fix the resource compiler error before materializing the bundle"},
	{CodeIngestClaudeLoadSkillFailed, SeverityError, "Failed to load a skill from the Claude source layout", "Check the skill directory structure and SKILL.md frontmatter"},
	{CodeIngestClaudeLoadCommandFailed, SeverityError, "Failed to load a command from the Claude source layout", "Check the command .md file structure and frontmatter"},
	{CodeIngestClaudeLoadSubagentFailed, SeverityError, "Failed to load a subagent from the Claude source layout", "Check the agent .md file structure and frontmatter"},
	{CodeIngestFactoryLoadSkillFailed, SeverityError, "Failed to load a skill from the Factory source layout", "Check the skill directory structure and SKILL.md contents"},
	{CodeIngestFactoryLoadCommandFailed, SeverityError, "Failed to load a command from the Factory source layout", "Check the command .md file structure"},
	{CodeIngestFactoryLoadSubagentFailed, SeverityError, "Failed to load a subagent from the Factory source layout", "Check the droid .md file structure and frontmatter"},
	{CodeIngestFactoryLoadMcpServersFailed, SeverityError, "Failed to load MCP servers from the Factory source layout", "Check the mcp.json file structure"},
	{CodeIngestCursorLoadSkillFailed, SeverityError, "Failed to load a skill from the Cursor source layout", "Check the skill directory structure and SKILL.md frontmatter"},
	{CodeIngestCursorLoadCommandFailed, SeverityError, "Failed to load a command from the Cursor source layout", "Check the command .md file structure and frontmatter"},
	{CodeIngestCursorLoadSubagentFailed, SeverityError, "Failed to load a subagent from the Cursor source layout", "Check the agent .md file structure and frontmatter"},
	{CodeIngestGeminiLoadSkillFailed, SeverityError, "Failed to load a skill from the Gemini source layout", "Check the .gemini/skills/<name>/SKILL.md layout"},
	{CodeIngestGeminiLoadCommandFailed, SeverityError, "Failed to load a command from the Gemini source layout", "Check the commands/*.toml file structure"},
	{CodeIngestGeminiLoadSubagentFailed, SeverityError, "Failed to load a subagent from the Gemini source layout", "Check the .gemini/agents/*.md frontmatter"},
	{CodeIngestGeminiLoadHookFailed, SeverityError, "Failed to load a hook from the Gemini source layout", "Check the settings.json hook structure"},
	{CodeIngestFactoryLoadRuleFailed, SeverityError, "Failed to load a rule from the Factory source layout", "Check the .factory/rules/*.md file structure"},
	{CodeIngestCursorLoadRuleFailed, SeverityError, "Failed to load a rule from the Cursor source layout", "Check the .cursor/rules/*.mdc file structure and frontmatter"},
	{CodeSubagentDevinBuiltinNameConflict, SeverityError, "Subagent name collides with a Devin built-in subagent, which cannot be replaced; artifact suppressed", "Rename the subagent so it does not shadow a Devin built-in such as subagent_explore or subagent_general"},
	{CodeSubagentDevinNestedNameUnsupported, SeverityError, "Subagent name expresses nesting, which Devin's flat agents directory cannot represent; artifact suppressed", "Rename the subagent to a flat name: Devin discovers only agents/<name>.md, and flattening the name could collide with another subagent"},
	{CodeHookDevinEventUnmappable, SeverityError, "Hook event is not documented by Devin; the hook entry was skipped and sync exits non-zero", "Use a Devin-documented hook event: PreToolUse, PostToolUse, PermissionRequest, UserPromptSubmit, Stop, SessionStart, SessionEnd, or PostCompaction"},
	{CodeHookDevinUnsupportedType, SeverityError, "Devin does not represent this hook type; the hook entry was skipped and sync exits non-zero", "Use a command or prompt hook supported by Devin"},
	{CodeHookDevinMatcherUnsupportedEvent, SeverityError, "Devin documents matchers for tool events only; the hook entry was skipped rather than emitted without its matcher", "Remove the matcher, or move the hook to PreToolUse or PostToolUse"},
	{CodeHookDevinPayloadMissing, SeverityError, "The hook type selects a payload the resource does not carry; the hook entry was skipped", "Set the command payload on a command hook or the prompt payload on a prompt hook"},
	{CodeHookDevinTimeoutInvalid, SeverityError, "Hook timeout is negative, which Devin does not define; the hook entry was skipped", "Use a non-negative timeout or remove the timeout"},
	{CodeMcpDevinTransportAmbiguous, SeverityError, "The MCP server declares both a stdio command and a remote url, which Devin's two documented server forms cannot express together; the server entry was skipped", "Split the definition into one stdio server and one remote server"},
	{CodeMcpDevinTransportIncomplete, SeverityError, "The MCP server declares neither a stdio command nor a remote url; the server entry was skipped", "Set a command for a stdio server or a url for a remote server"},
	{CodeMcpDevinTransportUnsupported, SeverityError, "The MCP server requests a transport Devin does not document; the server entry was skipped rather than defaulted", "Devin documents the http and sse remote transports"},
	{CodeMcpDevinInterpolationUnrepresentable, SeverityError, "The command or url the MCP server's documented form cannot do without uses an interpolation style Devin has no syntax for; the server entry was skipped", "Devin documents ${env:VAR} interpolation only; move the value into an environment variable, or use a literal command or url"},
	{CodeIngestDevinLoadSkillFailed, SeverityError, "Failed to load a skill directory from the Devin source layout", "Check the skills/<name>/SKILL.md layout and its YAML frontmatter"},
	{CodeIngestDevinLoadSubagentFailed, SeverityError, "Failed to load a flat subagent profile from the Devin source layout", "Check the agents/<name>.md frontmatter"},
	{CodeIngestDevinLoadHookFailed, SeverityError, "Failed to load hooks from the Devin source layout", "Check the hooks.v1.json event object or the hooks member of config.json"},
	{CodeIngestDevinLoadMcpServersFailed, SeverityError, "Failed to load MCP servers from the Devin source layout", "Check the mcpServers object in mcp_config.json"},
	{CodeIngestDevinToolUnreversible, SeverityError, "An allowed-tools entry is outside Devin's closed read/edit/grep/glob/exec vocabulary; the resource was not ingested", "Devin silently ignores unknown tool names, so the restriction is not enforced; fix the allowed-tools list rather than letting an unenforced restriction become canonical IR"},
	{CodeJSONDuplicateKey, SeverityError, "JSON object contains the same key more than once; the document was rejected before it could be decoded", "Remove the duplicate key: encoding/json would silently keep the last value and change commands, headers, env, disabled state, or hooks"},
	{CodeJSONMalformed, SeverityError, "JSON document is not well-formed and was rejected before it could be decoded", "Fix the JSON syntax reported in the message"},
	{CodeOwnershipStateCorrupt, SeverityError, "The durable ownership record could not be read or failed validation; every existing aggregate entry is treated as unmanaged", "Nothing is pruned or replaced; run doctor --repair to re-establish ownership from exact matches"},
	{CodeOwnershipStateRootMismatch, SeverityError, "An ownership record was found under this root key but records a different canonical target root", "This is a state-key collision and is refused rather than resolved; report it, and move or remove the conflicting record"},
	{CodeOwnershipStateUnsafeValue, SeverityError, "Refused to record an ownership value that is not a non-secret identity", "Ownership state stores object keys, event names, and content digests only; never a payload, matcher, env value, header, or secret"},
	{CodeOwnershipSafetyBackup, SeverityError, "The mandatory raw-byte safety backup could not be written; the aggregate mutation was refused", "Safety backups are mandatory even when configured backups are disabled; fix the XDG state directory permissions"},
	{CodeOwnershipForeignChange, SeverityError, "The aggregate file changed underneath a staged generation; the generation was abandoned and the file left as found", "Re-run sync: the file on disk is neither the pre-mutation nor the staged content, so it was written by something else"},
	{CodeOwnershipLockTimeout, SeverityError, "This target root's ownership lock could not be acquired before the timeout, so its aggregates were left exactly as found", "Another agent-marketplace process is still holding it; wait for that run to finish, or raise AGENT_MARKETPLACE_OWNERSHIP_LOCK_TIMEOUT"},
	{CodeOwnershipGenerationConflict, SeverityError, "The aggregate file or ownership generation changed underneath this mutation; the aggregate was left as found", "Another writer or an external tool changed the file or the ownership record after this run read it; re-run sync"},
	{CodeMaterializeHookSupportConflict, SeverityError, "Conflicting desired support files block affected hooks rather than selecting a winner", "Rename the conflicting support files or exclude one hook provider"},
	{CodeMaterializeDevinAggregateMalformed, SeverityError, "The destination Devin aggregate JSON could not be parsed, so that file was left exactly as found", "Fix the reported parse location by hand; a safety backup of the raw bytes is retained under the Agent Marketplace state root"},
	{CodeMaterializeDevinAggregateShape, SeverityError, "The destination Devin aggregate has a shape this target cannot patch, so that file was left exactly as found", "The aggregate root and its hooks/mcpServers members must be JSON objects; move the conflicting value out of the way by hand"},
	{CodeMaterializeDevinAggregateWrite, SeverityError, "The journaled Devin aggregate mutation failed, so that file was left exactly as found", "Nothing was partially written; check the reported cause and re-run sync"},
	{CodeMaterializeCodexAggregateMalformed, SeverityError, "A Codex aggregate could not be parsed or safely reconciled, so that file was left exactly as found", "Fix the reported hooks.json or config.toml error by hand; the malformed file is preserved"},
	{CodeMaterializeCodexAggregateUnpatchable, SeverityError, "An existing Codex MCP server uses an alternate TOML form that cannot be safely patched, so that entry was preserved", "Rewrite the server as a standard [mcp_servers.<name>] table or edit it by hand before syncing"},
	{CodeMaterializeDestinationOverlap, SeverityError, "Resolved Codex destinations overlap in a way that cannot be safely coalesced, so materialization was refused", "Use compatible repository and machine modes, resolve conflicting resources, or separate the repository and CODEX_HOME roots"},
	{CodeMaterializeCodexTrustReview, SeverityWarning, "Repository Codex hooks or MCP configuration changed and may require project trust review", "Review or re-approve the repository in Codex before using the changed configuration"},
	{CodeMaterializeCodexTrustReviewPending, SeverityWarning, "A check found a pending repository Codex change without mutating files or trust state", "A later sync may require reviewing or re-approving the repository in Codex"},
	{CodeMaterializeDevinSymlinkRequired, SeverityError, "Devin skills and subagents must be symlinks, so file-copy materialization was refused for this target", "Devin has no real-file fallback; set the materialization mode back to symlink for the Devin harness"},
	{CodeDoctorRepairFailed, SeverityError, "An authorized repair action failed; the aggregate was left exactly as found", "This is the partial repair failure outcome and exits 1; re-running the repair is safe"},
}

// DocForCode returns the documentation entry for the given diagnostic code.
// If no entry exists, it returns false.
func DocForCode(code string) (CodeDoc, bool) {
	for _, doc := range codeCatalog {
		if doc.Code == code {
			return doc, true
		}
	}
	return CodeDoc{}, false
}

// AllCodes returns every documented diagnostic code in codeCatalog order.
// codeCatalog is the single source of truth; codes.md follows this order.
func AllCodes() []string {
	codes := make([]string, 0, len(codeCatalog))
	for _, doc := range codeCatalog {
		codes = append(codes, doc.Code)
	}
	return codes
}
