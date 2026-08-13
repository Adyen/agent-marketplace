package mapping

import (
	"sort"
	"testing"
)

func TestDevinModel(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
		wantOK bool
	}{
		{"sonnet alias", "sonnet", "sonnet", true},
		{"opus alias", "opus", "opus", true},
		{"haiku alias", "haiku", "haiku", true},
		{"gemini flash family maps to the documented gemini alias", "gemini-3-flash-preview", "gemini", true},
		{"gemini pro family has no documented alias", "gemini-3-preview", "", false},
		{"versioned sonnet is unmappable, never passthrough", "claude-sonnet-4-5-20250929", "", false},
		{"versioned opus is unmappable, never passthrough", "claude-opus-4-20250514", "", false},
		{"versioned haiku is unmappable, never passthrough", "claude-haiku-3-5-20241022", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Model(tt.source, TargetDevin)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("Model(%q, Devin) = (%q, %v), want (%q, %v)", tt.source, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestDevinTool(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   []string
		wantOK bool
	}{
		{"Read", "Read", []string{"read"}, true},
		{"Edit", "Edit", []string{"edit"}, true},
		{"Grep", "Grep", []string{"grep"}, true},
		{"Glob", "Glob", []string{"glob"}, true},
		{"Bash", "Bash", []string{"exec"}, true},
		{"Write is unsupported", "Write", nil, false},
		{"MultiEdit is unsupported", "MultiEdit", nil, false},
		{"WebFetch is unsupported", "WebFetch", nil, false},
		{"WebSearch is unsupported", "WebSearch", nil, false},
		{"unknown tool", "NoSuchTool", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Tool(tt.source, TargetDevin)
			if ok != tt.wantOK || !sliceEqual(got, tt.want) {
				t.Errorf("Tool(%q, Devin) = (%v, %v), want (%v, %v)", tt.source, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

// TestDevinToolVocabularyIsClosed asserts that the Devin allowed-tools
// vocabulary produced by the mapping table is exactly the five documented
// values, so a future table edit cannot widen Devin's privileges silently.
func TestDevinToolWithOverride(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		overrides map[string]string
		want      []string
		wantOK    bool
	}{
		{"curated value overridden", "Read", map[string]string{"Read": "custom-read"}, []string{"custom-read"}, true},
		{"unknown tool rescued by override", "NoSuchTool", map[string]string{"NoSuchTool": "custom"}, []string{"custom"}, true},
		{"no override falls back to table", "Read", map[string]string{"Other": "x"}, []string{"read"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ToolWithOverride(tt.source, TargetDevin, tt.overrides)
			if ok != tt.wantOK || !sliceEqual(got, tt.want) {
				t.Errorf("ToolWithOverride(%q, Devin, %v) = (%v, %v), want (%v, %v)", tt.source, tt.overrides, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestDevinModelWithOverride(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		overrides map[string]string
		want      string
		wantOK    bool
	}{
		{"curated value overridden", "sonnet", map[string]string{"sonnet": "custom-model"}, "custom-model", true},
		{"unmappable value rescued by override", "gemini-3-preview", map[string]string{"gemini-3-preview": "custom"}, "custom", true},
		{"no override falls back to table", "sonnet", map[string]string{"other": "x"}, "sonnet", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ModelWithOverride(tt.source, TargetDevin, tt.overrides)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("ModelWithOverride(%q, Devin, %v) = (%q, %v), want (%q, %v)", tt.source, tt.overrides, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestDevinToolVocabularyIsClosed(t *testing.T) {
	want := []string{"edit", "exec", "glob", "grep", "read"}

	var got []string
	for _, entry := range toolTable {
		mapped, ok := entry.Targets[TargetDevin]
		if !ok || len(mapped) == 0 {
			continue
		}
		got = append(got, mapped...)
	}
	sort.Strings(got)

	if !sliceEqual(got, want) {
		t.Errorf("Devin allowed-tools vocabulary = %v, want %v", got, want)
	}
}

func TestDevinHookEvent(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
		wantOK bool
	}{
		{"PreToolUse", "PreToolUse", "PreToolUse", true},
		{"PostToolUse", "PostToolUse", "PostToolUse", true},
		{"PermissionRequest", "PermissionRequest", "PermissionRequest", true},
		{"UserPromptSubmit", "UserPromptSubmit", "UserPromptSubmit", true},
		{"Stop", "Stop", "Stop", true},
		{"SessionStart", "SessionStart", "SessionStart", true},
		{"SessionEnd", "SessionEnd", "SessionEnd", true},
		{"PostCompaction", "PostCompaction", "PostCompaction", true},
		{"SubagentStop is unmappable", "SubagentStop", "", false},
		{"Notification is unmappable", "Notification", "", false},
		{"PreCompact is never mapped onto PostCompaction", "PreCompact", "", false},
		{"unknown event", "NoSuchEvent", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := HookEvent(tt.source, "Read", TargetDevin)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("HookEvent(%q, Devin) = (%q, %v), want (%q, %v)", tt.source, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

// TestDevinReverseHookEvent pins the reverse arm D8 added.
//
// The table is written out rather than derived, for the same reason
// devinReverseTable is: a test that computed its expectations from the forward
// table would pass even if the reverse arm went back to doing a forward lookup.
func TestDevinReverseHookEvent(t *testing.T) {
	tests := []struct {
		name        string
		devinEvent  string
		wantEvent   string
		wantMatcher string
		wantOK      bool
	}{
		{"PreToolUse", "PreToolUse", "PreToolUse", "", true},
		{"PostToolUse", "PostToolUse", "PostToolUse", "", true},
		{"PermissionRequest", "PermissionRequest", "PermissionRequest", "", true},
		{"UserPromptSubmit", "UserPromptSubmit", "UserPromptSubmit", "", true},
		{"Stop", "Stop", "Stop", "", true},
		{"SessionStart", "SessionStart", "SessionStart", "", true},
		{"SessionEnd", "SessionEnd", "SessionEnd", "", true},
		{"PostCompaction", "PostCompaction", "PostCompaction", "", true},

		// Devin never encodes a tool matcher in the event name, so no row may
		// ever return one. A non-empty matcher here would silently narrow every
		// ingested hook to one tool.
		//
		// The rows below are the ones a name-based reversal would get wrong.
		{"PreCompact is never reversed onto PostCompaction", "PreCompact", "", "", false},
		{"SubagentStop is not a Devin event", "SubagentStop", "", "", false},
		{"Notification is not a Devin event", "Notification", "", "", false},
		{"unknown event", "NoSuchEvent", "", "", false},
		{"empty event", "", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event, matcher, ok := ReverseHookEvent(tt.devinEvent, TargetDevin)
			if event != tt.wantEvent || matcher != tt.wantMatcher || ok != tt.wantOK {
				t.Errorf("ReverseHookEvent(%q, Devin) = (%q, %q, %v), want (%q, %q, %v)",
					tt.devinEvent, event, matcher, ok, tt.wantEvent, tt.wantMatcher, tt.wantOK)
			}
		})
	}
}

// TestDevinReverseHookEventAgreesWithForwardTable is the drift guard between
// the hand-written reverse table and the forward one.
//
// Both directions must describe the same vocabulary: every canonical event with
// a non-empty Devin cell must reverse back to itself, and every Devin event the
// reverse table lists must be produced by the forward table. Neither table is
// derived from the other, so this is a real cross-check rather than a tautology.
func TestDevinReverseHookEventAgreesWithForwardTable(t *testing.T) {
	forward := map[string]string{} // devin event -> canonical event
	var unmappable []string        // canonical events with no Devin cell
	for _, entry := range hookEventTable {
		devinEvent := entry.Targets[TargetDevin]
		if devinEvent == "" {
			unmappable = append(unmappable, entry.Claude)
			continue
		}
		if existing, exists := forward[devinEvent]; exists {
			t.Errorf("forward table maps canonical events %q and %q to Devin event %q", existing, entry.Claude, devinEvent)
		}
		forward[devinEvent] = entry.Claude
	}

	// A canonical event Devin cannot represent must not come back through the
	// reverse arm under its own name either. This is the check that catches a
	// regression to the deleted forward-lookup arm: PreCompact and SubagentStop
	// would start reversing the moment the two directions share a table.
	for _, canonical := range unmappable {
		if _, alsoDevinEvent := forward[canonical]; alsoDevinEvent {
			continue
		}
		if _, _, ok := ReverseHookEvent(canonical, TargetDevin); ok {
			t.Errorf("canonical event %q has no Devin cell but ReverseHookEvent accepts it", canonical)
		}
	}

	for devinEvent, canonical := range forward {
		got, matcher, ok := ReverseHookEvent(devinEvent, TargetDevin)
		if !ok {
			t.Errorf("forward table maps %q to Devin %q but ReverseHookEvent rejects it", canonical, devinEvent)
			continue
		}
		if got != canonical || matcher != "" {
			t.Errorf("ReverseHookEvent(%q, Devin) = (%q, %q), want (%q, \"\")", devinEvent, got, matcher, canonical)
		}
	}

	if len(devinReverseTable) != len(forward) {
		t.Errorf("devinReverseTable has %d rows, forward table declares %d Devin events", len(devinReverseTable), len(forward))
	}
	for _, entry := range devinReverseTable {
		if _, ok := forward[entry.TargetEvent]; !ok {
			t.Errorf("devinReverseTable lists %q, which the forward table never produces", entry.TargetEvent)
		}
	}
}

func TestDevinMcpInterpolation(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
		wantOK bool
	}{
		{"inline values use env interpolation", "inline", "${env:VAR}", true},
		{"canonical env token", "${env:VAR}", "${env:VAR}", true},
		{"file interpolation is unmappable", "${file:PATH}", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := McpInterpolationSupported(tt.source, TargetDevin)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("McpInterpolationSupported(%q, Devin) = (%q, %v), want (%q, %v)", tt.source, got, ok, tt.want, tt.wantOK)
			}
			if plain := McpInterpolation(tt.source, TargetDevin); plain != tt.want {
				t.Errorf("McpInterpolation(%q, Devin) = %q, want %q", tt.source, plain, tt.want)
			}
		})
	}
}

func TestDevinEnvTokenRequiresCallerSuppliedPluginRoot(t *testing.T) {
	value, literal := EnvToken("${CLAUDE_PLUGIN_ROOT}", TargetDevin)
	if value != "" || !literal {
		t.Errorf("EnvToken(${CLAUDE_PLUGIN_ROOT}, Devin) = (%q, %v), want (\"\", true)", value, literal)
	}
}
