package mapping

import "regexp"

// mcpEntry describes how a source interpolation pattern is transformed for a
// given target harness.
type mcpEntry struct {
	Source  string // Source interpolation pattern
	Targets map[Target]string
}

// mcpUnmappable marks a target cell that cannot represent the source
// interpolation style at all. Callers must treat it as unmappable (see
// McpInterpolationSupported) and emit diag.CodeMcpInterpolationUnsupported
// instead of writing an empty value into the MCP config.
const mcpUnmappable = ""

// mcpTable is the curated interpolation pattern table for MCP server
// configuration. Different targets use different variable interpolation
// syntaxes in their MCP config files.
//
// Codex provenance for every Codex cell below: verified 2026-08-19 against
// https://developers.openai.com/codex/mcp. Codex supports literal values and
// environment-backed HTTP credentials, but documents no file interpolation.
var mcpTable = []mcpEntry{
	// Last verified: 2026-04-23 against Claude Code v1.0
	// Claude and Factory use inline environment variable values.
	{Source: "inline", Targets: map[Target]string{
		TargetClaude:   "inline",
		TargetFactory:  "inline",
		TargetCursor:   "inline",
		TargetWindsurf: "${env:VAR}",
		TargetGemini:   "${extensionPath}",
		// Devin provenance: bundled 3000.3 docs (docs/devin-cli-harness-spec-3000.3.md,
		// "MCP servers → Schema") show ${env:VAR} interpolation; the 3000.3.27
		// interpolation probe is still pending.
		TargetDevin:    "${env:VAR}",
		TargetCodex:    "inline",
		TargetOpenCode: "inline",
	}},

	// Last verified: 2026-04-23 against Windsurf v1.0
	// Windsurf uses ${env:VAR} for environment variables.
	{Source: "${env:VAR}", Targets: map[Target]string{
		TargetClaude:   "inline",
		TargetFactory:  "inline",
		TargetCursor:   "inline",
		TargetWindsurf: "${env:VAR}",
		TargetGemini:   "${extensionPath}",
		// Devin provenance: bundled 3000.3 docs document
		// "oauthClientSecret": "${env:OAUTH_CLIENT_SECRET}"; 3000.3.27 probe pending.
		TargetDevin:    "${env:VAR}",
		TargetCodex:    "${env:VAR}",
		TargetOpenCode: "{env:VAR}",
	}},

	// Last verified: 2026-04-23 against Windsurf v1.0
	// Windsurf uses ${file:...} for file references.
	{Source: "${file:PATH}", Targets: map[Target]string{
		TargetClaude:   "inline",
		TargetFactory:  "inline",
		TargetCursor:   "inline",
		TargetWindsurf: "${file:PATH}",
		TargetGemini:   "${extensionPath}",
		// Devin provenance: none. ${file:PATH} is a Windsurf-only interpolation
		// style with zero Devin evidence in the 3000.3 snapshot, so it is
		// explicitly unmappable rather than guessed.
		TargetDevin:    mcpUnmappable,
		TargetCodex:    mcpUnmappable,
		TargetOpenCode: "{file:PATH}",
	}},
}

// McpInterpolationSupported returns the target-specific interpolation pattern
// for a source pattern and reports whether the target can represent it.
//
// An explicitly empty target cell (mcpUnmappable) means the target has no
// equivalent interpolation syntax: ok is false and the caller must emit
// diag.CodeMcpInterpolationUnsupported and skip the value instead of writing an
// empty string. Unknown source patterns and targets without a declared cell are
// passed through unchanged with ok=true.
func McpInterpolationSupported(source string, target Target) (pattern string, ok bool) {
	for _, entry := range mcpTable {
		if entry.Source == source {
			m, found := entry.Targets[target]
			if !found {
				return source, true
			}
			if m == mcpUnmappable {
				return "", false
			}
			return m, true
		}
	}
	return source, true
}

// McpInterpolation returns the target-specific interpolation pattern for a
// given source pattern. The source pattern is a well-known interpolation style
// identifier (e.g. "inline", "${env:VAR}", "${file:PATH}").
//
// It returns "" when the target cannot represent the source style. Callers that
// must distinguish "unmappable" from "empty pattern" have to use
// McpInterpolationSupported.
func McpInterpolation(source string, target Target) string {
	pattern, _ := McpInterpolationSupported(source, target)
	return pattern
}

var bareEnvReference = regexp.MustCompile(`\$\w+`)

// ContainsBareEnvReference reports whether value contains a bare $NAME, which
// Gemini expands as an environment reference and canonical values treat as
// literal text.
func ContainsBareEnvReference(value string) bool {
	return bareEnvReference.MatchString(value)
}

// BraceBareEnvReferences rewrites every bare $NAME in value to ${NAME}.
func BraceBareEnvReferences(value string) string {
	return bareEnvReference.ReplaceAllStringFunc(value, func(match string) string {
		return "${" + match[1:] + "}"
	})
}
