package devin

import (
	"log/slog"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/mapping"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// This file holds the Devin frontmatter field mappers that more than one
// resource kind needs: the strict allowed-tools resolver and the stable-alias
// model resolver. Skills, converted commands (skill.go), and flat custom
// subagents (subagent.go) all route their security-affecting fields through
// exactly one implementation, so a rule such as "never emit an unmapped tool
// identifier" cannot drift between kinds.

const (
	// allowedToolsKey is the canonical Devin frontmatter key for the explicit
	// tool restriction. It is also a reserved extension key: an explicit tool
	// restriction must never reach the artifact without passing through the
	// closed Devin tool vocabulary. When it cannot pass through, the key is
	// omitted with a warning rather than emitted unmapped (see
	// resolveAllowedTools).
	allowedToolsKey = "allowed-tools"

	// AllowedToolsDefaultSetClause is the shared outcome clause for every
	// resolveAllowedTools message that drops an unrepresentable restriction.
	// It is exported so the restatement in subagent.go and tests can key off the
	// exact wording rather than duplicating it.
	AllowedToolsDefaultSetClause = "the allowed-tools key was omitted and Devin's default tool set applies"
	// allowedToolsSuppressedClause replaces the default-set clause when the
	// resource is suppressed for a different reason, so the message does not send
	// the user looking for an artifact that was never written.
	allowedToolsSuppressedClause = "the profile is suppressed for another reason, so no Devin artifact was written"

	// pluginRootName is the bare name inside the canonical plugin-root token.
	pluginRootName = "CLAUDE_PLUGIN_ROOT"
	// pluginRootToken is the canonical IR plugin-root token. Devin documents no
	// plugin-root interpolation of its own (see mapping/envvars.go), so it is
	// resolved at compile time or reported.
	pluginRootToken = "${" + pluginRootName + "}"
)

// allowedToolsDefaultSetMessage returns a warning message that explains an
// unrepresentable restriction was omitted and the resource will use Devin's
// default tool set. The shared clause is the single source of truth for that
// outcome so it cannot drift between the producer and the restatement path.
func allowedToolsDefaultSetMessage(reason string) string {
	return reason + "; " + AllowedToolsDefaultSetClause
}

// resolvePluginRootToken expands the canonical plugin-root token in value.
//
// It implements the contract D3 recorded in compile/mapping/envvars.go for the
// Devin cell: "resolve ${CLAUDE_PLUGIN_ROOT} from compile.Options.PluginRoot(),
// and when no plugin root is configured, leave the token untouched and emit a
// diagnostic. Expanding an empty replacement into the artifact would be a
// silently faked value and is forbidden."
//
// unresolved reports that the token was present but no plugin root was
// available, so the caller must emit its kind-scoped diagnostic. The value is
// never logged or returned in a diagnostic by this helper: hooks pass command
// lines through it and MCP servers pass env values, headers, and the OAuth
// client secret.
func resolvePluginRootToken(value string, opts compile.Options) (resolved string, unresolved bool) {
	if !strings.Contains(value, pluginRootToken) {
		return value, false
	}
	replacement, literal := mapping.EnvToken(pluginRootToken, mapping.TargetDevin)
	if literal && replacement == "" {
		replacement = opts.PluginRoot()
	}
	if replacement == "" {
		return value, true
	}
	return strings.ReplaceAll(value, pluginRootToken, replacement), false
}

// resolveAllowedTools maps an explicit allowed-tools restriction into Devin's
// closed read/edit/grep/glob/exec vocabulary.
//
// canonicalSet distinguishes an absent restriction from an explicit empty one.
// When the canonical IR field is absent, a reserved allowed-tools extension
// field is still honored rather than passed through verbatim: emitting an
// unmapped restriction would write tool identifiers Devin does not accept.
//
// # Unrepresentable restrictions omit the key and warn (bead agent-marketplace-pu6b)
//
// When any part of the restriction cannot be expressed in Devin's vocabulary —
// a scoped Claude specifier such as `Bash(openspec:*)`, a name outside the
// mapping table such as `Write`, or an allowed-tools value that is not a list
// shape at all — this function returns present=false with WARNING diagnostics
// and the caller emits the artifact with NO allowed-tools key.
//
// This deliberately REVERSES the original contract, which made every one of
// those cases an error that suppressed the artifact. The reversal is recorded in
// diag/codes.go, diag/codes.md, field-matrix.md, and CHANGELOG.md; read those
// before restoring the old behaviour. In short:
//
//   - Scoped `Bash(...)` is what the openspec skill generator emits, so three
//     skills selected by this repo's own committed .agents/config.yaml took the
//     whole `sync` run down and installed nothing. The user's only fix was
//     editing a registry they may not own.
//   - Omitting the key inherits Devin's default tool set, which IS a real
//     widening relative to the source's stated restriction. That is the
//     accepted, deliberate cost, and the evidence for accepting it is narrower
//     than "Devin ignores allowed-tools" — do not restate it that way. Devin
//     DOES enforce a list it can represent: CONTROL E on 3000.3.27 verified on
//     disk that a profile compiled with `allowed-tools: [read, grep]` could
//     neither edit a file nor run a command
//     (docs/devin-cli-harness-spec-3000.3.md). What Devin cannot do is
//     represent the restriction at issue here: it has five bare names and no
//     per-command scoping, and any other name is SILENTLY DROPPED rather than
//     rejected (internal/devinprobe.TestProbeAllowedToolVocabulary). So the
//     choice is never "enforced vs unenforced"; it is which unfaithful
//     representation to write, and none of them is the source's restriction.
//     The same control also found that even a representable list bounds the
//     profile's OWN toolset only and not what it reaches through nesting, so it
//     is not a hard privilege boundary either way.
//   - Emitting `allowed-tools: []` was considered and rejected. It never
//     broadens, but it means the resource may use NO tools, and
//     `openspec-apply-change` needs `exec` to run `openspec`. That trades a loud
//     absence for a silently inert install. The mapTriggers analogy does not
//     carry: `triggers: []` only stops auto-activation and leaves the skill
//     usable.
//   - A partially mapped list is also never emitted, for the same reason: the
//     surviving subset is narrower than the source asked for and can leave the
//     resource unable to do its job.
//
// Devin and Factory now both publish a profile when a subagent tool cannot be
// represented. Gemini and Cursor still fail closed on subagent.<target>.tool-unknown.
//
// Explicitly empty (`allowed-tools: []`, or a string that splits to nothing) and
// fully mappable restrictions are UNCHANGED: the first still renders the
// narrowest representable value and the second still renders the mapped list.
//
// fromExtra reports that the restriction was consumed from the extension field
// rather than from the typed IR field, so the caller must neither re-emit the
// raw value nor report it as an overridden extra. It stays true on the
// unrepresentable path precisely so the raw, unmapped value is not written to
// the artifact as a preserved extra.
//
// code is the caller's kind-scoped diagnostic code
// (skill.devin.tool-unknown or subagent.devin.tool-unknown). Only the code
// varies, so consumer UIs can attribute the loss to the right resource kind.
//
// Neither the diagnostics nor the structured logs name the offending tool:
// allowed-tools entries carry Claude-style scoped permission strings such as
// `Bash(git push --force:*)`, i.e. command strings and permission values that
// the plan's observability section keeps out of logs and diagnostics. Resource
// and Source localize the loss instead.
func resolveAllowedTools(resource string, source diag.SourceRef, canonical []string, canonicalSet bool, extra map[string]any, opts compile.Options, code string) (tools []string, present, fromExtra bool, diags []diag.Diagnostic) {
	requested := canonical
	if !canonicalSet {
		value, ok := extra[allowedToolsKey]
		if !ok {
			return nil, false, false, nil
		}
		fromExtra = true
		parsed, ok := parseToolList(value)
		if !ok {
			slog.Warn("devin allowed-tools restriction unrepresentable", "target", mapping.TargetDevin, "resource", resource,
				"reason", "unsupported allowed-tools shape", "outcome", "allowed-tools key omitted")
			return nil, false, true, []diag.Diagnostic{{
				Code:     code,
				Severity: diag.SeverityWarning,
				Target:   string(mapping.TargetDevin),
				Resource: resource,
				Message:  allowedToolsDefaultSetMessage("explicit allowed-tools restriction is not a list of tool names"),
				Hint:     "Express allowed-tools as a list of canonical tool names that map to Devin's read, edit, grep, glob, or exec; the resource is still installed, but with Devin's default tool set rather than the restriction the source asked for",
				Source:   source,
			}}
		}
		requested = parsed
	}

	// Both shapes are normalized identically before anything is classified, so
	// the empty-vs-absent boundary cannot be crossed by the YAML shape the
	// author happened to use (bead agent-marketplace-pu6b, adversarial review).
	requested = normalizeToolEntries(requested)

	mapped := make([]string, 0, len(requested))
	seen := make(map[string]struct{}, len(requested))
	scoped, unmappable := 0, 0
	for _, tool := range requested {
		if _, overridden := opts.ToolOverride()[tool]; !overridden && strings.ContainsAny(tool, "()") {
			// A scoped Claude specifier such as `Bash(openspec:*)` has no Devin
			// equivalent: Devin's vocabulary is five bare tool names with no
			// per-command scoping. Mapping it to bare `exec` would silently
			// invent a broader restriction than the source wrote, so the
			// restriction is dropped whole and reported instead.
			//
			// The one exception is an explicit compile.WithToolOverride entry
			// for this exact specifier, handled above: that is the consumer
			// stating, per invocation, which Devin tool the specifier means.
			// Ignoring it would discard a restriction the caller asked for and
			// fall back to Devin's default tool set, which is strictly wider
			// than what they requested.
			scoped++
			diags = append(diags, diag.Diagnostic{
				Code:     code,
				Severity: diag.SeverityWarning,
				Target:   string(mapping.TargetDevin),
				Resource: resource,
				Message:  allowedToolsDefaultSetMessage("an allowed-tools entry carries a scoped tool specifier, which has no Devin equivalent"),
				Hint:     "Devin's allowed tools (read, edit, grep, glob, exec) cannot be scoped to a command; remove the specifier to restrict the resource on Devin, or accept Devin's default tool set",
				Source:   source,
			})
			continue
		}
		targetTools, ok := mapping.ToolWithOverride(tool, mapping.TargetDevin, opts.ToolOverride())
		if !ok {
			unmappable++
			diags = append(diags, diag.Diagnostic{
				Code:     code,
				Severity: diag.SeverityWarning,
				Target:   string(mapping.TargetDevin),
				Resource: resource,
				Message:  allowedToolsDefaultSetMessage("an explicitly allowed tool has no Devin equivalent"),
				Hint:     "Use one of Devin's allowed tools (read, edit, grep, glob, exec), pass compile.WithToolOverride to map the name yourself, or accept Devin's default tool set",
				Source:   source,
			})
			continue
		}
		for _, targetTool := range targetTools {
			if _, duplicate := seen[targetTool]; duplicate {
				continue
			}
			seen[targetTool] = struct{}{}
			mapped = append(mapped, targetTool)
		}
	}
	if len(diags) > 0 {
		// present=false: the whole key is omitted, not the mappable subset. A
		// partial list is narrower than the source asked for and can leave the
		// resource unable to do its job (an openspec skill without exec cannot
		// run openspec), so it is never emitted.
		slog.Warn("devin allowed tools unrepresentable", "target", mapping.TargetDevin, "resource", resource,
			"unmappable_count", unmappable, "scoped_count", scoped, "outcome", "allowed-tools key omitted")
		return nil, false, fromExtra, diags
	}
	return mapped, true, fromExtra, nil
}

// parseToolList accepts the shapes an explicit allowed-tools extension field
// can legitimately take: a YAML/JSON list of strings, or a single
// comma-separated string. Anything else is rejected by the caller rather than
// guessed at, because guessing changes an explicit security restriction.
//
// A string that contains no tool names after splitting (`""`, `","`) is an
// explicit empty restriction, not a parse failure: it renders as
// `allowed-tools: []`, exactly like the `[]` list shape. Both are the narrowest
// representable value, so treating them alike can only fail closed. This is one
// of the two behaviours agent-marketplace-pu6b explicitly left unchanged.
//
// Comma splitting deliberately does not understand `Tool(a, b)` grouping. Any
// fragment that still carries a parenthesis is classified by resolveAllowedTools
// as a scoped specifier, so a mangled split can never be rewritten into a
// different bare tool name: the restriction is dropped whole, with a warning,
// rather than reassembled by guesswork.
func parseToolList(value any) ([]string, bool) {
	switch typed := value.(type) {
	case []string:
		return typed, true
	case []any:
		tools := make([]string, 0, len(typed))
		for _, element := range typed {
			text, ok := element.(string)
			if !ok {
				return nil, false
			}
			tools = append(tools, text)
		}
		return tools, true
	case string:
		return normalizeToolEntries([]string{typed}), true
	default:
		return nil, false
	}
}

// normalizeToolEntries reduces an allowed-tools restriction of ANY accepted
// shape to the tool names it actually contains: each entry is split on commas,
// trimmed, and dropped when nothing is left.
//
// It exists because the two shapes used to be normalized differently, and after
// bead agent-marketplace-pu6b that difference decided PRIVILEGE rather than a
// diagnostic. The comma-separated string shape trimmed and dropped blank
// fields, so `allowed-tools: ""`, `allowed-tools: "  "` and `allowed-tools: ","`
// all rendered `allowed-tools: []` — the narrowest representable value. The list
// shape did neither, so the semantically identical `allowed-tools: [""]` had a
// blank entry that matched no tool, which since the reversal omits the WHOLE key
// and inherits Devin's DEFAULT tool set. Two spellings of "no tools" therefore
// produced "no tools" and "all tools". The same asymmetry made `[" Read "]`
// unmappable while `" Read "` mapped to `read`: a trailing space in a YAML
// scalar silently replaced a read-only restriction with every default tool.
//
// Normalizing both shapes here can only ever move an input toward the narrower
// outcome — it removes nothing but blanks and it never invents a tool name that
// was not written in the source — so it cannot broaden a restriction. Splitting
// still does not understand `Tool(a, b)` grouping, and every fragment carrying a
// parenthesis is still classified as a scoped specifier that drops the key
// whole, so no mangled fragment can be reassembled into a different bare tool.
func normalizeToolEntries(entries []string) []string {
	tools := make([]string, 0, len(entries))
	for _, entry := range entries {
		for _, field := range strings.Split(entry, ",") {
			field = strings.TrimSpace(field)
			if field == "" {
				continue
			}
			tools = append(tools, field)
		}
	}
	return tools
}
