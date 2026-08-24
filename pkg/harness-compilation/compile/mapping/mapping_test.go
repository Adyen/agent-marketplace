package mapping

import (
	"testing"

	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

// --- Model tests ---

func TestModel_Families(t *testing.T) {
	tests := []struct {
		claude string
		target Target
		want   string
		ok     bool
	}{
		// sonnet
		{"sonnet", TargetClaude, "claude-sonnet-4-5-20250929", true},
		{"sonnet", TargetFactory, "claude-sonnet-4-5-20250929", true},
		{"sonnet", TargetCursor, "claude-sonnet-4-5-20250929", true},
		{"sonnet", TargetWindsurf, "sonnet", false},        // inherit → passthrough
		{"sonnet", TargetGemini, "gemini-3-preview", true}, // mapped to Gemini model

		// opus
		{"opus", TargetClaude, "claude-opus-4-20250514", true},
		{"opus", TargetFactory, "claude-opus-4-20250514", true},
		{"opus", TargetCursor, "claude-opus-4-20250514", true},
		{"opus", TargetWindsurf, "opus", false},          // inherit
		{"opus", TargetGemini, "gemini-3-preview", true}, // mapped to Gemini model

		// haiku
		{"haiku", TargetClaude, "claude-haiku-3-5-20241022", true},
		{"haiku", TargetFactory, "claude-haiku-3-5-20241022", true},
		{"haiku", TargetCursor, "claude-haiku-3-5-20241022", true},
		{"haiku", TargetWindsurf, "haiku", false},               // inherit
		{"haiku", TargetGemini, "gemini-3-flash-preview", true}, // mapped to Gemini model
	}
	for _, tt := range tests {
		got, ok := Model(tt.claude, tt.target)
		if got != tt.want || ok != tt.ok {
			t.Errorf("Model(%q, %s) = (%q, %v), want (%q, %v)", tt.claude, tt.target, got, ok, tt.want, tt.ok)
		}
	}
}

func TestModel_SpecificIDs(t *testing.T) {
	tests := []struct {
		claude string
		target Target
		want   string
		ok     bool
	}{
		{"claude-sonnet-4-5-20250929", TargetFactory, "claude-sonnet-4-5-20250929", true},
		{"claude-sonnet-4-5-20250929", TargetClaude, "claude-sonnet-4-5-20250929", true},
		{"claude-sonnet-4-5-20250929", TargetCursor, "claude-sonnet-4-5-20250929", true},
		{"claude-sonnet-4-5-20250929", TargetWindsurf, "claude-sonnet-4-5-20250929", false},
		{"claude-sonnet-4-5-20250929", TargetGemini, "gemini-3-preview", true},

		{"claude-opus-4-20250514", TargetFactory, "claude-opus-4-20250514", true},
		{"claude-opus-4-20250514", TargetClaude, "claude-opus-4-20250514", true},

		{"claude-haiku-3-5-20241022", TargetFactory, "claude-haiku-3-5-20241022", true},
		{"claude-haiku-3-5-20241022", TargetClaude, "claude-haiku-3-5-20241022", true},
	}
	for _, tt := range tests {
		got, ok := Model(tt.claude, tt.target)
		if got != tt.want || ok != tt.ok {
			t.Errorf("Model(%q, %s) = (%q, %v), want (%q, %v)", tt.claude, tt.target, got, ok, tt.want, tt.ok)
		}
	}
}

func TestModel_Unknown(t *testing.T) {
	got, ok := Model("nonexistent-model", TargetFactory)
	if ok {
		t.Errorf("Model(unknown, factory) = ok=true, want ok=false")
	}
	if got != "nonexistent-model" {
		t.Errorf("Model(unknown, factory) = %q, want passthrough %q", got, "nonexistent-model")
	}
}

// --- Tool tests ---

func TestTool_BasicMapping(t *testing.T) {
	tests := []struct {
		claude string
		target Target
		want   []string
		ok     bool
	}{
		// Read → same everywhere
		{"Read", TargetClaude, []string{"Read"}, true},
		{"Read", TargetFactory, []string{"Read"}, true},
		{"Read", TargetCursor, []string{"Read"}, true},
		{"Read", TargetWindsurf, []string{"Read"}, true},
		{"Read", TargetGemini, []string{"Read"}, true},

		// Write → Edit+Create for Factory, Write for others
		{"Write", TargetClaude, []string{"Write"}, true},
		{"Write", TargetFactory, []string{"Edit", "Create"}, true},
		{"Write", TargetCursor, []string{"Write"}, true},
		{"Write", TargetWindsurf, []string{"Write"}, true},
		{"Write", TargetGemini, []string{"Write"}, true},

		// Bash → Execute for Factory, Bash for others
		{"Bash", TargetClaude, []string{"Bash"}, true},
		{"Bash", TargetFactory, []string{"Execute"}, true},
		{"Bash", TargetGemini, []string{"Bash"}, true},

		{"MultiEdit", TargetFactory, nil, false},
		{"WebFetch", TargetFactory, []string{"FetchUrl"}, true},

		// Glob → same everywhere
		{"Glob", TargetClaude, []string{"Glob"}, true},
		{"Glob", TargetFactory, []string{"Glob"}, true},

		// Grep → same everywhere
		{"Grep", TargetClaude, []string{"Grep"}, true},
		{"Grep", TargetFactory, []string{"Grep"}, true},
	}
	for _, tt := range tests {
		got, ok := Tool(tt.claude, tt.target)
		if ok != tt.ok {
			t.Errorf("Tool(%q, %s): ok=%v, want %v", tt.claude, tt.target, ok, tt.ok)
		}
		if !sliceEqual(got, tt.want) {
			t.Errorf("Tool(%q, %s) = %v, want %v", tt.claude, tt.target, got, tt.want)
		}
	}
}

func TestTool_Unknown(t *testing.T) {
	got, ok := Tool("NonExistentTool", TargetFactory)
	if ok {
		t.Errorf("Tool(unknown, factory) = ok=true, want ok=false")
	}
	if got != nil {
		t.Errorf("Tool(unknown, factory) = %v, want nil", got)
	}
}

// --- EnvToken tests ---

func TestEnvToken_ClaudePluginRoot(t *testing.T) {
	tests := []struct {
		source  string
		target  Target
		want    string
		literal bool
	}{
		{"${CLAUDE_PLUGIN_ROOT}", TargetClaude, "${CLAUDE_PLUGIN_ROOT}", false},
		{"${CLAUDE_PLUGIN_ROOT}", TargetFactory, "${DROID_PLUGIN_ROOT}", false},
		{"${CLAUDE_PLUGIN_ROOT}", TargetCursor, "${CURSOR_PLUGIN_ROOT}", false},
		{"${CLAUDE_PLUGIN_ROOT}", TargetWindsurf, "", true},
		{"${CLAUDE_PLUGIN_ROOT}", TargetGemini, "${extensionPath}", false},
	}
	for _, tt := range tests {
		got, lit := EnvToken(tt.source, tt.target)
		if got != tt.want || lit != tt.literal {
			t.Errorf("EnvToken(%q, %s) = (%q, %v), want (%q, %v)", tt.source, tt.target, got, lit, tt.want, tt.literal)
		}
	}
}

func TestEnvToken_UnknownSource(t *testing.T) {
	got, lit := EnvToken("${UNKNOWN_VAR}", TargetFactory)
	if got != "${UNKNOWN_VAR}" {
		t.Errorf("EnvToken(unknown, factory) = %q, want passthrough %q", got, "${UNKNOWN_VAR}")
	}
	if lit {
		t.Errorf("EnvToken(unknown, factory) literal=true, want false")
	}
}

// --- McpInterpolation tests ---

func TestMcpInterpolation(t *testing.T) {
	tests := []struct {
		source string
		target Target
		want   string
	}{
		{"inline", TargetClaude, "inline"},
		{"inline", TargetFactory, "inline"},
		{"inline", TargetCursor, "inline"},
		{"inline", TargetWindsurf, "${env:VAR}"},
		{"inline", TargetGemini, "${extensionPath}"},

		{"${env:VAR}", TargetClaude, "inline"},
		{"${env:VAR}", TargetWindsurf, "${env:VAR}"},

		{"${file:PATH}", TargetWindsurf, "${file:PATH}"},
		{"${file:PATH}", TargetGemini, "${extensionPath}"},
	}
	for _, tt := range tests {
		got := McpInterpolation(tt.source, tt.target)
		if got != tt.want {
			t.Errorf("McpInterpolation(%q, %s) = %q, want %q", tt.source, tt.target, got, tt.want)
		}
	}
}

func TestMcpInterpolation_UnknownSource(t *testing.T) {
	got := McpInterpolation("unknown-pattern", TargetFactory)
	if got != "unknown-pattern" {
		t.Errorf("McpInterpolation(unknown, factory) = %q, want passthrough %q", got, "unknown-pattern")
	}
}

// --- Coverage test: prevents table-shrinkage ---

func TestMappingTables_Coverage(t *testing.T) {
	// Model table anchor entries — these must always be present.
	modelAnchors := []string{
		"sonnet",
		"opus",
		"haiku",
		"claude-sonnet-4-5-20250929",
		"claude-opus-4-20250514",
		"claude-haiku-3-5-20241022",
		"gemini-3-preview",
		"gemini-3-flash-preview",
	}
	modelSet := make(map[string]bool)
	for _, e := range modelTable {
		modelSet[e.Claude] = true
	}
	for _, a := range modelAnchors {
		if !modelSet[a] {
			t.Errorf("model table missing anchor entry %q", a)
		}
	}

	// Tool table anchor entries — these must always be present.
	toolAnchors := []string{
		"Read",
		"Write",
		"Edit",
		"Bash",
		"Glob",
		"Grep",
		"MultiEdit",
		"WebFetch",
		"WebSearch",
	}
	toolSet := make(map[string]bool)
	for _, e := range toolTable {
		toolSet[e.Claude] = true
	}
	for _, a := range toolAnchors {
		if !toolSet[a] {
			t.Errorf("tool table missing anchor entry %q", a)
		}
	}

	// Env table anchor entries.
	envAnchors := []string{
		"${CLAUDE_PLUGIN_ROOT}",
	}
	envSet := make(map[string]bool)
	for _, e := range envTable {
		envSet[e.Source] = true
	}
	for _, a := range envAnchors {
		if !envSet[a] {
			t.Errorf("env table missing anchor entry %q", a)
		}
	}

	// MCP table anchor entries.
	mcpAnchors := []string{
		"inline",
		"${env:VAR}",
		"${file:PATH}",
	}
	mcpSet := make(map[string]bool)
	for _, e := range mcpTable {
		mcpSet[e.Source] = true
	}
	for _, a := range mcpAnchors {
		if !mcpSet[a] {
			t.Errorf("mcp table missing anchor entry %q", a)
		}
	}

	// Each model entry must cover all declared targets.
	allTargets := targetmeta.Targets()
	for _, e := range modelTable {
		for _, tgt := range allTargets {
			if _, ok := e.Targets[tgt]; !ok {
				t.Errorf("model entry %q missing target %s", e.Claude, tgt)
			}
		}
	}

	// Each tool entry must cover all declared targets.
	for _, e := range toolTable {
		for _, tgt := range allTargets {
			if _, ok := e.Targets[tgt]; !ok {
				t.Errorf("tool entry %q missing target %s", e.Claude, tgt)
			}
		}
	}

	// Each env entry must cover all declared targets.
	for _, e := range envTable {
		for _, tgt := range allTargets {
			if _, ok := e.Targets[tgt]; !ok {
				t.Errorf("env entry %q missing target %s", e.Source, tgt)
			}
		}
	}

	// Each MCP interpolation entry must cover all declared targets.
	for _, e := range mcpTable {
		for _, tgt := range allTargets {
			if _, ok := e.Targets[tgt]; !ok {
				t.Errorf("mcp entry %q missing target %s", e.Source, tgt)
			}
		}
	}
}

// --- Helpers ---

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
