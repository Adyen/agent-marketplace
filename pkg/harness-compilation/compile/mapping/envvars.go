package mapping

// envEntry maps a source environment variable token to per-target replacements.
type envEntry struct {
	Source  string // Source token, e.g. "${CLAUDE_PLUGIN_ROOT}"
	Targets map[Target]envReplacement
}

// envReplacement holds the replacement string and whether it should be treated
// as a literal path rather than an environment variable reference.
type envReplacement struct {
	Value   string // Replacement string
	Literal bool   // If true, the replacement is a literal path, not an env var
}

// envTable is the curated substitution table for environment variable tokens.
//
// Some targets use env var references (e.g. ${DROID_PLUGIN_ROOT}), while others
// require literal path substitution (Windsurf) or use different variable names.
var envTable = []envEntry{
	// Last verified: 2026-04-23 against Factory Droid v1.0
	{Source: "${CLAUDE_PLUGIN_ROOT}", Targets: map[Target]envReplacement{
		TargetClaude:   {Value: "${CLAUDE_PLUGIN_ROOT}", Literal: false},
		TargetFactory:  {Value: "${DROID_PLUGIN_ROOT}", Literal: false},
		TargetCursor:   {Value: "${CURSOR_PLUGIN_ROOT}", Literal: false},
		TargetWindsurf: {Value: "", Literal: true}, // Caller provides via WithPluginRoot option
		TargetGemini:   {Value: "${extensionPath}", Literal: false},
		// Devin provenance: the 3000.3 evidence snapshot
		// (docs/devin-cli-harness-spec-3000.3.md) documents no plugin-root
		// interpolation token, and Devin plugins are explicitly out of scope for
		// this increment. The cell is therefore the same "caller must supply a
		// literal path" shape as Windsurf, not a mappable token.
		//
		// Contract for the Devin compilers that consume it (hooks in D6 and MCP
		// servers in D7 are the only Devin outputs that carry command strings):
		// resolve ${CLAUDE_PLUGIN_ROOT} from compile.Options.PluginRoot(), and
		// when no plugin root is configured, leave the token untouched and emit a
		// diagnostic. Expanding an empty replacement into the artifact would be a
		// silently faked value and is forbidden.
		TargetDevin: {Value: "", Literal: true},
		// Codex provenance: verified 2026-08-19 against
		// https://developers.openai.com/codex/hooks. Codex supplies
		// CLAUDE_PLUGIN_ROOT only to plugin-bundled hooks; plugin packaging is
		// outside this compiler's output contract, so callers must provide the
		// literal root for directly materialized hooks and MCP configuration.
		TargetCodex: {Value: "", Literal: true},
	}},
}

// EnvToken returns the replacement for a source environment variable token for
// a given target. If the replacement is a literal path, literal=true and the
// caller should treat the replacement as a verbatim string (not an env var).
// For targets where the caller must supply the value (e.g. Windsurf and Devin
// need a literal plugin root path), the replacement Value will be empty and
// literal=true. An empty literal replacement must never be substituted into an
// artifact: the caller supplies the path (compile.WithPluginRoot) or emits a
// diagnostic and leaves the source token in place.
func EnvToken(source string, target Target) (replacement string, literal bool) {
	for _, entry := range envTable {
		if entry.Source == source {
			r, found := entry.Targets[target]
			if !found {
				return source, false
			}
			return r.Value, r.Literal
		}
	}
	return source, false
}
